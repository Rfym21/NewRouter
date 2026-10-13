package gemini

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

func GeminiTextGenerationHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// 读取响应体
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	logger.LogDebug(c, "Gemini native response body: %s", responseBody)

	// 解析为 Gemini 原生响应格式
	var geminiResponse dto.GeminiChatResponse
	err = common.Unmarshal(responseBody, &geminiResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	info.ObserveResponseModel(gjson.GetBytes(responseBody, "modelVersion").Str)
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

	usage := service.ResponseText2Usage(c, "", info.UpstreamModelName, info.GetEstimatePromptTokens())

	if info.IsGeminiBatchEmbedding {
		var geminiResponse dto.GeminiBatchEmbeddingResponse
		err = common.Unmarshal(responseBody, &geminiResponse)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
	} else {
		var geminiResponse dto.GeminiEmbeddingResponse
		err = common.Unmarshal(responseBody, &geminiResponse)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
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

	// Flush residual held tails as trailing native chunks (native has no [DONE],
	// so this only needs to precede the function return).
	if restorer != nil {
		keys := restorer.PendingKeys()
		sort.Strings(keys)
		for _, key := range keys {
			residual := restorer.Flush(key)
			if residual == "" {
				continue
			}
			chunk, ok := buildGeminiNativeResidual(key, residual)
			if !ok {
				continue
			}
			if err := helper.StringData(c, relaycommon.ApplyResponseModelNameString(chunk, info)); err != nil {
				logger.LogError(c, "gemini native residual flush failed: "+err.Error())
				break
			}
			info.SendResponseCount++
		}
	}

	return usage, apiErr
}

// deanonymizeGeminiNativeChunk restores anonymized values in one native Gemini
// stream chunk, reassembling surrogates split across chunks via the carry buffer.
// Only candidates[].content.parts[].text is routed through the buffer (keyed by
// candidate index + thought flag); it edits those fields via sjson so all other
// upstream fields (usageMetadata, modelVersion, safetyRatings, …) are preserved
// verbatim. A nil restorer falls back to the stateless whole-chunk restore.
func deanonymizeGeminiNativeChunk(c *gin.Context, data string, restorer *anonymizer.StreamRestorer) (string, error) {
	if restorer == nil {
		return deanonymizeResponseString(c, data)
	}
	if anonymizer.FromContext(c) == nil {
		return data, nil
	}
	candidates := gjson.Get(data, "candidates")
	if !candidates.IsArray() {
		return deanonymizeResponseString(c, data)
	}
	out := data
	for ci, cand := range candidates.Array() {
		idx := ci
		if v := cand.Get("index"); v.Exists() {
			idx = int(v.Int())
		}
		parts := cand.Get("content.parts")
		if !parts.IsArray() {
			continue
		}
		for pi, part := range parts.Array() {
			tv := part.Get("text")
			if tv.Type != gjson.String {
				continue
			}
			field := "content"
			if part.Get("thought").Bool() {
				field = "reasoning"
			}
			flushed := restorer.Push(strconv.Itoa(idx)+":"+field, tv.String())
			path := "candidates." + strconv.Itoa(ci) + ".content.parts." + strconv.Itoa(pi) + ".text"
			if patched, err := sjson.Set(out, path, flushed); err == nil {
				out = patched
			}
		}
	}
	return out, nil
}

// buildGeminiNativeResidual builds a minimal native Gemini chunk carrying the
// residual text for the given carry key ("<candidateIndex>:content|reasoning").
func buildGeminiNativeResidual(key string, residual string) (string, bool) {
	sep := strings.IndexByte(key, ':')
	if sep < 0 {
		return "", false
	}
	idx, _ := strconv.Atoi(key[:sep])
	field := key[sep+1:]
	chunk := `{"candidates":[{"content":{"parts":[{}],"role":"model"},"index":0}]}`
	chunk, _ = sjson.Set(chunk, "candidates.0.index", idx)
	chunk, _ = sjson.Set(chunk, "candidates.0.content.parts.0.text", residual)
	if field == "reasoning" {
		chunk, _ = sjson.Set(chunk, "candidates.0.content.parts.0.thought", true)
	}
	return chunk, true
}
