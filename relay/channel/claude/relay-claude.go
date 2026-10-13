package claude

import (
	"cmp"
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
	"new-router/relaykit/relayconvert"
	"new-router/relaykit/types"
	"new-router/service"
	"new-router/setting/model_setting"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/sjson"
)

// applyClaudeUsageRewrites applies channel-level Claude usage rewrites to a parsed *dto.ClaudeUsage.
// Ordering is load-bearing: enforce the minimum cache-read ratio first (rescales the 5m/1h sub-buckets),
// then zero cache tokens for forced cache, then fold any remaining 1h creation tokens into the 5m bucket.
func applyClaudeUsageRewrites(usage *dto.ClaudeUsage, forceCache bool, cache1hAs5m bool, cacheReadMinRatio float64) {
	if usage == nil {
		return
	}
	hasAuthoritativeBillingUsage := usage.BillingUsage != nil
	if cacheReadMinRatio > 0 {
		ApplyCacheReadMinRatioUsage(usage, cacheReadMinRatio)
	}
	if forceCache {
		ApplyForceCacheUsageZeroing(usage)
	}
	if cache1hAs5m {
		ApplyCache1hAs5mUsage(usage)
	}
	if hasAuthoritativeBillingUsage && (forceCache || cache1hAs5m || cacheReadMinRatio > 0) {
		usage.BillingUsage = dto.NewClaudeMessagesBillingUsage(usage)
	}
}

func stopReasonClaude2OpenAI(reason string) string {
	return relayconvert.StopReasonClaudeToOpenAI(reason)
}

func maybeMarkClaudeRefusal(c *gin.Context, info *relaycommon.RelayInfo, stopReason string) {
	if c == nil {
		return
	}
	if strings.EqualFold(stopReason, "refusal") {
		info.PerformanceBusinessRejection = true
		common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "claude_stop_reason=refusal")
	}
}

func StreamResponseClaude2OpenAI(claudeResponse *dto.ClaudeResponse) *dto.ChatCompletionsStreamResponse {
	return relayconvert.StreamResponseClaude2OpenAI(claudeResponse)
}

func ResponseClaude2OpenAI(claudeResponse *dto.ClaudeResponse) *dto.OpenAITextResponse {
	return relayconvert.ResponseClaude2OpenAI(claudeResponse)
}

type ClaudeResponseInfo = relayconvert.ClaudeResponseInfo

const claudeStreamRestorerContextKey = "new-router.claude.stream-restorer"

type claudeStreamRestoreState struct {
	restorer *anonymizer.StreamRestorer
}

func claudeStreamRestorer(c *gin.Context) *anonymizer.StreamRestorer {
	if c == nil {
		return nil
	}
	if value, exists := c.Get(claudeStreamRestorerContextKey); exists {
		state, _ := value.(*claudeStreamRestoreState)
		if state != nil {
			return state.restorer
		}
	}
	state := &claudeStreamRestoreState{restorer: anonymizer.FromContext(c).NewStreamRestorer()}
	c.Set(claudeStreamRestorerContextKey, state)
	return state.restorer
}

func cacheCreationTokensForOpenAIUsage(usage *dto.Usage) int {
	if usage == nil {
		return 0
	}
	openAIUsage := relayconvert.UsageFromClaudeUsage(usage)
	if openAIUsage == nil {
		return 0
	}
	return openAIUsage.PromptTokens - usage.PromptTokens - usage.PromptTokensDetails.CachedTokens
}

func buildOpenAIStyleUsageFromClaudeUsage(usage *dto.Usage) dto.Usage {
	mapped := relayconvert.UsageFromClaudeUsage(usage)
	if mapped == nil {
		return dto.Usage{}
	}
	return *mapped
}

func buildMessageDeltaPatchUsage(claudeResponse *dto.ClaudeResponse, claudeInfo *ClaudeResponseInfo) *dto.ClaudeUsage {
	return relayconvert.BuildMessageDeltaPatchUsage(claudeResponse, claudeInfo)
}

func shouldSkipClaudeMessageDeltaUsagePatch(info *relaycommon.RelayInfo) bool {
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled {
		return true
	}
	if info == nil {
		return false
	}
	return info.ChannelSetting.PassThroughBodyEnabled
}

