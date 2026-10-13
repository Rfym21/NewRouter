package claude

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"new-router/common"
	"new-router/constant"
	"new-router/logger"
	"new-router/relay/channel"
	relaycommon "new-router/relay/common"
	"new-router/relaykit/dto"
	"new-router/relaykit/relayconvert"
	"new-router/relaykit/types"
	"new-router/service"
	"new-router/setting/model_setting"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	result, err := service.ConvertRequest(c, info, types.RelayFormatClaude, request)
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	if request.MaxTokens != nil && *request.MaxTokens == 0 {
		request.MaxTokens = nil
	}
	if err := relayconvert.ApplyClaudeThinkingModel(request, info); err != nil {
		return nil, err
	}
	// A model reasoning suffix replaces the client's thinking config, but
	// block_binding is a conversation control, not a reasoning setting. Keep
	// the client's value when the rendered mode is still adaptive.
	if original, ok := info.Request.(*dto.ClaudeRequest); ok && original.Thinking != nil &&
		request.Thinking != nil && request.Thinking.Type == "adaptive" && len(request.Thinking.BlockBinding) == 0 {
		request.Thinking.BlockBinding = original.Thinking.BlockBinding
	}
	if request.MaxTokens == nil {
		defaultMaxTokens := uint(model_setting.GetClaudeSettings().GetDefaultMaxTokens(request.Model))
		request.MaxTokens = &defaultMaxTokens
	}
	// ApplyClaudeThinkingModel no longer rewrites request.Model. Do not write
	// a still-suffixed name back over the entry-normalized UpstreamModelName
	// (AWS/Vertex look up getAwsModelID / claudeModelMap from that field).
	if info.UpstreamModelName == "" {
		info.UpstreamModelName = request.Model
	}
	return request, nil
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	requestURL := fmt.Sprintf("%s/v1/messages", info.ChannelBaseUrl)
	if !shouldAppendClaudeBetaQuery(info) {
		return requestURL, nil
	}

	parsedURL, err := url.Parse(requestURL)
	if err != nil {
		return "", err
	}
	query := parsedURL.Query()
	query.Set("beta", "true")
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}

func shouldAppendClaudeBetaQuery(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	if info.IsClaudeBetaQuery {
		return true
	}
	if info.ChannelOtherSettings.ClaudeBetaQuery {
		return true
	}
	return false
}

func CommonClaudeHeadersOperation(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) {
	// common headers operation
	// A client may send several anthropic-beta lines; the API reads one
	// comma-separated list, so forward all of them.
	anthropicBeta := strings.Join(c.Request.Header.Values("anthropic-beta"), ",")
	if anthropicBeta != "" {
		req.Set("anthropic-beta", anthropicBeta)
	}
	model_setting.GetClaudeSettings().WriteHeaders(info.OriginModelName, req)
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("x-api-key", info.ApiKey)
	anthropicVersion := c.Request.Header.Get("anthropic-version")
	if anthropicVersion == "" {
		anthropicVersion = "2023-06-01"
	}
	req.Set("anthropic-version", anthropicVersion)
	CommonClaudeHeadersOperation(c, req, info)
	if info.ChannelOtherSettings.ClaudeCodeSimulationEnabled {
		resolvedHeaders, err := channel.ResolveHeaderOverride(info, c, *req)
		if err != nil {
			return err
		}
		// Resolve identity passthrough before simulation so defaults cannot mask an allowed client value.
		for name, value := range resolvedHeaders {
			if isClaudeCodeIdentityHeader(name) {
				req.Set(name, value)
			}
		}
		ApplyClaudeCodeSimulationHeaders(req, info.ApiKey, info.ChannelOtherSettings.ClaudeCodeSimulation)
	}
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	result, err := service.ConvertRequest(c, info, types.RelayFormatClaude, request)
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	result, err := service.ConvertRequest(c, info, types.RelayFormatClaude, &request)
	if err != nil {
		return nil, err
	}
	claudeRequest, ok := result.Value.(*dto.ClaudeRequest)
	if !ok {
		return nil, fmt.Errorf("expected Anthropic Messages request, got %T", result.Value)
	}
	return claudeRequest, nil
}

