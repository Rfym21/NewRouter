package dto

import (
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"new-router/relaykit/types"

	"golang.org/x/net/http/httpguts"
)

type ChannelSettings struct {
	TaskPluginKey             string `json:"task_plugin_key,omitempty"`
	ForceFormat               bool   `json:"force_format,omitempty"`
	ThinkingToContent         bool   `json:"thinking_to_content,omitempty"`
	Proxy                     string `json:"proxy"`
	PassThroughBodyEnabled    bool   `json:"pass_through_body_enabled,omitempty"`
	ResponsesWebSocketEnabled bool   `json:"responses_websocket_enabled,omitempty"`
	SystemPrompt              string `json:"system_prompt,omitempty"`
	SystemPromptOverride      bool   `json:"system_prompt_override,omitempty"`
	// TaskExtendPluginKeys lists the task plugins a New API channel (type 60)
	// is extended with. The upstream gateway may host many plugins, so the
	// channel serves every listed plugin's models while the request still pins
	// the executing plugin. TaskPluginKey remains the single type-61 binding
	// and stays valid on a New API channel as well.
	TaskExtendPluginKeys []string `json:"task_extend_plugin_keys,omitempty"`
	// EmptyResponseNoBillingEnabled inherits the global setting when nil and overrides it otherwise.
	EmptyResponseNoBillingEnabled *bool `json:"empty_response_no_billing_enabled,omitempty"`
	// AnonymizationEnabled inherits the global anonymization setting when nil and overrides it otherwise.
	AnonymizationEnabled *bool `json:"anonymization_enabled,omitempty"`
	// SensitiveCheckEnabled inherits the global prompt filter when nil and overrides it otherwise.
	SensitiveCheckEnabled *bool `json:"sensitive_check_enabled,omitempty"`
	// SensitiveMode selects the channel word-list strategy: inherit, modify, or override.
	SensitiveMode string `json:"sensitive_mode,omitempty"`
	// SensitiveAddedWords extends the global list in modify mode.
	SensitiveAddedWords []string `json:"sensitive_added_words,omitempty"`
	// SensitiveRemovedWords removes normalized global words in modify mode.
	SensitiveRemovedWords []string `json:"sensitive_removed_words,omitempty"`
	// SensitiveOverrideWords is the complete channel list in override mode.
	SensitiveOverrideWords []string `json:"sensitive_override_words,omitempty"`
	// UnifyRequestResponseModelNameEnabled rewrites response model fields back to the client-requested model name.
	// It is channel-agnostic: when enabled it applies to every upstream (OpenAI/Claude/Gemini response paths).
	UnifyRequestResponseModelNameEnabled bool `json:"unify_request_response_model_name_enabled,omitempty"`
	// HTTPProtocol controls outbound HTTP version negotiation for this channel.
	// Accepted values: "", "auto" (default), "http1".
	HTTPProtocol string `json:"http_protocol,omitempty"`
	// HTTP2ConnectionShards spreads HTTP/2 traffic across N independent transports
	// (1-8). Zero/unset means 1. Ignored when HTTPProtocol is "http1".
	HTTP2ConnectionShards int `json:"http2_connection_shards,omitempty"`
	// TLSInsecureSkipVerify accepts upstream certificates that fail
	// verification, such as self-signed ones, for this channel only.
	TLSInsecureSkipVerify bool `json:"tls_insecure_skip_verify,omitempty"`
}

// ShouldSkipEmptyResponseBilling resolves the channel override against the global setting.
func (s ChannelSettings) ShouldSkipEmptyResponseBilling(globalEnabled bool) bool {
	if s.EmptyResponseNoBillingEnabled == nil {
		return globalEnabled
	}
	return *s.EmptyResponseNoBillingEnabled
}

// ShouldCheckSensitive resolves the channel prompt-filter override against the global setting.
func (s ChannelSettings) ShouldCheckSensitive(globalEnabled bool) bool {
	if s.SensitiveCheckEnabled == nil {
		return globalEnabled
	}
	return *s.SensitiveCheckEnabled
}

// ShouldAnonymize resolves the channel anonymization override against the global setting.
// A nil AnonymizationEnabled inherits globalEnabled; an explicit true/false forces the result.
func (s ChannelSettings) ShouldAnonymize(globalEnabled bool) bool {
	if s.AnonymizationEnabled == nil {
		return globalEnabled
	}
	return *s.AnonymizationEnabled
}

// EffectiveSensitiveWords resolves the channel word-list strategy against the global list.
func (s ChannelSettings) EffectiveSensitiveWords(global []string) []string {
	switch s.SensitiveMode {
	case "override":
		return deduplicateSensitiveWords(s.SensitiveOverrideWords)
	case "modify":
		words := make(map[string]struct{}, len(global)+len(s.SensitiveAddedWords))
		for _, word := range global {
			if normalized := normalizeSensitiveWord(word); normalized != "" {
				words[normalized] = struct{}{}
			}
		}
		for _, word := range s.SensitiveRemovedWords {
			if normalized := normalizeSensitiveWord(word); normalized != "" {
				delete(words, normalized)
			}
		}
		for _, word := range s.SensitiveAddedWords {
			if normalized := normalizeSensitiveWord(word); normalized != "" {
				words[normalized] = struct{}{}
			}
		}

		resolved := make([]string, 0, len(words))
		for word := range words {
			resolved = append(resolved, word)
		}
		sort.Strings(resolved)
		return resolved
	default:
		return deduplicateSensitiveWords(global)
	}
}

