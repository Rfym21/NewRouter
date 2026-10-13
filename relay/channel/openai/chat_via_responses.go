package openai

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"new-router/common"
	"new-router/logger"
	"new-router/pkg/anonymizer"
	relaycommon "new-router/relay/common"
	"new-router/relay/helper"
	"new-router/relaykit/dto"
	"new-router/relaykit/relayconvert"
	"new-router/relaykit/types"
	"new-router/service"

	"github.com/gin-gonic/gin"
)

func OaiResponsesToChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var responsesResp dto.OpenAIResponsesResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	if err := common.Unmarshal(body, &responsesResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if oaiError := responsesResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	info.ObserveResponseModel(responsesResp.Model)
	responseValue, usage, err := convertResponsesResponseForClient(c, info, &responsesResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	responseBody, err := common.Marshal(responseValue)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}

	responseBody, err = deanonymizeResponseBytes(c, responseBody)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}

	responseBody = relaycommon.ApplyResponseModelNameBytes(responseBody, info)
	service.IOCopyBytesGracefully(c, resp, responseBody)
	return usage, nil
}

func OaiResponsesToChatBufferedStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.RequireTerminal()
	accumulator := relayconvert.NewResponsesBufferedAccumulator()
	var finalResponse *dto.OpenAIResponsesResponse
	var streamErr *types.NewAPIError

	scanner := helper.NewStreamScanner(resp.Body)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 6 || line[:5] != "data:" {
			continue
		}
		data := line[5:]
		data = strings.TrimSpace(data)
		if data == "" || data == "[DONE]" {
			if data == "[DONE]" {
				break
			}
			continue
		}

		var streamResp dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResp); err != nil {
			logger.LogError(c, common.LogText("failed to unmarshal buffered responses stream event: %s", err.Error()))
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
			break
		}
		if streamResp.Response != nil {
			info.ObserveResponseModel(streamResp.Response.Model)
		}
		service.ObserveResponsesOutcome(info, &streamResp)
		accumulator.ProcessEvent(&streamResp)
		switch streamResp.Type {
		case "response.completed", "response.done", "response.incomplete":
			finalResponse = streamResp.Response
			if streamResp.Type == "response.incomplete" {
				if finalResponse == nil {
					finalResponse = &dto.OpenAIResponsesResponse{}
				}
				if len(finalResponse.Status) == 0 {
					finalResponse.Status = []byte(`"incomplete"`)
				}
			}
		case "response.failed", "response.error":
			if streamResp.Response != nil {
				if oaiErr := streamResp.Response.GetOpenAIError(); oaiErr != nil && oaiErr.Type != "" {
					streamErr = types.WithOpenAIError(*oaiErr, http.StatusInternalServerError)
					break
				}
			}
			streamErr = types.NewOpenAIError(fmt.Errorf("responses stream error: %s", streamResp.Type), types.ErrorCodeBadResponse, http.StatusInternalServerError)
		}
		if streamErr != nil || finalResponse != nil {
			break
		}
	}
	if streamErr != nil {
		return nil, streamErr
	}
	if err := scanner.Err(); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	if finalResponse == nil {
		finalResponse = &dto.OpenAIResponsesResponse{
			ID:        helper.GetResponseID(c),
			CreatedAt: dto.IntValue(time.Now().Unix()),
			Model:     info.UpstreamModelName,
			Status:    []byte(`"completed"`),
		}
	}
	accumulator.SupplementResponseOutput(finalResponse)

	responseValue, usage, err := convertResponsesResponseForClient(c, info, finalResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	responseBody, err := common.Marshal(responseValue)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}

	responseBody, err = deanonymizeResponseBytes(c, responseBody)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}

	responseBody = relaycommon.ApplyResponseModelNameBytes(responseBody, info)
	service.IOCopyBytesGracefully(c, resp, responseBody)
	return usage, nil
}

func convertResponsesResponseForClient(c *gin.Context, info *relaycommon.RelayInfo, response *dto.OpenAIResponsesResponse) (any, *dto.Usage, error) {
	if responseID := helper.GetResponseID(c); responseID != "" {
		response.ID = responseID
	}

	usage := relayconvert.UsageFromResponsesUsage(response.Usage)
	if usage == nil || usage.TotalTokens == 0 {
		text := service.ExtractOutputTextFromResponses(response)
		usage = service.ResponseText2Usage(c, text, info.UpstreamModelName, info.GetEstimatePromptTokens())
		response.Usage = relayconvert.UsageFromChatUsage(usage)
	}

	result, err := service.ConvertResponse(c, info, info.RelayFormat, response)
	if err != nil {
		return nil, nil, err
	}
	if result.Usage != nil && result.Usage.TotalTokens != 0 {
		usage = result.Usage
	}
	return result.Value, usage, nil
}

func OaiResponsesToChatStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	responseId := helper.GetResponseID(c)
	createAt := time.Now().Unix()
	restorer := anonymizer.FromContext(c).NewStreamRestorer()
	state, err := relayconvert.NewResponseStreamState(types.RelayFormatOpenAIResponses, info.RelayFormat, relayconvert.ResponseStreamOptions{
		ID:      responseId,
		Model:   info.UpstreamModelName,
		Created: createAt,
		Transform: func(_ relayconvert.ResponseStep, value any) (any, error) {
			switch chunk := value.(type) {
			case dto.ChatCompletionsStreamResponse:
				deanonymizeChatStreamChunk(c, &chunk, restorer)
				return chunk, nil
			case *dto.ChatCompletionsStreamResponse:
				deanonymizeChatStreamChunk(c, chunk, restorer)
				return chunk, nil
			default:
				return value, nil
			}
		},
	})
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	streamErr := (*types.NewAPIError)(nil)
	// upstreamFailed records a response.failed / response.error event and
	// failedUsage the usage it reported, if any.
	upstreamFailed := false
	var failedUsage *dto.Usage

	if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo == nil {
		info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
	}

	// writeFailed stops the stream after a failed client write. When the client
	// went away the upstream already produced output, so the request settles
	// normally instead of failing; any other write error stays a server error.
	writeFailed := func(err error) bool {
		if c.Request.Context().Err() != nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
			return false
		}
		streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		return false
	}

	sendGeminiResponse := func(geminiResponse *dto.GeminiChatResponse) bool {
		if geminiResponse == nil {
			return true
		}
		geminiResponseStr, err := common.Marshal(geminiResponse)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
			return false
		}
		c.Render(-1, common.CustomEvent{Data: "data: " + relaycommon.ApplyResponseModelNameString(string(geminiResponseStr), info)})
		_ = helper.FlushWriter(c)
		return true
	}

	sendClaudeResponse := func(claudeResponse *dto.ClaudeResponse) bool {
		if claudeResponse == nil {
			return true
		}
		for _, chunk := range restoreConvertedClaudeChunk(claudeResponse, restorer) {
			data, err := common.Marshal(chunk)
			if err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
				return false
			}
			data, _ = deanonymizeResponseBytes(c, data)
			if err := helper.ClaudeData(c, *chunk, relaycommon.ApplyResponseModelNameString(string(data), info)); err != nil {
				return writeFailed(err)
			}
		}
		return true
	}

	sendStreamResult := func(result relayconvert.ResponseResult) bool {
		switch value := result.Value.(type) {
		case dto.ChatCompletionsStreamResponse:
			if len(value.Choices) == 0 && value.Usage == nil {
				return true
			}
			value.Model = info.ClientResponseModelName(value.Model)
			if err := helper.ObjectData(c, &value); err != nil {
				return writeFailed(err)
			}
			return true
		case *dto.ChatCompletionsStreamResponse:
			if value == nil || (len(value.Choices) == 0 && value.Usage == nil) {
				return true
			}
			value.Model = info.ClientResponseModelName(value.Model)
			if err := helper.ObjectData(c, value); err != nil {
				return writeFailed(err)
			}
			return true
		case dto.ClaudeResponse:
			return sendClaudeResponse(&value)
		case *dto.ClaudeResponse:
			return sendClaudeResponse(value)
		case dto.GeminiChatResponse:
			return sendGeminiResponse(&value)
		case *dto.GeminiChatResponse:
			return sendGeminiResponse(value)
		default:
			streamErr = types.NewOpenAIError(fmt.Errorf("unsupported converted stream response type %T", result.Value), types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if streamErr != nil {
			stopStream(sr, streamErr)
			return
		}

		var streamResp dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResp); err != nil {
			logger.LogError(c, common.LogText("failed to unmarshal responses stream event: %s", err.Error()))
			sr.Error(err)
			return
		}

		if streamResp.Response != nil {
			info.ObserveResponseModel(streamResp.Response.Model)
		}
		if streamResp.Type == "response.error" || streamResp.Type == "response.failed" {
			upstreamFailed = true
			if streamResp.Response != nil {
				failedUsage = streamResp.Response.Usage
				if oaiErr := streamResp.Response.GetOpenAIError(); oaiErr != nil && oaiErr.Type != "" {
					streamErr = types.WithOpenAIError(*oaiErr, http.StatusInternalServerError)
					stopStream(sr, streamErr)
					return
				}
			}
			streamErr = types.NewOpenAIError(fmt.Errorf("responses stream error: %s", streamResp.Type), types.ErrorCodeBadResponse, http.StatusInternalServerError)
			stopStream(sr, streamErr)
			return
		}

		results, err := service.ConvertStreamResponseChunk(c, info, state, &streamResp)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			stopStream(sr, streamErr)
			return
		}
		for _, result := range results {
			if !sendStreamResult(result) {
				stopStream(sr, streamErr)
				return
			}
		}
	})

	if streamErr != nil {
		if !upstreamFailed || state.UsageText() == "" {
			return nil, streamErr
		}
		// The upstream failed after output reached the client: bill the
		// delivered output (upstream usage when reported, otherwise the
		// estimate) and send the client the same error the failed request
		// path writes. stream_status already records the failure.
		usage := relayconvert.UsageFromResponsesUsage(failedUsage)
		if usage.TotalTokens == 0 {
			usage = state.Usage()
		}
		if usage == nil || usage.TotalTokens == 0 {
			usage = service.ResponseText2Usage(c, state.UsageText(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		} else if usage.CompletionTokens == 0 || usage.PromptTokens == 0 {
			// As in the native Responses accumulator, reported usage that
			// omits the output or the prompt is completed from the estimate of
			// the delivered output and the prompt.
			estimate := service.ResponseText2Usage(c, state.UsageText(), info.UpstreamModelName, info.GetEstimatePromptTokens())
			if usage.CompletionTokens == 0 {
				usage.CompletionTokens = estimate.CompletionTokens
			}
			if usage.PromptTokens == 0 {
				usage.PromptTokens = estimate.PromptTokens
			}
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
			if usage.BillingUsage != nil {
				usage.BillingUsage = dto.CloneBillingUsageWithEstimatedCompletion(usage.BillingUsage, usage.CompletionTokens)
			}
		}
		logger.LogError(c, common.LogText("responses stream failed after output was delivered, billing the delivered output: %s", streamErr.Error()))
		streamErr.SetMessage(common.MessageWithRequestId(streamErr.Error(), c.GetString(common.RequestIdKey)))
		if info.RelayFormat == types.RelayFormatClaude {
			c.JSON(streamErr.StatusCode, gin.H{"type": "error", "error": streamErr.ToClaudeError()})
		} else {
			c.JSON(streamErr.StatusCode, gin.H{"error": streamErr.ToOpenAIError()})
		}
		return usage, nil
	}

	// Emit any surrogate tail held by the cross-chunk carry buffer before the
	// finalize/usage chunks.
	if restorer != nil {
		for _, key := range restorer.PendingKeys() {
			if strings.HasPrefix(key, "claude:") {
				if chunk := flushConvertedClaudeResidual(restorer, key); chunk != nil {
					data, err := common.Marshal(chunk)
					if err == nil {
						helper.ClaudeChunkData(c, *chunk, string(data))
					}
				}
				continue
			}
			residual := restorer.Flush(key)
			if residual == "" {
				continue
			}
			idx := 0
			field := key
			if parts := strings.SplitN(key, ":", 2); len(parts) == 2 {
				idx, _ = strconv.Atoi(parts[0])
				field = parts[1]
			}
			var delta dto.ChatCompletionsStreamResponseChoiceDelta
			if field == "reasoning_content" {
				delta.SetReasoningContent(residual)
			} else {
				delta.SetContentString(residual)
			}
			chunk := dto.ChatCompletionsStreamResponse{
				Id:      responseId,
				Object:  "chat.completion.chunk",
				Created: createAt,
				Model:   info.UpstreamModelName,
				Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: idx, Delta: delta}},
			}
			if chunkData, err := common.Marshal(&chunk); err == nil {
				if info.RelayFormat == types.RelayFormatOpenAI {
					_ = helper.StringData(c, relaycommon.ApplyResponseModelNameString(string(chunkData), info))
				} else {
					_ = HandleStreamFormat(c, info, string(chunkData), false, false)
				}
			}
		}
	}

	usage := state.Usage()
	if usage == nil || usage.TotalTokens == 0 {
		usage = service.ResponseText2Usage(c, state.UsageText(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		state.SetUsage(usage)
	}
	if info.StreamStatus.EndReason == relaycommon.StreamEndReasonClientGone {
		return usage, nil
	}

	if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo != nil {
		info.ClaudeConvertInfo.Usage = usage
	}
	finalResults, err := service.FinalizeStreamResponse(c, info, state)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	for _, result := range finalResults {
		if !sendStreamResult(result) {
			if streamErr != nil {
				return nil, streamErr
			}
			return usage, nil
		}
	}
	if info.RelayFormat == types.RelayFormatOpenAI && info.ShouldIncludeUsage && usage != nil {
		if err := helper.ObjectData(c, helper.GenerateFinalUsageResponse(responseId, createAt, info.ClientResponseModelName(info.UpstreamModelName), *usage)); err != nil {
			writeFailed(err)
			if streamErr != nil {
				return nil, streamErr
			}
			return usage, nil
		}
	}

	if info.RelayFormat == types.RelayFormatOpenAI {
		helper.Done(c)
	}
	return usage, nil
}