// ShouldApplyClaudeRequestBodySettings reports whether channel settings need Claude request body rewriting.
func ShouldApplyClaudeRequestBodySettings(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	settings := info.ChannelOtherSettings
	return settings.ForceCacheEnabled ||
		settings.ForceCache1hEnabled ||
		settings.ClaudeCodeSimulationEnabled
}

// ApplyClaudeRequestBodySettings applies channel-level Claude request body rewrites.
// Injection order matters: force-cache injects with its own TTL first, Claude Code simulation
// runs next, and the blanket 1h rewrite applies last so every cache_control ends at ttl=1h.
func ApplyClaudeRequestBodySettings(c *gin.Context, info *relaycommon.RelayInfo, bodyBytes []byte) ([]byte, error) {
	if info == nil {
		return bodyBytes, nil
	}
	settings := info.ChannelOtherSettings

	if settings.ForceCacheEnabled {
		newBody, injected := InjectForceCacheControl(bodyBytes, settings.ForceCacheTTL)
		bodyBytes = newBody
		if injected {
			common.SetContextKey(c, constant.ContextKeyClaudeForceCacheInjected, true)
		}
	}

	if settings.ClaudeCodeSimulationEnabled {
		newBody, err := ApplyClaudeCodeSimulationBody(bodyBytes, settings.ClaudeCodeSimulation)
		if err != nil {
			return nil, fmt.Errorf("apply claude code simulation failed: %w", err)
		}
		bodyBytes = newBody
	}

	if settings.ForceCache1hEnabled {
		if rewritten, _ := ForceCacheControlTTLTo1h(bodyBytes); rewritten != nil {
			bodyBytes = rewritten
		}
	}

	return bodyBytes, nil
}

// ApplyClaudeResponseUsageSettings enables channel-level Claude usage rewrites for the response path.
func ApplyClaudeResponseUsageSettings(c *gin.Context, info *relaycommon.RelayInfo) {
	if info == nil {
		return
	}
	settings := info.ChannelOtherSettings

	if settings.Cache1hBillAs5mEnabled {
		common.SetContextKey(c, constant.ContextKeyClaudeCache1hAs5m, true)
	}

	if settings.CacheReadMinRatioEnabled {
		ratio := NormalizeCacheReadMinRatio(settings.CacheReadMinRatio)
		if ratio > 0 {
			common.SetContextKey(c, constant.ContextKeyClaudeCacheReadMinRatio, ratio)
		}
	}
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	var bodyBytes []byte
	bodyReady := false

	if info != nil && requestBody != nil {
		settings := info.ChannelOtherSettings
		needRead := ShouldApplyClaudeRequestBodySettings(info) ||
			settings.ThinkingSignatureRectifierEnabled
		if needRead {
			b, err := service.ReadBufferedResponseBody(requestBody)
			if err != nil {
				return nil, fmt.Errorf("read claude request body failed: %w", err)
			}
			bodyBytes = b
			bodyReady = true

			bodyBytes, err = ApplyClaudeRequestBodySettings(c, info, bodyBytes)
			if err != nil {
				return nil, err
			}
		}

		ApplyClaudeResponseUsageSettings(c, info)
	}

	if bodyReady {
		requestBody = bytes.NewBuffer(bodyBytes)
	}

	resp, err := channel.DoApiRequest(a, c, info, requestBody)
	if err != nil {
		return resp, err
	}

	if info == nil || !info.ChannelOtherSettings.ThinkingSignatureRectifierEnabled || !bodyReady {
		return resp, nil
	}
	if resp == nil || resp.StatusCode < http.StatusBadRequest {
		return resp, nil
	}

	rectified, retryErr := tryRectifyThinkingSignature(c, info, a, resp, bodyBytes)
	if retryErr != nil {
		logger.LogWarn(c, "claude thinking signature rectifier abort: "+retryErr.Error())
		return resp, nil
	}
	if rectified != nil {
		return rectified, nil
	}
	return resp, nil
}