func normalizeSensitiveWord(word string) string {
	return strings.ToLower(strings.TrimSpace(word))
}

func deduplicateSensitiveWords(words []string) []string {
	if len(words) == 0 {
		return nil
	}

	uniqueWords := make(map[string]struct{}, len(words))
	for _, word := range words {
		if normalized := normalizeSensitiveWord(word); normalized != "" {
			uniqueWords[normalized] = struct{}{}
		}
	}

	resolved := make([]string, 0, len(uniqueWords))
	for word := range uniqueWords {
		resolved = append(resolved, word)
	}
	sort.Strings(resolved)
	return resolved

}

// BindsTaskPlugin reports whether the channel is bound to the task plugin,
// either through the single binding or the New API extension list.
func (s ChannelSettings) BindsTaskPlugin(key string) bool {
	if key == "" {
		return false
	}
	return s.TaskPluginKey == key || slices.Contains(s.TaskExtendPluginKeys, key)
}

// TaskPluginBindings returns the sorted, de-duplicated set of task plugins the
// channel binds through either field, so callers can compare bindings as a set.
func (s ChannelSettings) TaskPluginBindings() []string {
	bindings := make([]string, 0, len(s.TaskExtendPluginKeys)+1)
	if s.TaskPluginKey != "" {
		bindings = append(bindings, s.TaskPluginKey)
	}
	for _, key := range s.TaskExtendPluginKeys {
		if key != "" && !slices.Contains(bindings, key) {
			bindings = append(bindings, key)
		}
	}
	slices.Sort(bindings)
	return bindings
}

const (
	HTTPProtocolAuto         = "auto"
	HTTPProtocolHTTP1        = "http1"
	MaxHTTP2ConnectionShards = 8
)

// ValidateHTTPTransport validates save-time HTTP transport channel settings.
func (s *ChannelSettings) ValidateHTTPTransport() error {
	if s == nil {
		return nil
	}
	protocol := strings.ToLower(strings.TrimSpace(s.HTTPProtocol))
	switch protocol {
	case "", HTTPProtocolAuto, HTTPProtocolHTTP1:
	default:
		return fmt.Errorf("invalid http_protocol: %s", s.HTTPProtocol)
	}
	if s.HTTP2ConnectionShards < 0 || s.HTTP2ConnectionShards > MaxHTTP2ConnectionShards {
		return fmt.Errorf("invalid http2_connection_shards: %d", s.HTTP2ConnectionShards)
	}
	if protocol == HTTPProtocolHTTP1 && s.HTTP2ConnectionShards > 1 {
		return fmt.Errorf("http2_connection_shards must be 1 when http_protocol is http1")
	}
	return nil
}

type VertexKeyType string

const (
	VertexKeyTypeJSON   VertexKeyType = "json"
	VertexKeyTypeAPIKey VertexKeyType = "api_key"
)

type AwsKeyType string

const (
	AwsKeyTypeAKSK   AwsKeyType = "ak_sk" // 默认
	AwsKeyTypeApiKey AwsKeyType = "api_key"
)

// ClaudeCodeSimulationConfig controls the configurable Claude Code request fingerprint.
type ClaudeCodeSimulationConfig struct {
	// Headers contains the complete simulated header set. Values may contain the {api_key} placeholder.
	Headers map[string]string `json:"headers"`
	// Metadata is encoded as the JSON string stored in metadata.user_id.
	Metadata map[string]string `json:"metadata"`
	// System contains the system blocks prepended to the original request system.
	System []any `json:"system"`
}

// Validate verifies that configured Claude Code simulation values can be sent upstream safely.
func (c *ClaudeCodeSimulationConfig) Validate() error {
	if c == nil {
		return nil
	}

	seenHeaders := make(map[string]struct{}, len(c.Headers))
	for name, value := range c.Headers {
		if !httpguts.ValidHeaderFieldName(name) {
			return fmt.Errorf("claude_code_simulation.headers contains invalid header name %q", name)
		}
		if !httpguts.ValidHeaderFieldValue(value) {
			return fmt.Errorf("claude_code_simulation.headers.%s contains an invalid header value", name)
		}
		normalizedName := strings.ToLower(name)
		if _, exists := seenHeaders[normalizedName]; exists {
			return fmt.Errorf("claude_code_simulation.headers contains duplicate header name %q", name)
		}
		seenHeaders[normalizedName] = struct{}{}
	}

	if c.Metadata != nil {
		deviceID, hasDeviceID := c.Metadata["device_id"]
		_, hasAccountUUID := c.Metadata["account_uuid"]
		sessionID, hasSessionID := c.Metadata["session_id"]
		if !hasDeviceID || strings.TrimSpace(deviceID) == "" {
			return fmt.Errorf("claude_code_simulation.metadata.device_id is required")
		}
		if !hasAccountUUID {
			return fmt.Errorf("claude_code_simulation.metadata.account_uuid is required")
		}
		if !hasSessionID || strings.TrimSpace(sessionID) == "" {
			return fmt.Errorf("claude_code_simulation.metadata.session_id is required")
		}
	}

	for i, block := range c.System {
		value := reflect.ValueOf(block)
		if !value.IsValid() || value.Kind() != reflect.Map || value.Type().Key().Kind() != reflect.String || value.Len() == 0 {
			return fmt.Errorf("claude_code_simulation.system[%d] must be a non-empty object", i)
		}
	}

	return nil
}