func patchClaudeMessageDeltaUsageData(data string, usage *dto.ClaudeUsage) string {
	return relayconvert.PatchClaudeMessageDeltaUsageData(data, usage)
}

func FormatClaudeResponseInfo(claudeResponse *dto.ClaudeResponse, oaiResponse *dto.ChatCompletionsStreamResponse, claudeInfo *ClaudeResponseInfo) bool {
	return relayconvert.FormatClaudeResponseInfo(claudeResponse, oaiResponse, claudeInfo)
}

func HandleStreamResponseData(c *gin.Context, info *relaycommon.RelayInfo, claudeInfo *ClaudeResponseInfo, data string) *types.NewAPIError {
	var claudeResponse dto.ClaudeResponse
	err := common.UnmarshalJsonStr(data, &claudeResponse)
	if err != nil {
		common.SysLog(common.LogText("error unmarshalling stream response: %s", err.Error()))
		return types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	restorer := claudeStreamRestorer(c)
	if claudeError := claudeResponse.GetClaudeError(); claudeError != nil && claudeError.Type != "" {
		return types.WithClaudeError(*claudeError, http.StatusInternalServerError)
	}
	if claudeResponse.Type == "message_start" && claudeResponse.Message != nil {
		info.ObserveResponseModel(claudeResponse.Message.Model)
	}
	if claudeResponse.StopReason != "" {
		maybeMarkClaudeRefusal(c, info, claudeResponse.StopReason)
	}
	if claudeResponse.Delta != nil && claudeResponse.Delta.StopReason != nil {
		maybeMarkClaudeRefusal(c, info, *claudeResponse.Delta.StopReason)
	}
	if claudeResponse.Type == "message_stop" {
		info.StreamStatus.MarkCompleted()
	}

	// 渠道级 Claude 缓存改写：先按最低读占比重分配，再强制缓存归零，最后 1h 折算为 5m。
	// 顺序与响应字节改写保持一致，struct 改写喂给计费（claudeInfo.Usage），字节改写喂给客户端可见 usage。
	forceCache := common.GetContextKeyBool(c, constant.ContextKeyClaudeForceCacheInjected)
	cache1hAs5m := common.GetContextKeyBool(c, constant.ContextKeyClaudeCache1hAs5m)
	cacheReadMinRatio := common.GetContextKeyFloat64(c, constant.ContextKeyClaudeCacheReadMinRatio)
	if (forceCache || cache1hAs5m || cacheReadMinRatio > 0) && info.RelayFormat != types.RelayFormatGemini {
		if claudeResponse.Message != nil {
			applyClaudeUsageRewrites(claudeResponse.Message.Usage, forceCache, cache1hAs5m, cacheReadMinRatio)
		}
		applyClaudeUsageRewrites(claudeResponse.Usage, forceCache, cache1hAs5m, cacheReadMinRatio)
	}

	if info.RelayFormat == types.RelayFormatClaude {
		FormatClaudeResponseInfo(&claudeResponse, nil, claudeInfo)

		if claudeResponse.Type == "message_start" {
			// message_start, 获取usage
			if claudeResponse.Message != nil {
				info.UpstreamModelName = claudeResponse.Message.Model
			}
		} else if claudeResponse.Type == "message_delta" {
			// 确保 message_delta 的 usage 包含完整的 input_tokens 和 cache 相关字段
			// 解决 AWS Bedrock 等上游返回的 message_delta 缺少这些字段的问题
			if !shouldSkipClaudeMessageDeltaUsagePatch(info) {
				data = patchClaudeMessageDeltaUsageData(data, buildMessageDeltaPatchUsage(&claudeResponse, claudeInfo))
			}
		}
		countClaudeStreamBillableTools(info, &claudeResponse)
		if cacheReadMinRatio > 0 {
			data = RewriteClaudeStreamUsageRatioString(data, cacheReadMinRatio)
		}
		if forceCache {
			data = RewriteClaudeStreamUsageString(data)
		}
		if cache1hAs5m {
			data = RewriteClaudeStreamUsage1hAs5mString(data)
		}
		// Cross-chunk restore: route incremental text/thinking through the carry
		// buffer so surrogates split across content_block_delta events are reassembled.
		if restorer != nil && claudeResponse.Type == "content_block_delta" && claudeResponse.Delta != nil {
			blockIdx := 0
			if claudeResponse.Index != nil {
				blockIdx = *claudeResponse.Index
			}
			switch claudeResponse.Delta.Type {
			case "text_delta":
				if claudeResponse.Delta.Text != nil {
					flushed := restorer.Push(strconv.Itoa(blockIdx)+":text", *claudeResponse.Delta.Text)
					if patched, e := sjson.Set(data, "delta.text", flushed); e == nil {
						data = patched
					}
				}
			case "thinking_delta":
				if claudeResponse.Delta.Thinking != nil {
					flushed := restorer.Push(strconv.Itoa(blockIdx)+":thinking", *claudeResponse.Delta.Thinking)
					if patched, e := sjson.Set(data, "delta.thinking", flushed); e == nil {
						data = patched
					}
				}
			}
		}
		// Flush the block's held tail before its content_block_stop is forwarded.
		if restorer != nil && claudeResponse.Type == "content_block_stop" {
			blockIdx := 0
			if claudeResponse.Index != nil {
				blockIdx = *claudeResponse.Index
			}
			emitClaudeNativeResidual(c, restorer, blockIdx)
		}
		if deanon, e := deanonymizeResponseString(c, data); e == nil {
			data = deanon
		}
		data = relaycommon.ApplyResponseModelNameString(data, info)
		helper.ClaudeChunkData(c, claudeResponse, data)
	} else if info.RelayFormat == types.RelayFormatOpenAI {
		// content_block_stop produces no OpenAI chunk, so flush the block's held
		// tail here before the converter drops it.
		if restorer != nil && claudeResponse.Type == "content_block_stop" {
			blockIdx := 0
			if claudeResponse.Index != nil {
				blockIdx = *claudeResponse.Index
			}
			emitClaudeOpenAIResidual(c, info, restorer, blockIdx, claudeInfo)
		}
		state, err := claudeToChatStreamState(info)
		if err != nil {
			return types.NewError(err, types.ErrorCodeBadResponseBody)
		}
		response, err := state.ConvertChunk(&claudeResponse)
		if err != nil {
			return types.NewError(err, types.ErrorCodeBadResponseBody)
		}

		if !FormatClaudeResponseInfo(&claudeResponse, response, claudeInfo) {
			return nil
		}

		if restorer != nil && response != nil {
			blockIdx := 0
			if claudeResponse.Index != nil {
				blockIdx = *claudeResponse.Index
			}
			for i := range response.Choices {
				ch := &response.Choices[i]
				if ch.Delta.Content != nil {
					flushed := restorer.Push(strconv.Itoa(blockIdx)+":text", *ch.Delta.Content)
					ch.Delta.Content = &flushed
				}
				if ch.Delta.ReasoningContent != nil {
					flushed := restorer.Push(strconv.Itoa(blockIdx)+":thinking", *ch.Delta.ReasoningContent)
					ch.Delta.ReasoningContent = &flushed
				}
			}
		}

		countClaudeStreamBillableTools(info, &claudeResponse)

		if response == nil {
			return nil
		}
		response.Model = info.ClientResponseModelName(response.Model)
		err = helper.ObjectData(c, response)
		if err != nil {
			logger.LogError(c, common.LogText("send_stream_response_failed: %s", err.Error()))
		}
	} else if info.RelayFormat == types.RelayFormatGemini {
		state, err := claudeToGeminiStreamState(info)
		if err != nil {
			return types.NewError(err, types.ErrorCodeBadResponseBody)
		}
		var results []relayconvert.ResponseResult
		for _, chunk := range prepareConvertedClaudeChunk(c, &claudeResponse, restorer) {
			converted, err := service.ConvertStreamResponseChunk(c, info, state, chunk)
			if err != nil {
				return types.NewError(err, types.ErrorCodeBadResponseBody)
			}
			results = append(results, converted...)
		}
		if !FormatClaudeResponseInfo(&claudeResponse, nil, claudeInfo) {
			return nil
		}
		countClaudeStreamBillableTools(info, &claudeResponse)
		if sendErr := sendGeminiStreamResults(c, info, results); sendErr != nil {
			return sendErr
		}
	}
	return nil
}

// emitClaudeNativeResidual flushes and emits any held tail for a Claude content
// block as a synthetic content_block_delta, keeping restored text before the
// block's content_block_stop.
func emitClaudeNativeResidual(c *gin.Context, restorer *anonymizer.StreamRestorer, blockIdx int) {
	for _, field := range []string{"text", "thinking"} {
		residual := restorer.Flush(strconv.Itoa(blockIdx) + ":" + field)
		if residual == "" {
			continue
		}
		delta := &dto.ClaudeMediaMessage{Type: "text_delta"}
		if field == "thinking" {
			delta.Type = "thinking_delta"
			delta.Thinking = &residual
		} else {
			delta.SetText(residual)
		}
		resp := dto.ClaudeResponse{Type: "content_block_delta", Delta: delta}
		resp.SetIndex(blockIdx)
		_ = helper.ClaudeData(c, resp)
	}
}

// emitClaudeOpenAIResidual flushes and emits any held tail for a Claude content
// block as a trailing chat.completion.chunk (OpenAI output format).
func emitClaudeOpenAIResidual(c *gin.Context, info *relaycommon.RelayInfo, restorer *anonymizer.StreamRestorer, blockIdx int, claudeInfo *ClaudeResponseInfo) {
	for _, field := range []string{"text", "thinking"} {
		residual := restorer.Flush(strconv.Itoa(blockIdx) + ":" + field)
		if residual == "" {
			continue
		}
		var delta dto.ChatCompletionsStreamResponseChoiceDelta
		if field == "thinking" {
			delta.ReasoningContent = &residual
		} else {
			delta.Content = &residual
		}
		resp := &dto.ChatCompletionsStreamResponse{
			Id:      claudeInfo.ResponseId,
			Object:  "chat.completion.chunk",
			Created: claudeInfo.Created,
			Model:   info.ClientResponseModelName(info.UpstreamModelName),
			Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0, Delta: delta}},
		}
		data, err := common.Marshal(resp)
		if err != nil {
			continue
		}
		_ = helper.StringData(c, string(data))
	}
}

