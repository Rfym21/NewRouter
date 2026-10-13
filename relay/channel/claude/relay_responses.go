package claude

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"new-router/common"
	"new-router/logger"
	"new-router/pkg/anonymizer"
	relaycommon "new-router/relay/common"
	"new-router/relay/helper"
	"new-router/relaykit/dto"
	"new-router/relaykit/relayconvert"
	"new-router/relaykit/types"
	"new-router/service"
)

func ClaudeResponsesStreamHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	responseID := helper.GetResponseID(c)
	created := common.GetTimestamp()
	restorer := anonymizer.FromContext(c).NewStreamRestorer()
	state, err := relayconvert.NewResponseStreamState(types.RelayFormatClaude, types.RelayFormatOpenAIResponses, relayconvert.ResponseStreamOptions{
		ID:                 responseID,
		Model:              info.UpstreamModelName,
		Created:            created,
		EmitSequenceNumber: true,
	})
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	hostedBridge := relayconvert.NewClaudeHostedStreamBridge()

	claudeInfo := &ClaudeResponseInfo{
		ResponseId:   responseID,
		Created:      created,
		Model:        info.UpstreamModelName,
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}
	var streamErr *types.NewAPIError
	// streamFailed means a Responses-native terminal error was sent successfully.
	// In that case the scanner stops without a transport error and the partial
	// upstream usage remains billable.
	streamFailed := false

	sendResponsesEvent := func(eventType string, payload dto.ResponsesStreamResponse) bool {
		payload.Type = eventType
		data, err := common.Marshal(payload)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
			return false
		}
		if err := helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: eventType}, relaycommon.ApplyResponseModelNameString(string(data), info)); err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		return true
	}
	sendResult := func(result relayconvert.ResponseResult) bool {
		event, ok := result.Value.(relayconvert.ChatToResponsesStreamEvent)
		if !ok {
			streamErr = types.NewOpenAIError(
				fmt.Errorf("expected OpenAI Responses stream event, got %T", result.Value),
				types.ErrorCodeBadResponse,
				http.StatusInternalServerError,
			)
			return false
		}
		return sendResponsesEvent(event.Type, event.Payload)
	}
	failResponsesStream := func(err error) bool {
		failureResults, handled := state.FailResponsesStream("server_error", err.Error(), "")
		if !handled {
			return false
		}
		for _, result := range failureResults {
			if !sendResult(result) {
				return true
			}
		}
		streamFailed = true
		return true
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		var claudeResponse dto.ClaudeResponse
		if err := common.UnmarshalJsonStr(data, &claudeResponse); err != nil {
			logger.LogError(c, common.LogText("failed to unmarshal Claude stream event: %s", err.Error()))
			if failResponsesStream(err) {
				// A nil streamErr here is intentional: the protocol-level failure
				// event was delivered, so only the scanner needs to stop.
				sr.Stop(streamErr)
				return
			}
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
			sr.Stop(streamErr)
			return
		}
		if claudeError := claudeResponse.GetClaudeError(); claudeError != nil && claudeError.Type != "" {
			if failResponsesStream(fmt.Errorf("%s", claudeError.Message)) {
				sr.Stop(streamErr)
				return
			}
			streamErr = types.WithClaudeError(*claudeError, http.StatusInternalServerError)
			sr.Stop(streamErr)
			return
		}

		for _, prepared := range prepareConvertedClaudeChunk(c, &claudeResponse, restorer) {
			claudeResponse := *prepared
			if claudeResponse.StopReason != "" {
				maybeMarkClaudeRefusal(c, info, claudeResponse.StopReason)
			}
			if claudeResponse.Delta != nil && claudeResponse.Delta.StopReason != nil {
				maybeMarkClaudeRefusal(c, info, *claudeResponse.Delta.StopReason)
			}
			if claudeResponse.Type == "message_stop" {
				info.StreamStatus.MarkCompleted()
			}
			if claudeResponse.Type == "message_start" && claudeResponse.Message != nil {
				info.ObserveResponseModel(claudeResponse.Message.Model)
				info.UpstreamModelName = claudeResponse.Message.Model
			}
			FormatClaudeResponseInfo(&claudeResponse, nil, claudeInfo)
			countClaudeStreamBillableTools(info, &claudeResponse)
			hostedEvents, consumed, err := hostedBridge.Convert(&claudeResponse, state)
			if err != nil {
				if failResponsesStream(err) {
					sr.Stop(streamErr)
					return
				}
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				sr.Stop(streamErr)
				return
			}
			for _, event := range hostedEvents {
				if !sendResponsesEvent(event.Type, event.Payload) {
					sr.Stop(streamErr)
					return
				}
			}
			if consumed {
				continue
			}

			results, err := service.ConvertStreamResponseChunk(c, info, state, &claudeResponse)
			if err != nil {
				if failResponsesStream(err) {
					sr.Stop(streamErr)
					return
				}
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				sr.Stop(streamErr)
				return
			}
			for _, result := range results {
				if !sendResult(result) {
					sr.Stop(streamErr)
					return
				}
			}
		}
	})
	if streamErr != nil {
		return nil, streamErr
	}
	if streamFailed {
		return claudeInfo.Usage, nil
	}

	for _, chunk := range flushConvertedClaudeChunks(restorer, "") {
		results, err := service.ConvertStreamResponseChunk(c, info, state, chunk)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
		for _, result := range results {
			if !sendResult(result) {
				return nil, streamErr
			}
		}
	}
	HandleStreamFinalResponse(c, info, claudeInfo)
	openAIUsage := buildOpenAIStyleUsageFromClaudeUsage(claudeInfo.Usage)
	state.SetUsage(&openAIUsage)
	finalResults, err := service.FinalizeStreamResponse(c, info, state)
	if err != nil {
		if failResponsesStream(err) {
			return claudeInfo.Usage, streamErr
		}
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	for _, result := range finalResults {
		if !sendResult(result) {
			return nil, streamErr
		}
	}
	return claudeInfo.Usage, nil
}