type ChannelOtherSettings struct {
	AzureResponsesVersion   string        `json:"azure_responses_version,omitempty"`
	VertexKeyType           VertexKeyType `json:"vertex_key_type,omitempty"` // "json" or "api_key"
	OpenRouterEnterprise    *bool         `json:"openrouter_enterprise,omitempty"`
	ClaudeBetaQuery         bool          `json:"claude_beta_query,omitempty"`          // Claude 渠道是否强制追加 ?beta=true
	AllowServiceTier        bool          `json:"allow_service_tier,omitempty"`         // 是否允许 service_tier 透传（默认过滤以避免额外计费）
	AllowInferenceGeo       bool          `json:"allow_inference_geo,omitempty"`        // 是否允许 inference_geo 透传（仅 Claude，默认过滤以满足数据驻留合规
	AllowSpeed              bool          `json:"allow_speed,omitempty"`                // 是否允许 speed 透传（仅 Claude，默认过滤以避免意外切换推理速度模式）
	AllowSafetyIdentifier   bool          `json:"allow_safety_identifier,omitempty"`    // 是否允许 safety_identifier 透传（默认过滤以保护用户隐私）
	DisableStore            bool          `json:"disable_store,omitempty"`              // 是否禁用 store 透传（默认允许透传，禁用后可能导致 Codex 无法使用）
	AllowIncludeObfuscation bool          `json:"allow_include_obfuscation,omitempty"`  // 是否允许 stream_options.include_obfuscation 透传（默认过滤以避免关闭流混淆保护）
	DisableTaskPollingSleep bool          `json:"disable_task_polling_sleep,omitempty"` // 是否跳过异步任务轮询间隔
	AwsKeyType              AwsKeyType    `json:"aws_key_type,omitempty"`
	// BodyFieldPassThrough controls channel-level request body field passthrough.
	// Explicit rules take precedence over the legacy fixed-field switches above.
	BodyFieldPassThrough []FieldPassThroughRule `json:"body_field_pass_through,omitempty"`
	// HeaderFieldPassThrough controls exact incoming request header passthrough.
	HeaderFieldPassThrough                []FieldPassThroughRule `json:"header_field_pass_through,omitempty"`
	UpstreamModelUpdateCheckEnabled       bool                   `json:"upstream_model_update_check_enabled,omitempty"`        // 是否检测上游模型更新
	UpstreamModelUpdateAutoSyncEnabled    bool                   `json:"upstream_model_update_auto_sync_enabled,omitempty"`    // 是否自动同步上游模型更新
	UpstreamModelUpdateLastCheckTime      int64                  `json:"upstream_model_update_last_check_time,omitempty"`      // 上次检测时间
	UpstreamModelUpdateLastDetectedModels []string               `json:"upstream_model_update_last_detected_models,omitempty"` // 上次检测到的可加入模型
	UpstreamModelUpdateLastRemovedModels  []string               `json:"upstream_model_update_last_removed_models,omitempty"`  // 上次检测到的可删除模型
	UpstreamModelUpdateIgnoredModels      []string               `json:"upstream_model_update_ignored_models,omitempty"`       // 手动忽略的模型
	AdvancedCustom                        *AdvancedCustomConfig  `json:"advanced_custom,omitempty"`

	// ForceCacheEnabled 渠道级强制缓存开关，仅对 Claude 上游（原生 Anthropic / Vertex Claude / AWS Claude）生效。
	// 开启后：若上游请求体已存在任何 cache_control 标记则保持原样；否则按 system(1)+messages(2)+tools(1)
	// 注入 ephemeral 缓存标记，同时将响应 usage 中的缓存创建/读取 token 归零并折算回 input_tokens。
	ForceCacheEnabled bool `json:"force_cache_enabled,omitempty"`
	// ForceCacheTTL 配合 ForceCacheEnabled，决定注入 cache_control 时的 TTL。"" / "5m" 为默认 5 分钟；"1h" 为 1 小时。
	ForceCacheTTL string `json:"force_cache_ttl,omitempty"`
	// ForceCache1hEnabled 渠道级 1h 强制缓存开关，仅对 Claude 上游生效。
	// 开启后将请求体中所有已存在的 cache_control 对象的 ttl 统一改写为 "1h"。与 ForceCacheEnabled 独立：
	// 同时开启时先按 ForceCacheTTL 注入，再统一改写为 1h。
	ForceCache1hEnabled bool `json:"force_cache_1h_enabled,omitempty"`
	// Cache1hBillAs5mEnabled 渠道级 "1h 缓存按 5m 计费" 开关，仅对 Claude 上游生效。
	// 开启后在响应 usage 中把 cache_creation.ephemeral_1h_input_tokens 累加到 ephemeral_5m_input_tokens 并置零；
	// 与 ForceCacheEnabled 同时开启时，强制缓存的整体归零优先（此时本开关无实际作用）。
	Cache1hBillAs5mEnabled bool `json:"cache_1h_bill_as_5m_enabled,omitempty"`
	// CacheReadMinRatioEnabled 渠道级 "缓存读写比例控制器" 开关，仅对 Claude 上游生效。
	// 开启后在响应 usage 中校验 cache_read 占 (读+写) 的比例是否低于 CacheReadMinRatio；低于则按阈值重新分配 read/write，
	// 使 read 占比恰好达到下限，并按比例缩放 cache_creation.ephemeral_5m/1h 子项。read+write 总量不变。
	CacheReadMinRatioEnabled bool `json:"cache_read_min_ratio_enabled,omitempty"`
	// CacheReadMinRatio 缓存读 token 占 (读+写) 的最低比例，取值 0-1。仅当 CacheReadMinRatioEnabled 且本字段 > 0 时生效。
	CacheReadMinRatio float64 `json:"cache_read_min_ratio,omitempty"`
	// ThinkingSignatureRectifierEnabled 渠道级 thinking signature 自动剥离重试开关，对以 Claude 为上游的渠道生效
	// （原生 Anthropic、Vertex Claude、AWS Claude 的 ApiKey 与 AKSK 模式）。
	// 开启后：上游返回 >= 400 时顺序尝试两个剥离阶段并重发，直到成功或两阶段均失败。触发条件因渠道而异：
	// 原生 Anthropic 额外要求响应体命中 signature 特征；Vertex 与 AWS 仅以状态码 >= 400 为触发条件。
	ThinkingSignatureRectifierEnabled bool `json:"thinking_signature_rectifier_enabled,omitempty"`
	// ClaudeCodeSimulationEnabled 渠道级 Claude Code 请求模拟开关，对 Claude 上游生效。
	// 开启后为请求体补齐 metadata.user_id、system 前缀和默认工具定义；原生 Anthropic 追加完整模拟请求头，
	// Vertex / AWS Claude 仅追加 anthropic-beta token。
	ClaudeCodeSimulationEnabled bool `json:"claude_code_simulation_enabled,omitempty"`
	// ClaudeCodeSimulation customizes simulated headers, metadata, and system blocks. Nil fields use defaults.
	ClaudeCodeSimulation *ClaudeCodeSimulationConfig `json:"claude_code_simulation,omitempty"`
	// ResponsesEncryptedContentRectifierEnabled 渠道级 Responses 加密推理内容自动剥离重试开关，
	// 对以 OpenAI / Azure 为上游的 Responses API 渠道生效。开启后：仅当上游返回 >= 400 且响应体命中加密内容错误特征时，
	// 剥离 input 中带 encrypted_content 的 reasoning item 后原参重放一次；成功则改用重试响应，失败则回退原始错误。
	ResponsesEncryptedContentRectifierEnabled bool `json:"responses_encrypted_content_rectifier_enabled,omitempty"`
	// OllamaOpenAIChat routes Ollama chat completions to the OpenAI-compatible
	// /v1/chat/completions endpoint. When unset, chat completions keep using
	// the native /api/chat protocol.
	OllamaOpenAIChat bool `json:"ollama_openai_chat,omitempty"`
	// ToolLossPolicy is a channel-level opt-in for request-phase conversion
	// rejection. Empty follows the default allow policy. Accepted values:
	// "", "allow", "safe", "strict".
	ToolLossPolicy string `json:"tool_loss_policy,omitempty"`
}