func claudeToChatStreamState(info *relaycommon.RelayInfo) (*relayconvert.ClaudeToChatStreamState, error) {
	if info != nil && info.ClaudeToChatStreamState != nil {
		state, ok := info.ClaudeToChatStreamState.(*relayconvert.ClaudeToChatStreamState)
		if !ok || state == nil {
			return nil, fmt.Errorf("invalid Claude-to-Chat stream state %T", info.ClaudeToChatStreamState)
		}
		return state, nil
	}

	state := relayconvert.NewClaudeToChatStreamState()
	if info != nil {
		info.ClaudeToChatStreamState = state
	}
	return state, nil
}

func claudeToGeminiStreamState(info *relaycommon.RelayInfo) (*relayconvert.ResponseStreamState, error) {
	if info != nil && info.ChatToGeminiStreamState != nil {
		state, ok := info.ChatToGeminiStreamState.(*relayconvert.ResponseStreamState)
		if !ok || state == nil {
			return nil, fmt.Errorf("invalid Claude-to-Gemini stream state %T", info.ChatToGeminiStreamState)
		}
		return state, nil
	}

	state, err := relayconvert.NewResponseStreamState(types.RelayFormatClaude, types.RelayFormatGemini, relayconvert.ResponseStreamOptions{})
	if err != nil {
		return nil, err
	}
	if info != nil {
		info.ChatToGeminiStreamState = state
	}
	return state, nil
}

