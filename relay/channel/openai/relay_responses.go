package openai

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"new-router/common"
	"new-router/constant"
	"new-router/logger"
	"new-router/pkg/anonymizer"
	relaycommon "new-router/relay/common"
	"new-router/relay/helper"
	"new-router/relaykit/dto"
	"new-router/relaykit/types"
	"new-router/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	info.ObserveResponseModel(responsesResponse.Model)
	responseBody = rewriteSGLangResponsesCreatedAt(info, responseBody, "created_at", responsesResponse.CreatedAt)

	// 写入新的 response body
	responseBody, err = deanonymizeResponseBytes(c, responseBody)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	responseBody = relaycommon.ApplyResponseModelNameBytes(responseBody, info)
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := &dto.Usage{}
	service.ApplyResponsesUsage(usage, responsesResponse.Usage)
	// Count actual tool invocations from Output (not tool declarations).
	for _, output := range responsesResponse.Output {
		switch output.Type {
		case dto.BuildInCallWebSearchCall:
			info.CountBillableToolCall(dto.BuildInCallWebSearchCall, "")
		case dto.BuildInCallFileSearchCall:
			info.CountBillableToolCall(dto.BuildInCallFileSearchCall, "")
		case dto.BuildInCallFunctionCall:
			info.CountBillableToolCall(dto.BuildInCallFunctionCall, output.Name)
		}
	}
	info.ApplyVendorToolUsage(responseBody)

	imageCounter := &relaycommon.ImageGenerationCallCounter{}
	for i := range responsesResponse.Output {
		imageCounter.Observe(&responsesResponse.Output[i], &i)
	}
	imageCounter.Commit(info)

	return usage, nil
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, common.LogText("invalid response or response body"))
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}
	defer service.CloseResponseBodyGracefully(resp)

	accumulator := service.NewResponsesUsageAccumulator(info)
	restorer := anonymizer.FromContext(c).NewStreamRestorer()
	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			logger.LogError(c, common.LogText("failed to unmarshal stream response: %s", err.Error()))
			sr.Error(err)
			return
		}
		// Account for upstream output before anonymization restores client text.
		accumulator.Observe(&streamResponse, common.StringToByteSlice(data))
		if streamResponse.Response != nil {
			data = string(rewriteSGLangResponsesCreatedAt(info, []byte(data), "response.created_at", streamResponse.Response.CreatedAt))
		}

		// Cross-chunk restore for incremental text deltas: reassemble surrogates
		// split across SSE chunks before emitting. Other event types fall through
		// to the stateless whole-chunk restore below.
		if restorer != nil {
			switch streamResponse.Type {
			case "response.output_text.delta", "response.reasoning_summary_text.delta":
				rawDelta := streamResponse.Delta
				flush := restorer.Push(responsesStreamKey(&streamResponse), rawDelta)
				if flush != "" {
					streamResponse.Delta = flush
					patched, setErr := sjson.Set(data, "delta", flush)
					if setErr != nil {
						patched = data
					}
					sendResponsesStreamData(c, info, streamResponse, patched)
				}
				return
			case "response.output_text.done", "response.reasoning_summary_text.done":
				// Emit the held tail (in order) before the terminal-per-part event,
				// whose `text` carries the full value and is restored below.
				emitResponsesResidual(c, info, restorer, responsesStreamKey(&streamResponse))
			}
		}

		if deanonData, err := deanonymizeResponseString(c, data); err == nil && deanonData != data {
			if err := common.UnmarshalJsonStr(deanonData, &streamResponse); err == nil {
				data = deanonData
			}
		}
		sendResponsesStreamData(c, info, streamResponse, data)
	})

	// Safety net: flush any residual held tail when the stream ended without a
	// matching *.done event (truncated/abnormal end). On a normal stream the
	// per-part flush above already emptied the carry.
	if restorer != nil {
		keys := restorer.PendingKeys()
		sort.Strings(keys)
		for _, key := range keys {
			emitResponsesResidual(c, info, restorer, key)
		}
	}

	common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, info.StreamStatus)
	info.StreamStatus.RequireTerminal()
	return accumulator.Finish(), nil
}

func rewriteSGLangResponsesCreatedAt(info *relaycommon.RelayInfo, payload []byte, path string, createdAt dto.IntValue) []byte {
	if info.GetChannelType() != constant.ChannelTypeSGLang {
		return payload
	}
	if !gjson.GetBytes(payload, path).Exists() {
		return payload
	}
	patched, err := sjson.SetBytes(payload, path, int(createdAt))
	if err != nil {
		return payload
	}
	return patched
}

// responsesStreamKey derives a stable carry-buffer key for an incremental text
// event, partitioning by text family (output vs reasoning summary), item id, and
// output/content(summary) indices so concurrent output parts never cross-restore.
func responsesStreamKey(r *dto.ResponsesStreamResponse) string {
	family := "output_text"
	sub := r.ContentIndex
	if strings.HasPrefix(r.Type, "response.reasoning_summary_text") {
		family = "reasoning_summary"
		sub = r.SummaryIndex
	}
	out := 0
	if r.OutputIndex != nil {
		out = *r.OutputIndex
	}
	s := 0
	if sub != nil {
		s = *sub
	}
	return family + "\x00" + r.ItemID + "\x00" + strconv.Itoa(out) + "\x00" + strconv.Itoa(s)
}

// emitResponsesResidual flushes the held tail for key and, if non-empty, emits it
// as a synthetic *.delta event carrying the same item_id/index so clients splice
// it into the correct stream.
func emitResponsesResidual(c *gin.Context, info *relaycommon.RelayInfo, restorer *anonymizer.StreamRestorer, key string) {
	residual := restorer.Flush(key)
	if residual == "" {
		return
	}
	parts := strings.Split(key, "\x00")
	if len(parts) != 4 {
		return
	}
	out, _ := strconv.Atoi(parts[2])
	sub, _ := strconv.Atoi(parts[3])
	ev := dto.ResponsesStreamResponse{Delta: residual, ItemID: parts[1], OutputIndex: &out}
	if parts[0] == "reasoning_summary" {
		ev.Type = "response.reasoning_summary_text.delta"
		ev.SummaryIndex = &sub
	} else {
		ev.Type = "response.output_text.delta"
		ev.ContentIndex = &sub
	}
	body, err := common.Marshal(ev)
	if err != nil {
		return
	}
	sendResponsesStreamData(c, info, ev, string(body))
}