// FieldPassThroughRule declares whether a request body field or header may be passed upstream.
type FieldPassThroughRule struct {
	// Field identifies a JSON path for request bodies or an exact HTTP header name.
	Field string `json:"field"`
	// Enabled allows passthrough when true and applies the default deny behavior when false.
	Enabled bool `json:"enabled"`
	// Comment stores administrator-facing context without affecting relay behavior.
	Comment string `json:"comment,omitempty"`
}

// IsBodyFieldAllowed resolves an explicit body rule against the legacy field switches.
// JSON field names are case-sensitive, and the last duplicate rule wins.
func (s *ChannelOtherSettings) IsBodyFieldAllowed(field string) bool {
	if s != nil {
		matched := false
		allowed := false
		for _, rule := range s.BodyFieldPassThrough {
			if strings.TrimSpace(rule.Field) == field {
				matched = true
				allowed = rule.Enabled
			}
		}
		if matched {
			return allowed
		}
	}

	switch field {
	case "service_tier":
		return s != nil && s.AllowServiceTier
	case "inference_geo":
		return s != nil && s.AllowInferenceGeo
	case "speed":
		return s != nil && s.AllowSpeed
	case "store":
		return s == nil || !s.DisableStore
	case "safety_identifier":
		return s != nil && s.AllowSafetyIdentifier
	case "stream_options.include_obfuscation":
		return s != nil && s.AllowIncludeObfuscation
	default:
		return false
	}
}

func (s *ChannelOtherSettings) IsOpenRouterEnterprise() bool {
	if s == nil || s.OpenRouterEnterprise == nil {
		return false
	}
	return *s.OpenRouterEnterprise
}

// ValidateToolLossPolicy validates the channel-level request-phase tool-loss
// policy. Empty keeps the default allow policy.
func (s *ChannelOtherSettings) ValidateToolLossPolicy() error {
	if s == nil {
		return nil
	}
	switch strings.TrimSpace(s.ToolLossPolicy) {
	case "", string(types.ConversionLossPolicyAllow), string(types.ConversionLossPolicySafe), string(types.ConversionLossPolicyStrict):
		return nil
	default:
		return fmt.Errorf("invalid tool_loss_policy: %s", s.ToolLossPolicy)
	}
}

