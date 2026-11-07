package gemini

import (
	"fmt"
	"io"
	"net/http"

	"new-router/common"
	"new-router/constant"
	"new-router/logger"
	"new-router/pkg/anonymizer"
	relaycommon "new-router/relay/common"
	"new-router/relay/helper"
	"new-router/relaykit/dto"
	"new-router/relaykit/types"
	"new-router/service"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
)

func GeminiTextGenerationHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// 读取响应体
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if err := anonymizer.ObserveResponse(c, responseBody); err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}

	logger.LogDebug(c, "Gemini native response body: %s", responseBody)

	// 解析为 Gemini 原生响应格式
	var geminiResponse dto.GeminiChatResponse
	err = common.Unmarshal(responseBody, &geminiResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	info.ObserveResponseModel(ojson.GetBytes(responseBody, "modelVersion").Str)
	var grounding geminiGroundingCounter
	grounding.observe(&geminiResponse)
	grounding.commit(info)
	countGeminiBillableFunctionCalls(info, &geminiResponse)

	if len(geminiResponse.Candidates) == 0 && geminiResponse.PromptFeedback != nil && geminiResponse.PromptFeedback.BlockReason != nil {
		info.PerformanceBusinessRejection = true
		common.SetContextKey(c, constant.ContextKeyAdminRejectReason, fmt.Sprintf("gemini_block_reason=%s", *geminiResponse.PromptFeedback.BlockReason))
	}

	// 计算使用量（优先上游 UsageMetadata，缺失时本地估算并保留 Gemini 计费语义）
	usage := buildUsageFromGeminiResponse(c, info, &geminiResponse)

	responseBody = relaycommon.ApplyResponseModelNameBytes(responseBody, info)
	if restored, deanonErr := deanonymizeResponseBytes(c, responseBody); deanonErr == nil {
		responseBody = restored
	}
	service.IOCopyBytesGracefully(c, resp, responseBody)

	return &usage, nil
}

func NativeGeminiEmbeddingHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	logger.LogDebug(c, "Gemini native embedding response body: %s", responseBody)

	var metadata *dto.GeminiEmbeddingUsageMetadata
	if info.IsGeminiBatchEmbedding {
		var geminiResponse dto.GeminiBatchEmbeddingResponse
		err = common.Unmarshal(responseBody, &geminiResponse)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		metadata = geminiResponse.UsageMetadata
	} else {
		var geminiResponse dto.GeminiEmbeddingResponse
		err = common.Unmarshal(responseBody, &geminiResponse)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		metadata = geminiResponse.UsageMetadata
	}

	var usage *dto.Usage
	if upstream := metadata.GeminiUsageMetadata(); upstream != nil {
		upstreamUsage := buildUsageFromGeminiMetadata(upstream, info.GetEstimatePromptTokens())
		usage = &upstreamUsage
	} else {
		usage = service.ResponseText2Usage(c, "", info.UpstreamModelName, info.GetEstimatePromptTokens())
	}

	responseBody = relaycommon.ApplyResponseModelNameBytes(responseBody, info)
	service.IOCopyBytesGracefully(c, resp, responseBody)

	return usage, nil
}

func GeminiTextGenerationStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	helper.SetEventStreamHeaders(c)

	restorer := anonymizer.FromContext(c).NewStreamRestorer()

	usage, apiErr := geminiStreamHandler(c, info, resp, func(data string, geminiResponse *dto.GeminiChatResponse) bool {
		out, err := deanonymizeGeminiNativeChunk(c, data, restorer)
		if err != nil {
			logger.LogError(c, "failed to deanonymize stream chunk: "+err.Error())
			return false
		}
		if err := helper.StringData(c, relaycommon.ApplyResponseModelNameString(out, info)); err != nil {
			logger.LogError(c, "failed to write stream data: "+err.Error())
			return false
		}
		info.SendResponseCount++
		return true
	})

	// Flush validated tails before returning, including abnormal EOF.
	frames, flushErr := flushGeminiNativeRestored(c, restorer)
	if flushErr != nil {
		return usage, types.NewError(flushErr, types.ErrorCodeBadResponseBody)
	}
	for _, frame := range frames {
		if err := helper.StringData(c, relaycommon.ApplyResponseModelNameString(string(frame), info)); err != nil {
			logger.LogError(c, "gemini native residual flush failed: "+err.Error())
			break
		}
		info.SendResponseCount++
	}

	return usage, apiErr
}

// geminiStreamRestorerContext reuses call metadata alongside the caller's carry
// buffer. Each new stream gets a fresh restorer, even on a reused request context.
const geminiStreamRestorerContext = "new-router.gemini.stream-restorer"

type geminiRestoreState struct {
	stream  *anonymizer.StreamRestorer
	restore *anonymizer.GeminiStreamRestorer
}

func deanonymizeGeminiNativeChunk(c *gin.Context, data string, stream *anonymizer.StreamRestorer) (string, error) {
	session := anonymizer.FromContext(c)
	if session == nil || stream == nil {
		return deanonymizeResponseString(c, data)
	}
	var state *geminiRestoreState
	if value, ok := c.Get(geminiStreamRestorerContext); ok {
		state, _ = value.(*geminiRestoreState)
	}
	if state == nil || state.stream != stream {
		state = &geminiRestoreState{stream: stream, restore: anonymizer.NewGeminiStreamRestorer(session, stream)}
		c.Set(geminiStreamRestorerContext, state)
	}
	frames, err := state.restore.Restore([]byte(data))
	if err != nil {
		return data, err
	}
	if len(frames) == 0 {
		return data, nil
	}
	return string(frames[0]), nil
}

func buildGeminiNativeResidual(key string, residual string) (string, bool) {
	return anonymizer.BuildGeminiNativeResidual(key, residual)
}

func flushGeminiNativeRestored(c *gin.Context, stream *anonymizer.StreamRestorer) ([][]byte, error) {
	if stream == nil {
		return nil, nil
	}
	if value, ok := c.Get(geminiStreamRestorerContext); ok {
		if state, ok := value.(*geminiRestoreState); ok && state.stream == stream {
			return state.restore.Flush()
		}
	}
	return anonymizer.NewGeminiStreamRestorer(anonymizer.FromContext(c), stream).Flush()
}