// RectifyOptions 控制 thinking signature 剥离重试的触发与阶段间判定策略。
type RectifyOptions struct {
	// RequireSignatureError 为 true 时，仅当响应体命中 signature 特征才启动/继续重试（原生 Claude 行为）；
	// 为 false 时，只要上游状态码 >= 400 即逐阶段剥离重放（Vertex/AWS 行为，不做错误消息字符匹配）。
	RequireSignatureError bool
}

// RectifyThinkingSignatureHTTP 对基于 *http.Response 的 Claude 上游执行两阶段 thinking 剥离重试，
// 供原生 Claude 及 Vertex / AWS（ApiKey 模式）等复用。
// originalResp 为首次请求得到的失败响应（其 Body 会被读取并复原，以便调用方在跳过重试时继续消费）；
// originalBody 为已缓存的原始请求体；replay 由调用方用自身适配器重发剥离后的请求体。
// 返回非 nil *http.Response 表示应改用重试得到的响应；返回 (nil, nil) 表示保持 originalResp。
func RectifyThinkingSignatureHTTP(
	c *gin.Context,
	originalBody []byte,
	originalResp *http.Response,
	opts RectifyOptions,
	replay func(stripped []byte) (*http.Response, error),
) (*http.Response, error) {
	respBody, err := service.ReadBufferedResponseBody(originalResp.Body)
	if err != nil {
		_ = originalResp.Body.Close()
		return nil, fmt.Errorf("read upstream response: %w", err)
	}
	_ = originalResp.Body.Close()
	// Restore the body so callers can still consume the original response if retry is skipped.
	originalResp.Body = io.NopCloser(bytes.NewBuffer(respBody))

	if opts.RequireSignatureError && !IsSignatureRelatedError(respBody) {
		return nil, nil
	}

	stages := []struct {
		name string
		fn   func([]byte) ([]byte, bool)
	}{
		{StripStageThinkingOnly, StripThinkingFromClaudeRequest},
		{StripStageThinkingAndTools, StripSignatureSensitiveBlocksFromClaudeRequest},
	}

	for _, stage := range stages {
		stripped, changed := stage.fn(originalBody)
		if !changed {
			continue
		}
		logger.LogDebug(c, "claude thinking signature rectifier retry stage=%s", stage.name)
		retryResp, retryErr := replay(stripped)
		if retryErr != nil {
			logger.LogWarn(c, fmt.Sprintf("claude rectifier stage=%s do request failed: %s", stage.name, retryErr.Error()))
			continue
		}
		if retryResp == nil {
			continue
		}
		if retryResp.StatusCode < http.StatusBadRequest {
			_ = originalResp.Body.Close()
			return retryResp, nil
		}
		// 失败响应消费掉，避免连接泄漏。
		retryBody, _ := service.ReadBufferedResponseBody(retryResp.Body)
		_ = retryResp.Body.Close()
		if opts.RequireSignatureError && !IsSignatureRelatedError(retryBody) {
			// 非 signature 错误，恢复 body 后让上层处理。
			retryResp.Body = io.NopCloser(bytes.NewBuffer(retryBody))
			_ = originalResp.Body.Close()
			return retryResp, nil
		}
		logger.LogWarn(c, fmt.Sprintf("claude rectifier stage=%s still error, status=%d", stage.name, retryResp.StatusCode))
	}

	return nil, nil
}

// tryRectifyThinkingSignature retries a Claude request after removing thinking/signature-sensitive blocks.
// A nil response with nil error means the caller should keep using the original upstream response.
func tryRectifyThinkingSignature(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	a *Adaptor,
	originalResp *http.Response,
	originalBody []byte,
) (*http.Response, error) {
	return RectifyThinkingSignatureHTTP(c, originalBody, originalResp,
		RectifyOptions{RequireSignatureError: true},
		func(stripped []byte) (*http.Response, error) {
			return channel.DoApiRequest(a, c, info, bytes.NewBuffer(stripped))
		})
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	info.FinalRequestRelayFormat = types.RelayFormatClaude
	if info.RelayFormat == types.RelayFormatOpenAIResponses && info.IsStream {
		return ClaudeResponsesStreamHandler(c, resp, info)
	}
	if info.IsStream {
		return ClaudeStreamHandler(c, resp, info)
	} else {
		return ClaudeHandler(c, resp, info)
	}
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