const (
	AdvancedCustomConverterSGLangRerank                = "jina_rerank_to_sglang"
	advancedCustomConverterNone                        = "none"
	advancedCustomConverterClaudeMessagesToOpenAIChat  = "anthropic_messages_to_openai_chat_completions"
	advancedCustomConverterOpenAIChatToClaudeMessages  = "openai_chat_completions_to_anthropic_messages"
	advancedCustomConverterOpenAIChatToOpenAIResponses = "openai_chat_completions_to_openai_responses"
	advancedCustomConverterOpenAIResponsesToOpenAIChat = "openai_responses_to_openai_chat_completions"
	advancedCustomConverterOpenAIResponsesToGemini     = "openai_responses_to_gemini_generate_content"
	advancedCustomConverterGeminiContentToOpenAIChat   = "gemini_generate_content_to_openai_chat_completions"
	advancedCustomConverterOpenAIChatToGeminiContent   = "openai_chat_completions_to_gemini_generate_content"
	// Same values as relayconvert.ConverterClaudeMessagesToOpenAIResponses and
	// relayconvert.ConverterGeminiContentToOpenAIResponses.
	advancedCustomConverterClaudeMessagesToOpenAIResponses = "claude_messages_to_openai_responses"
	advancedCustomConverterGeminiContentToOpenAIResponses  = "gemini_generate_content_to_openai_responses"
)

const (
	AdvancedCustomAuthTypeNone   = "none"
	AdvancedCustomAuthTypeHeader = "header"
	AdvancedCustomAuthTypeQuery  = "query"
)

type AdvancedCustomConfig struct {
	Routes []AdvancedCustomRoute `json:"advanced_routes,omitempty"`
}

type AdvancedCustomRoute struct {
	IncomingPath           string                   `json:"incoming_path,omitempty"`
	UpstreamPath           string                   `json:"upstream_path,omitempty"`
	Converter              string                   `json:"converter,omitempty"`
	Models                 []string                 `json:"models,omitempty"`
	Auth                   *AdvancedCustomRouteAuth `json:"auth,omitempty"`
	PassThroughBodyEnabled bool                     `json:"pass_through_body_enabled,omitempty"`
}

// SupportsPassThroughBody reports whether the route converter leaves the request body untouched.
func (r AdvancedCustomRoute) SupportsPassThroughBody() bool {
	switch strings.TrimSpace(r.Converter) {
	case "", advancedCustomConverterNone, AdvancedCustomConverterSGLangRerank:
		return true
	default:
		return false
	}
}