func sendGeminiStreamResults(c *gin.Context, info *relaycommon.RelayInfo, results []relayconvert.ResponseResult) *types.NewAPIError {
	for _, result := range results {
		geminiResponse, ok := result.Value.(*dto.GeminiChatResponse)
		if !ok {
			return types.NewError(fmt.Errorf("expected Gemini stream response, got %T", result.Value), types.ErrorCodeBadResponseBody)
		}
		if geminiResponse == nil {
			continue
		}
		data, err := common.Marshal(geminiResponse)
		if err != nil {
			return types.NewError(err, types.ErrorCodeBadResponseBody)
		}
		c.Render(-1, common.CustomEvent{Data: "data: " + relaycommon.ApplyResponseModelNameString(string(data), info)})
		_ = helper.FlushWriter(c)
	}
	return nil
}

func countClaudeStreamBillableTools(info *relaycommon.RelayInfo, claudeResponse *dto.ClaudeResponse) {
	if claudeResponse == nil {
		return
	}
	if claudeResponse.Type == "content_block_start" &&
		claudeResponse.ContentBlock != nil &&
		claudeResponse.ContentBlock.Type == "tool_use" {
		info.CountBillableToolCall(dto.BuildInCallToolUse, claudeResponse.ContentBlock.Name)
	}
	if claudeResponse.Type == "message_delta" && claudeResponse.Usage != nil {
		// message_delta usage is cumulative, so the last report wins.
		recordClaudeServerToolUse(info, claudeResponse.Usage.ServerToolUse)
	}
}