type AdvancedCustomRouteAuth struct {
	Type  string `json:"type,omitempty"`
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

const (
	advancedCustomModelPlaceholder = "{model}"
	advancedCustomModelRegexPrefix = "re:"
)

const (
	advancedCustomEndpointPathOpenAIChat             = "/v1/chat/completions"
	advancedCustomEndpointPathOpenAIResponses        = "/v1/responses"
	advancedCustomEndpointPathOpenAIResponsesCompact = "/v1/responses/compact"
	advancedCustomEndpointPathOpenAIAlphaSearch      = "/v1/alpha/search"
	advancedCustomEndpointPathClaudeMessages         = "/v1/messages"
	advancedCustomEndpointPathJinaRerank             = "/v1/rerank"
	advancedCustomEndpointPathImageGeneration        = "/v1/images/generations"
	advancedCustomEndpointPathEmbeddings             = "/v1/embeddings"
)

const (
	// AdvancedCustomModelListPath identifies the optional OpenAI Models discovery route.
	AdvancedCustomModelListPath = "/v1/models"
	// AdvancedCustomBalancePath identifies the optional balance lookup route used by channel management.
	AdvancedCustomBalancePath = "/v1/dashboard/billing/credit_grants"
)

// IsNative reports whether the route forwards requests without protocol
// conversion. Validate normalizes an empty converter to none, but callers may
// see configurations that were never saved.
func (r AdvancedCustomRoute) IsNative() bool {
	converter := strings.TrimSpace(r.Converter)
	return converter == "" || converter == advancedCustomConverterNone
}

// MatchPath returns the first route whose IncomingPath matches requestPath.
// Matching mirrors the relay adaptor: exact match, {model} placeholder, and
// :generateContent <-> :streamGenerateContent equivalence.
func (c *AdvancedCustomConfig) MatchPath(requestPath string) (AdvancedCustomRoute, bool) {
	if c == nil {
		return AdvancedCustomRoute{}, false
	}
	for _, route := range c.Routes {
		if matchAdvancedCustomIncomingPath(strings.TrimSpace(route.IncomingPath), requestPath) {
			return route, true
		}
	}
	return AdvancedCustomRoute{}, false
}

// MatchPathForModel returns the first route whose IncomingPath and Models match.
// An empty Models list is a catch-all fallback for that incoming path.
func (c *AdvancedCustomConfig) MatchPathForModel(requestPath string, model string) (AdvancedCustomRoute, bool) {
	if c == nil {
		return AdvancedCustomRoute{}, false
	}
	model = strings.TrimSpace(model)
	for _, route := range c.Routes {
		if matchAdvancedCustomIncomingPath(strings.TrimSpace(route.IncomingPath), requestPath) &&
			matchAdvancedCustomRouteModel(route.Models, model) {
			return route, true
		}
	}
	return AdvancedCustomRoute{}, false
}

// ModelListRoute returns the explicitly configured OpenAI Models discovery route.
// Template routes that merely happen to match /v1/models are not discovery routes.
func (c *AdvancedCustomConfig) ModelListRoute() (AdvancedCustomRoute, bool) {
	if c == nil {
		return AdvancedCustomRoute{}, false
	}
	for _, route := range c.Routes {
		if strings.TrimSpace(route.IncomingPath) == AdvancedCustomModelListPath {
			return route, true
		}
	}
	return AdvancedCustomRoute{}, false
}

// BalanceRoute returns the explicitly configured channel-management balance route.
func (c *AdvancedCustomConfig) BalanceRoute() (AdvancedCustomRoute, bool) {
	if c == nil {
		return AdvancedCustomRoute{}, false
	}
	for _, route := range c.Routes {
		if strings.TrimSpace(route.IncomingPath) == AdvancedCustomBalancePath {
			return route, true
		}
	}
	return AdvancedCustomRoute{}, false
}

// SupportsPath reports whether any route matches requestPath.
func (c *AdvancedCustomConfig) SupportsPath(requestPath string) bool {
	_, ok := c.MatchPath(requestPath)
	return ok
}

// SupportsPathForModel reports whether any route matches requestPath and model.
func (c *AdvancedCustomConfig) SupportsPathForModel(requestPath string, model string) bool {
	_, ok := c.MatchPathForModel(requestPath, model)
	return ok
}

func (c *AdvancedCustomConfig) SupportedEndpointTypesForModel(model string) []types.EndpointType {
	if c == nil {
		return nil
	}
	model = strings.TrimSpace(model)
	endpoints := make([]types.EndpointType, 0, len(c.Routes))
	seen := make(map[types.EndpointType]struct{}, len(c.Routes))
	for _, route := range c.Routes {
		if !matchAdvancedCustomRouteModel(route.Models, model) {
			continue
		}
		endpointType, ok := advancedCustomEndpointTypeFromIncomingPath(strings.TrimSpace(route.IncomingPath))
		if !ok {
			continue
		}
		if _, exists := seen[endpointType]; exists {
			continue
		}
		seen[endpointType] = struct{}{}
		endpoints = append(endpoints, endpointType)
	}
	return endpoints
}

func advancedCustomEndpointTypeFromIncomingPath(incomingPath string) (types.EndpointType, bool) {
	switch incomingPath {
	case advancedCustomEndpointPathOpenAIChat:
		return types.EndpointTypeOpenAI, true
	case advancedCustomEndpointPathOpenAIResponses:
		return types.EndpointTypeOpenAIResponse, true
	case advancedCustomEndpointPathOpenAIResponsesCompact:
		return types.EndpointTypeOpenAIResponseCompact, true
	case advancedCustomEndpointPathOpenAIAlphaSearch:
		return types.EndpointTypeOpenAIAlphaSearch, true
	case advancedCustomEndpointPathClaudeMessages:
		return types.EndpointTypeAnthropic, true
	case advancedCustomEndpointPathJinaRerank:
		return types.EndpointTypeJinaRerank, true
	case advancedCustomEndpointPathImageGeneration:
		return types.EndpointTypeImageGeneration, true
	case advancedCustomEndpointPathEmbeddings:
		return types.EndpointTypeEmbeddings, true
	default:
		if isAdvancedCustomGeminiIncomingPath(incomingPath) {
			return types.EndpointTypeGemini, true
		}
		return "", false
	}
}

func isAdvancedCustomGeminiIncomingPath(incomingPath string) bool {
	if !strings.HasPrefix(incomingPath, "/v1beta/models/") {
		return false
	}
	return strings.Contains(incomingPath, ":generateContent") || strings.Contains(incomingPath, ":streamGenerateContent")
}

func matchAdvancedCustomRouteModel(models []string, model string) bool {
	normalizedModels := normalizeAdvancedCustomRouteModels(models)
	if len(normalizedModels) == 0 {
		return true
	}
	for _, allowedModel := range normalizedModels {
		if matchAdvancedCustomRouteModelRule(allowedModel, model) {
			return true
		}
	}
	return false
}

// advancedCustomModelRegexCache caches compiled route model patterns. Route model
// matching runs on the request hot path (distributor affinity, ability filtering,
// channel cache filtering, adaptor resolve), so patterns must not be recompiled per
// request. Invalid patterns are cached as nil to avoid recompiling them as well.
var advancedCustomModelRegexCache sync.Map // pattern string -> *regexp.Regexp (nil when invalid)

func compileAdvancedCustomModelRegex(pattern string) *regexp.Regexp {
	if cached, ok := advancedCustomModelRegexCache.Load(pattern); ok {
		re, _ := cached.(*regexp.Regexp)
		return re
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		re = nil
	}
	advancedCustomModelRegexCache.Store(pattern, re)
	return re
}

func matchAdvancedCustomRouteModelRule(rule string, model string) bool {
	if !strings.HasPrefix(rule, advancedCustomModelRegexPrefix) {
		return rule == model
	}
	pattern := strings.TrimPrefix(rule, advancedCustomModelRegexPrefix)
	if pattern == "" {
		return false
	}
	re := compileAdvancedCustomModelRegex(pattern)
	return re != nil && re.MatchString(model)
}

func matchAdvancedCustomIncomingPath(configuredPath string, requestPath string) bool {
	if matchAdvancedCustomIncomingPathTemplate(configuredPath, requestPath) {
		return true
	}
	if strings.Contains(configuredPath, ":generateContent") {
		streamPath := strings.Replace(configuredPath, ":generateContent", ":streamGenerateContent", 1)
		return matchAdvancedCustomIncomingPathTemplate(streamPath, requestPath)
	}
	return false
}

func matchAdvancedCustomIncomingPathTemplate(configuredPath string, requestPath string) bool {
	if !strings.Contains(configuredPath, advancedCustomModelPlaceholder) {
		return configuredPath == requestPath
	}

	parts := strings.Split(configuredPath, advancedCustomModelPlaceholder)
	if len(parts) != 2 {
		return false
	}
	if !strings.HasPrefix(requestPath, parts[0]) || !strings.HasSuffix(requestPath, parts[1]) {
		return false
	}

	model := strings.TrimSuffix(strings.TrimPrefix(requestPath, parts[0]), parts[1])
	return model != "" && !strings.Contains(model, "/")
}

func IsAdvancedCustomConverterAllowed(converter string) bool {
	switch converter {
	case advancedCustomConverterNone,
		AdvancedCustomConverterSGLangRerank,
		advancedCustomConverterClaudeMessagesToOpenAIChat,
		advancedCustomConverterOpenAIChatToClaudeMessages,
		advancedCustomConverterOpenAIChatToOpenAIResponses,
		advancedCustomConverterOpenAIResponsesToOpenAIChat,
		advancedCustomConverterOpenAIResponsesToGemini,
		advancedCustomConverterGeminiContentToOpenAIChat,
		advancedCustomConverterOpenAIChatToGeminiContent,
		advancedCustomConverterClaudeMessagesToOpenAIResponses,
		advancedCustomConverterGeminiContentToOpenAIResponses:
		return true
	default:
		return false
	}
}

func (c *AdvancedCustomConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("advanced_custom is required")
	}
	if len(c.Routes) == 0 {
		return fmt.Errorf("advanced_custom requires at least one route")
	}

	paths := make(map[string]*advancedCustomPathModelState, len(c.Routes))
	modelListRouteIndex := -1
	balanceRouteIndex := -1
	for i := range c.Routes {
		route := c.Routes[i]
		route.IncomingPath = strings.TrimSpace(route.IncomingPath)
		upstreamPath := strings.TrimSpace(route.UpstreamPath)
		route.Converter = strings.TrimSpace(route.Converter)
		if route.Converter == "" {
			route.Converter = advancedCustomConverterNone
		}

		if route.IncomingPath == "" {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].incoming_path is required", i)
		}
		if !strings.HasPrefix(route.IncomingPath, "/") {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].incoming_path must start with /", i)
		}
		if strings.Contains(route.IncomingPath, "?") {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].incoming_path must not include query", i)
		}
		if route.IncomingPath == AdvancedCustomModelListPath || route.IncomingPath == AdvancedCustomBalancePath {
			managementRouteName := route.IncomingPath
			previousIndex := modelListRouteIndex
			if route.IncomingPath == AdvancedCustomBalancePath {
				previousIndex = balanceRouteIndex
			}
			if previousIndex >= 0 {
				return fmt.Errorf("advanced_custom.advanced_routes[%d] duplicates the %s route at advanced_routes[%d]", i, managementRouteName, previousIndex)
			}
			if route.IncomingPath == AdvancedCustomModelListPath {
				modelListRouteIndex = i
			} else {
				balanceRouteIndex = i
			}
			if len(normalizeAdvancedCustomRouteModels(route.Models)) > 0 {
				return fmt.Errorf("advanced_custom.advanced_routes[%d].models must be empty for %s", i, managementRouteName)
			}
			if route.Converter != advancedCustomConverterNone {
				return fmt.Errorf("advanced_custom.advanced_routes[%d].converter must be none for %s", i, managementRouteName)
			}
			if strings.Contains(upstreamPath, advancedCustomModelPlaceholder) {
				return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path must not contain %s for %s", i, advancedCustomModelPlaceholder, managementRouteName)
			}
			if route.PassThroughBodyEnabled {
				return fmt.Errorf("advanced_custom.advanced_routes[%d].pass_through_body_enabled must be false for %s", i, managementRouteName)
			}
		}
		if err := validateAdvancedCustomRouteModels(i, route.IncomingPath, route.Models, paths); err != nil {
			return err
		}

		if upstreamPath == "" {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path is required", i)
		}
		if err := validateAdvancedCustomUpstreamTarget(i, upstreamPath); err != nil {
			return err
		}

		if !IsAdvancedCustomConverterAllowed(route.Converter) {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].converter is not registered: %s", i, route.Converter)
		}
		if err := validateAdvancedCustomConverterPath(i, route.IncomingPath, route.Converter); err != nil {
			return err
		}
		if route.PassThroughBodyEnabled && !route.SupportsPassThroughBody() {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].pass_through_body_enabled requires converter none: %s", i, route.Converter)
		}
		if err := validateAdvancedCustomRouteAuth(i, route.Auth); err != nil {
			return err
		}
	}

	return nil
}

type advancedCustomPathModelState struct {
	catchAllIndex int
	modelIndexes  map[string]int
}

func validateAdvancedCustomRouteModels(index int, incomingPath string, models []string, paths map[string]*advancedCustomPathModelState) error {
	state := paths[incomingPath]
	if state == nil {
		state = &advancedCustomPathModelState{
			catchAllIndex: -1,
			modelIndexes:  make(map[string]int),
		}
		paths[incomingPath] = state
	}

	normalizedModels := normalizeAdvancedCustomRouteModels(models)
	if len(normalizedModels) == 0 {
		if state.catchAllIndex >= 0 {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].models catch-all already exists for incoming_path: %s", index, incomingPath)
		}
		state.catchAllIndex = index
		return nil
	}

	if state.catchAllIndex >= 0 {
		return fmt.Errorf("advanced_custom.advanced_routes[%d].models catch-all route must be last for incoming_path: %s", index, incomingPath)
	}

	seenInRoute := make(map[string]struct{}, len(normalizedModels))
	for _, model := range normalizedModels {
		if err := validateAdvancedCustomRouteModelRule(index, incomingPath, model); err != nil {
			return err
		}
		if _, exists := seenInRoute[model]; exists {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].models contains duplicate model for incoming_path %s: %s", index, incomingPath, model)
		}
		seenInRoute[model] = struct{}{}
		if existingIndex, exists := state.modelIndexes[model]; exists {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].models overlaps with advanced_routes[%d] for incoming_path %s: %s", index, existingIndex, incomingPath, model)
		}
		state.modelIndexes[model] = index
	}
	return nil
}