// recordClaudeServerToolUse bills the server tool requests Anthropic reports
// in usage.server_tool_use. web_fetch, code_execution, and tool_search have no
// built-in price and bill only once an operator prices them.
func recordClaudeServerToolUse(info *relaycommon.RelayInfo, serverToolUse *dto.ClaudeServerToolUse) {
	if info == nil || serverToolUse == nil {
		return
	}
	for name, count := range map[string]int{
		cmp.Or(info.WebSearchBillingKey, dto.BuildInToolWebSearch): serverToolUse.WebSearchRequests,
		"web_fetch":      serverToolUse.WebFetchRequests,
		"code_execution": serverToolUse.CodeExecutionRequests,
		"tool_search":    serverToolUse.ToolSearchRequests,
	} {
		if count > 0 {
			info.SetBillableToolCount(name, count)
		}
	}
}

func HandleStreamFinalResponse(c *gin.Context, info *relaycommon.RelayInfo, claudeInfo *ClaudeResponseInfo) {
	// Safety net: flush any residual carry left by a truncated stream (no
	// content_block_stop). Runs before the final usage chunk / [DONE].
	if restorer := claudeStreamRestorer(c); restorer != nil {
		keys := restorer.PendingKeys()
		sort.Strings(keys)
		seen := make(map[int]struct{}, len(keys))
		for _, key := range keys {
			parts := strings.SplitN(key, ":", 2)
			if len(parts) != 2 {
				continue
			}
			blockIdx, _ := strconv.Atoi(parts[0])
			if _, ok := seen[blockIdx]; ok {
				continue
			}
			seen[blockIdx] = struct{}{}
			if info.RelayFormat == types.RelayFormatClaude {
				emitClaudeNativeResidual(c, restorer, blockIdx)
			} else if info.RelayFormat == types.RelayFormatGemini {
				state, err := claudeToGeminiStreamState(info)
				if err == nil {
					for _, chunk := range flushConvertedClaudeChunks(restorer, strconv.Itoa(blockIdx)+":") {
						results, err := service.ConvertStreamResponseChunk(c, info, state, chunk)
						if err == nil {
							_ = sendGeminiStreamResults(c, info, results)
						}
					}
				}
			} else {
				emitClaudeOpenAIResidual(c, info, restorer, blockIdx, claudeInfo)
			}
		}
	}
	if claudeInfo.Usage.PromptTokens == 0 {
		//上游出错
	}
	if claudeInfo.Usage.CompletionTokens == 0 || !claudeInfo.Done {
		if common.DebugEnabled {
			common.SysLog(common.LogText("claude response usage is not complete, maybe upstream error"))
		}
		// 只补缺失字段，不整份覆盖——保留 message_start 已拿到的 cache 字段
		fallback := service.ResponseText2Usage(c, claudeInfo.ResponseText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		// Each tool_use block costs about 40 output tokens beyond its name and JSON
		// input (measured on Claude Haiku 4.5 and Sonnet 5 agent turns).
		fallback.CompletionTokens += claudeInfo.ToolUseCount * 40
		if claudeInfo.Usage.CompletionTokens == 0 ||
			(!claudeInfo.Done && fallback.CompletionTokens > claudeInfo.Usage.CompletionTokens) {
			claudeInfo.Usage.CompletionTokens = fallback.CompletionTokens
		}
		if claudeInfo.Usage.PromptTokens == 0 {
			claudeInfo.Usage.PromptTokens = fallback.PromptTokens
		}
		claudeInfo.Usage.TotalTokens = claudeInfo.Usage.PromptTokens + claudeInfo.Usage.CompletionTokens
	}
	if claudeInfo.Usage != nil {
		claudeInfo.Usage.UsageSemantic = "anthropic"
	}
	relayconvert.FinalizeClaudeStreamBillingUsage(claudeInfo)

	if info.RelayFormat == types.RelayFormatClaude {
		//
	} else if info.RelayFormat == types.RelayFormatOpenAI {
		if info.ShouldIncludeUsage {
			openAIUsage := buildOpenAIStyleUsageFromClaudeUsage(claudeInfo.Usage)
			response := helper.GenerateFinalUsageResponse(claudeInfo.ResponseId, claudeInfo.Created, info.ClientResponseModelName(info.UpstreamModelName), openAIUsage)
			err := helper.ObjectData(c, response)
			if err != nil {
				common.SysLog(common.LogText("send final response failed: %s", err.Error()))
			}
		}
		helper.Done(c)
	} else if info.RelayFormat == types.RelayFormatGemini {
		state, err := claudeToGeminiStreamState(info)
		if err != nil {
			common.SysLog(common.LogText("error creating Gemini stream state: %s", err.Error()))
			return
		}
		results, err := service.FinalizeStreamResponse(c, info, state)
		if err != nil {
			common.SysLog(common.LogText("error finalizing Gemini stream response: %s", err.Error()))
			return
		}
		if sendErr := sendGeminiStreamResults(c, info, results); sendErr != nil {
			common.SysLog("send final Gemini stream response failed: " + sendErr.Error())
		}
	}
}

func ClaudeStreamHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	claudeInfo := &ClaudeResponseInfo{
		ResponseId:   helper.GetResponseID(c),
		Created:      common.GetTimestamp(),
		Model:        info.UpstreamModelName,
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}
	var err *types.NewAPIError
	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		err = HandleStreamResponseData(c, info, claudeInfo, data)
		if err != nil {
			sr.Stop(err)
		}
	})
	info.StreamStatus.RequireTerminal()
	if err != nil {
		return nil, err
	}

	HandleStreamFinalResponse(c, info, claudeInfo)
	return claudeInfo.Usage, nil
}