func validateAdvancedCustomRouteModelRule(index int, incomingPath string, model string) error {
	if !strings.HasPrefix(model, advancedCustomModelRegexPrefix) {
		return nil
	}
	pattern := strings.TrimPrefix(model, advancedCustomModelRegexPrefix)
	if pattern == "" {
		return fmt.Errorf("advanced_custom.advanced_routes[%d].models regex is empty for incoming_path %s: %s", index, incomingPath, model)
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("advanced_custom.advanced_routes[%d].models regex is invalid for incoming_path %s: %s", index, incomingPath, model)
	}
	return nil
}

func normalizeAdvancedCustomRouteModels(models []string) []string {
	if len(models) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model != "" {
			normalized = append(normalized, model)
		}
	}
	return normalized
}

func validateAdvancedCustomUpstreamTarget(index int, upstreamPath string) error {
	if strings.HasPrefix(upstreamPath, "/") {
		if strings.HasPrefix(upstreamPath, "//") {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path must be a full URL or a path starting with /", index)
		}
		return nil
	}

	parsedURL, err := url.Parse(upstreamPath)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path must be a full URL or a path starting with /", index)
	}
	if !strings.EqualFold(parsedURL.Scheme, "http") && !strings.EqualFold(parsedURL.Scheme, "https") {
		return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path must use http or https", index)
	}
	return nil
}

func validateAdvancedCustomConverterPath(index int, incomingPath string, converter string) error {
	if converter == AdvancedCustomConverterSGLangRerank && (incomingPath == "/v1/rerank" || incomingPath == "/rerank") {
		return nil
	}
	if incomingPath == advancedCustomEndpointPathOpenAIAlphaSearch {
		if converter == advancedCustomConverterNone {
			return nil
		}
		return fmt.Errorf("advanced_custom.advanced_routes[%d].converter does not match incoming_path: %s", index, converter)
	}
	switch converter {
	case advancedCustomConverterNone:
		return nil
	case advancedCustomConverterClaudeMessagesToOpenAIChat,
		advancedCustomConverterClaudeMessagesToOpenAIResponses:
		if incomingPath == "/v1/messages" {
			return nil
		}
	case advancedCustomConverterOpenAIChatToClaudeMessages,
		advancedCustomConverterOpenAIChatToOpenAIResponses,
		advancedCustomConverterOpenAIChatToGeminiContent:
		if incomingPath == "/v1/chat/completions" {
			return nil
		}
	case advancedCustomConverterOpenAIResponsesToOpenAIChat:
		if incomingPath == "/v1/responses" {
			return nil
		}
	case advancedCustomConverterOpenAIResponsesToGemini:
		if incomingPath == "/v1/responses" {
			return nil
		}
	case advancedCustomConverterGeminiContentToOpenAIChat,
		advancedCustomConverterGeminiContentToOpenAIResponses:
		if strings.Contains(incomingPath, ":generateContent") || strings.Contains(incomingPath, ":streamGenerateContent") {
			return nil
		}
	}
	return fmt.Errorf("advanced_custom.advanced_routes[%d].converter does not match incoming_path: %s", index, converter)
}

func validateAdvancedCustomRouteAuth(index int, auth *AdvancedCustomRouteAuth) error {
	if auth == nil {
		return nil
	}
	authType := strings.TrimSpace(auth.Type)
	switch authType {
	case AdvancedCustomAuthTypeNone:
		return nil
	case AdvancedCustomAuthTypeHeader, AdvancedCustomAuthTypeQuery:
		if strings.TrimSpace(auth.Name) == "" {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].auth.name is required", index)
		}
		if strings.TrimSpace(auth.Value) == "" {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].auth.value is required", index)
		}
		return nil
	default:
		return fmt.Errorf("advanced_custom.advanced_routes[%d].auth.type is invalid: %s", index, auth.Type)
	}
}