func HandleClaudeResponseData(c *gin.Context, info *relaycommon.RelayInfo, claudeInfo *ClaudeResponseInfo, httpResp *http.Response, data []byte) *types.NewAPIError {
	var claudeResponse dto.ClaudeResponse
	err := common.Unmarshal(data, &claudeResponse)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	if claudeError := claudeResponse.GetClaudeError(); claudeError != nil && claudeError.Type != "" {
		return types.WithClaudeError(*claudeError, http.StatusInternalServerError)
	}
	info.ObserveResponseModel(claudeResponse.Model)
	maybeMarkClaudeRefusal(c, info, claudeResponse.StopReason)
	if claudeInfo.Usage == nil {
		claudeInfo.Usage = &dto.Usage{}
	}

	// 渠道级 Claude 缓存改写（非流式）：struct 改写喂给计费，字节改写喂给客户端可见 usage。
	forceCache := common.GetContextKeyBool(c, constant.ContextKeyClaudeForceCacheInjected)
	cache1hAs5m := common.GetContextKeyBool(c, constant.ContextKeyClaudeCache1hAs5m)
	cacheReadMinRatio := common.GetContextKeyFloat64(c, constant.ContextKeyClaudeCacheReadMinRatio)
	if forceCache || cache1hAs5m || cacheReadMinRatio > 0 {
		applyClaudeUsageRewrites(claudeResponse.Usage, forceCache, cache1hAs5m, cacheReadMinRatio)
	}

	if claudeResponse.Usage != nil {
		claudeInfo.Usage.PromptTokens = claudeResponse.Usage.InputTokens
		claudeInfo.Usage.CompletionTokens = claudeResponse.Usage.OutputTokens
		claudeInfo.Usage.TotalTokens = claudeResponse.Usage.InputTokens + claudeResponse.Usage.OutputTokens
		claudeInfo.Usage.UsageSemantic = "anthropic"
		claudeInfo.Usage.BillingUsage = dto.CloneBillingUsage(claudeResponse.Usage.BillingUsage)
		if claudeInfo.Usage.BillingUsage == nil {
			claudeInfo.Usage.BillingUsage = dto.NewClaudeMessagesBillingUsage(claudeResponse.Usage)
		}
		claudeInfo.Usage.PromptTokensDetails.CachedTokens = claudeResponse.Usage.CacheReadInputTokens
		claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens = claudeResponse.Usage.CacheCreationInputTokens
		claudeInfo.Usage.ClaudeCacheCreation5mTokens = claudeResponse.Usage.GetCacheCreation5mTokens()
		claudeInfo.Usage.ClaudeCacheCreation1hTokens = claudeResponse.Usage.GetCacheCreation1hTokens()
	}
	var responseData []byte
	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		openaiResponse := ResponseClaude2OpenAI(&claudeResponse)
		openaiResponse.Usage = buildOpenAIStyleUsageFromClaudeUsage(claudeInfo.Usage)
		responseData, err = common.Marshal(openaiResponse)
		if err != nil {
			return types.NewError(err, types.ErrorCodeBadResponseBody)
		}
	case types.RelayFormatOpenAIResponses:
		convertResult, err := service.ConvertResponse(c, info, types.RelayFormatOpenAIResponses, &claudeResponse)
		if err != nil {
			return types.NewError(err, types.ErrorCodeBadResponseBody)
		}
		responsesResponse, ok := convertResult.Value.(*dto.OpenAIResponsesResponse)
		if !ok {
			return types.NewError(fmt.Errorf("expected OpenAI Responses response, got %T", convertResult.Value), types.ErrorCodeBadResponseBody)
		}
		if responseID := helper.GetResponseID(c); responseID != "" {
			responsesResponse.ID = responseID
		}
		responseData, err = common.Marshal(responsesResponse)
		if err != nil {
			return types.NewError(err, types.ErrorCodeBadResponseBody)
		}
	case types.RelayFormatClaude:
		responseData = data
		if cacheReadMinRatio > 0 {
			responseData = RewriteClaudeResponseUsageRatioBytes(responseData, cacheReadMinRatio)
		}
		if forceCache {
			responseData = RewriteClaudeResponseUsageBytes(responseData)
		}
		if cache1hAs5m {
			responseData = RewriteClaudeResponseUsage1hAs5mBytes(responseData)
		}
		responseData = relaycommon.ApplyResponseModelNameBytes(responseData, info)
	case types.RelayFormatGemini:
		{
			convertResult, convertErr := service.ConvertResponse(c, info, types.RelayFormatGemini, &claudeResponse)
			if convertErr != nil {
				return types.NewError(convertErr, types.ErrorCodeBadResponseBody)
			}
			geminiResponse, ok := convertResult.Value.(*dto.GeminiChatResponse)
			if !ok {
				return types.NewError(fmt.Errorf("expected Gemini generateContent response, got %T", convertResult.Value), types.ErrorCodeBadResponseBody)
			}
			responseData, err = common.Marshal(geminiResponse)
			if err != nil {
				return types.NewError(err, types.ErrorCodeBadResponseBody)
			}
		}
	}

	if claudeResponse.Usage != nil {
		recordClaudeServerToolUse(info, claudeResponse.Usage.ServerToolUse)
	}

	if restored, deanonErr := deanonymizeResponseBytes(c, responseData); deanonErr == nil {
		responseData = restored
	}

	for _, block := range claudeResponse.Content {
		if block.Type == "tool_use" {
			info.CountBillableToolCall(dto.BuildInCallToolUse, block.Name)
		}
	}

	responseData = relaycommon.ApplyResponseModelNameBytes(responseData, info)
	service.IOCopyBytesGracefully(c, httpResp, responseData)
	return nil
}

func ClaudeHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	claudeInfo := &ClaudeResponseInfo{
		ResponseId:   helper.GetResponseID(c),
		Created:      common.GetTimestamp(),
		Model:        info.UpstreamModelName,
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	logger.LogDebug(c, "responseBody: %s", responseBody)
	handleErr := HandleClaudeResponseData(c, info, claudeInfo, resp, responseBody)
	if handleErr != nil {
		return nil, handleErr
	}
	return claudeInfo.Usage, nil
}
