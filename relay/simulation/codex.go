package simulation

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	relaycommon "new-router/relay/common"
	"new-router/relaykit/dto"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	codexInstallationID = "39faaa95-1437-46d2-8f53-100e5d157bfb"
	codexSessionID      = "01a1dec3-6e18-7193-a48f-e1c4667583c5"
	codexTurnID         = "01a1dec3-6e31-77a2-8afe-c49d875185a6"
	codexContextWindow  = "01a1dec3-6e1c-76c1-be3e-1befdfc5c5a7"
	codexVersion        = "0.167.0"
	codexOriginator     = "codex-tui"
	codexUserAgent      = "codex-tui/" + codexVersion + " (Windows 10.0.26340; x86_64) WindowsTerminal (codex-tui; " + codexVersion + ")"
	codexBetaFeatures   = "remote_compaction_v2"
)

func defaultCodexSimulationMetadata() map[string]any {
	return map[string]any{
		"installation_id":                codexInstallationID,
		"session_id":                     codexSessionID,
		"thread_id":                      codexSessionID,
		"turn_id":                        codexTurnID,
		"root_turn_id":                   codexTurnID,
		"window_id":                      codexSessionID + ":0",
		"window_number":                  float64(0),
		"context_window_id":              codexContextWindow,
		"agent_name":                     "/root",
		"request_kind":                   "turn",
		"history_initialization":         "new",
		"thread_source":                  "user",
		"turn_trigger":                   "user",
		"sandbox":                        "none",
		"sandbox_mode":                   "read-only",
		"auto_review_enabled":            false,
		"node_repl_auto_review_required": false,
		"node_repl_disabled":             false,
		"turn_started_at_unix_ms":        float64(1794740237086),
		"analytics_enabled":              true,
	}
}

func defaultCodexSimulationHeaders() map[string]string {
	return map[string]string{
		"accept":                "{accept}",
		"content-type":          "application/json",
		"originator":            codexOriginator,
		"user-agent":            codexUserAgent,
		"session-id":            "{session_id}",
		"thread-id":             "{thread_id}",
		"x-client-request-id":   "{thread_id}",
		"x-codex-window-id":     "{window_id}",
		"x-codex-turn-metadata": "{turn_metadata}",
		"x-codex-beta-features": codexBetaFeatures,
	}
}

func defaultCodexSimulationSystem() []any {
	return []any{
		map[string]any{
			"type": "message",
			"role": "developer",
			"content": []any{
				map[string]any{
					"type": "input_text",
					"text": "You are Codex, a coding agent based on GPT-5. You and the user share one workspace, and your job is to collaborate with them until their goal is genuinely handled.",
				},
			},
		},
	}
}

func codexSimulationMetadata(info *relaycommon.RelayInfo, config *dto.CodexSimulationConfig) map[string]any {
	metadata := defaultCodexSimulationMetadata()
	if config != nil {
		for key, value := range config.Metadata {
			metadata[key] = value
		}
	}

	sessionID := codexStringValue(metadata["session_id"])
	if sessionID == "" {
		sessionID = codexSessionID
		metadata["session_id"] = sessionID
	}
	if codexStringValue(metadata["thread_id"]) == "" {
		metadata["thread_id"] = sessionID
	}
	if codexStringValue(metadata["window_id"]) == "" {
		metadata["window_id"] = sessionID + ":0"
	}
	if codexStringValue(metadata["turn_id"]) == "" {
		metadata["turn_id"] = codexTurnID
	}
	if codexStringValue(metadata["root_turn_id"]) == "" {
		metadata["root_turn_id"] = metadata["turn_id"]
	}
	if codexStringValue(metadata["installation_id"]) == "" {
		metadata["installation_id"] = codexInstallationID
	}
	if info != nil {
		if model := strings.TrimSpace(info.UpstreamModelName); model != "" {
			if _, exists := metadata["model"]; !exists {
				metadata["model"] = model
			}
		}
		if effort := strings.TrimSpace(info.ReasoningEffort); effort != "" {
			if _, exists := metadata["reasoning_effort"]; !exists {
				metadata["reasoning_effort"] = effort
			}
		}
	}
	return metadata
}

func codexStringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func codexTurnMetadataJSON(metadata map[string]any) string {
	raw, err := marshalNoEscape(metadata)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

// marshalNoEscape keeps the turn metadata header compact and free of HTML escaping.
func marshalNoEscape(value any) ([]byte, error) {
	var builder strings.Builder
	encoder := json.NewEncoder(&builder)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return []byte(strings.TrimSpace(builder.String())), nil
}

func codexSimulationHeaders(info *relaycommon.RelayInfo, config *dto.CodexSimulationConfig) map[string]string {
	metadata := codexSimulationMetadata(info, config)
	headers := defaultCodexSimulationHeaders()
	if config != nil && config.Headers != nil {
		headers = config.Headers
	}

	accept := "application/json"
	if info != nil && info.IsStream {
		accept = "text/event-stream"
	}

	replacements := map[string]string{
		"{accept}":          accept,
		"{session_id}":      codexStringValue(metadata["session_id"]),
		"{thread_id}":       codexStringValue(metadata["thread_id"]),
		"{installation_id}": codexStringValue(metadata["installation_id"]),
		"{window_id}":       codexStringValue(metadata["window_id"]),
		"{turn_id}":         codexStringValue(metadata["turn_id"]),
		"{turn_metadata}":   codexTurnMetadataJSON(metadata),
	}
	if info != nil {
		replacements["{api_key}"] = info.ApiKey
	}

	resolved := make(map[string]string, len(headers))
	for name, value := range headers {
		for placeholder, replacement := range replacements {
			value = strings.ReplaceAll(value, placeholder, sanitizeHeaderValue(replacement))
		}
		resolved[name] = value
	}
	return resolved
}

// sanitizeHeaderValue strips characters net/http refuses to write in a header.
func sanitizeHeaderValue(value string) string {
	return strings.Map(func(character rune) rune {
		if character == '\r' || character == '\n' || character == 0 {
			return -1
		}
		return character
	}, value)
}

func applyCodexSimulationHeaders(req *http.Header, info *relaycommon.RelayInfo, config *dto.CodexSimulationConfig) {
	if req == nil {
		return
	}
	headers := codexSimulationHeaders(info, config)

	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, rawName := range keys {
		name := strings.TrimSpace(rawName)
		if name == "" || isAuthHeader(name) {
			continue
		}
		value := headers[rawName]
		if strings.EqualFold(name, "x-codex-beta-features") {
			value = mergeCommaHeader(req.Get(name), value)
		}
		req.Set(name, value)
	}
}

func codexClientMetadata(metadata map[string]any) map[string]any {
	return map[string]any{
		"x-codex-installation-id": codexStringValue(metadata["installation_id"]),
		"session_id":              codexStringValue(metadata["session_id"]),
		"thread_id":               codexStringValue(metadata["thread_id"]),
		"x-codex-window-id":       codexStringValue(metadata["window_id"]),
		"turn_id":                 codexStringValue(metadata["turn_id"]),
		"x-codex-turn-metadata":   codexTurnMetadataJSON(metadata),
	}
}

func applyCodexSimulationBody(jsonData []byte, info *relaycommon.RelayInfo, config *dto.CodexSimulationConfig) ([]byte, error) {
	if len(jsonData) == 0 || !gjson.ValidBytes(jsonData) {
		return jsonData, nil
	}
	root := gjson.ParseBytes(jsonData)
	if !root.IsObject() {
		return jsonData, nil
	}

	metadata := codexSimulationMetadata(info, config)
	var err error

	clientMetadata, err := marshalNoEscape(codexClientMetadata(metadata))
	if err != nil {
		return nil, err
	}
	jsonData, err = sjson.SetRawBytes(jsonData, "client_metadata", clientMetadata)
	if err != nil {
		return nil, err
	}

	if info == nil || !info.ChannelOtherSettings.DisableStore {
		jsonData, err = sjson.SetRawBytes(jsonData, "store", []byte("false"))
		if err != nil {
			return nil, err
		}
	}
	if info != nil && info.IsStream {
		jsonData, err = sjson.SetRawBytes(jsonData, "stream", []byte("true"))
		if err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(gjson.GetBytes(jsonData, "prompt_cache_key").String()) == "" {
		jsonData, err = sjson.SetBytes(jsonData, "prompt_cache_key", codexStringValue(metadata["session_id"]))
		if err != nil {
			return nil, err
		}
	}
	if !gjson.GetBytes(jsonData, "parallel_tool_calls").Exists() {
		jsonData, err = sjson.SetRawBytes(jsonData, "parallel_tool_calls", []byte("true"))
		if err != nil {
			return nil, err
		}
	}
	if !gjson.GetBytes(jsonData, "tool_choice").Exists() {
		jsonData, err = sjson.SetRawBytes(jsonData, "tool_choice", []byte(`"auto"`))
		if err != nil {
			return nil, err
		}
	}
	if !gjson.GetBytes(jsonData, "text").Exists() {
		jsonData, err = sjson.SetRawBytes(jsonData, "text", []byte(`{"verbosity":"low"}`))
		if err != nil {
			return nil, err
		}
	}
	jsonData, err = ensureCodexEncryptedReasoning(jsonData)
	if err != nil {
		return nil, err
	}
	return ensureCodexDeveloperInput(jsonData, config)
}

func ensureCodexEncryptedReasoning(jsonData []byte) ([]byte, error) {
	include := gjson.GetBytes(jsonData, "include")
	if !include.Exists() || !include.IsArray() {
		return sjson.SetRawBytes(jsonData, "include", []byte(`["reasoning.encrypted_content"]`))
	}
	for _, item := range include.Array() {
		if item.Type == gjson.String && item.String() == "reasoning.encrypted_content" {
			return jsonData, nil
		}
	}
	return sjson.SetRawBytes(jsonData, "include.-1", []byte(`"reasoning.encrypted_content"`))
}

func ensureCodexDeveloperInput(jsonData []byte, config *dto.CodexSimulationConfig) ([]byte, error) {
	blocks := defaultCodexSimulationSystem()
	usingDefault := config == nil || config.System == nil
	if !usingDefault {
		blocks = make([]any, len(config.System))
		copy(blocks, config.System)
	}
	if len(blocks) == 0 {
		return jsonData, nil
	}

	input := gjson.GetBytes(jsonData, "input")
	switch {
	case input.IsArray():
		items := input.Array()
		if len(items) > 0 && isCodexDeveloperMessage(items[0]) {
			return jsonData, nil
		}
		prefix, err := marshalNoEscape(blocks)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return sjson.SetRawBytes(jsonData, "input", prefix)
		}
		existing := []byte(input.Raw)
		if len(prefix) < 2 || len(existing) < 2 {
			return jsonData, nil
		}
		merged := make([]byte, 0, len(prefix)+len(existing))
		merged = append(merged, prefix[:len(prefix)-1]...)
		merged = append(merged, ',')
		merged = append(merged, existing[1:]...)
		return sjson.SetRawBytes(jsonData, "input", merged)
	case input.Type == gjson.String:
		if usingDefault && isCodexDeveloperText(input.String()) {
			return jsonData, nil
		}
		message := map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": input.String()},
			},
		}
		items := append(append([]any{}, blocks...), message)
		raw, err := marshalNoEscape(items)
		if err != nil {
			return nil, err
		}
		return sjson.SetRawBytes(jsonData, "input", raw)
	case !input.Exists():
		raw, err := marshalNoEscape(blocks)
		if err != nil {
			return nil, err
		}
		return sjson.SetRawBytes(jsonData, "input", raw)
	default:
		return jsonData, nil
	}
}

func isCodexDeveloperMessage(item gjson.Result) bool {
	if !item.IsObject() {
		return false
	}
	if item.Get("role").String() != "developer" {
		return false
	}
	content := item.Get("content")
	if content.Type == gjson.String {
		return isCodexDeveloperText(content.String())
	}
	if content.IsArray() {
		for _, part := range content.Array() {
			if isCodexDeveloperText(part.Get("text").String()) {
				return true
			}
		}
	}
	return false
}

func isCodexDeveloperText(text string) bool {
	return strings.Contains(text, "You are Codex")
}

// ApplyCodexSimulationHeaders applies the Codex CLI header set, preserving authentication headers.
func ApplyCodexSimulationHeaders(req *http.Header, info *relaycommon.RelayInfo, config *dto.CodexSimulationConfig) {
	applyCodexSimulationHeaders(req, info, config)
}

// ApplyCodexSimulationBody applies Codex CLI metadata and Responses body defaults.
func ApplyCodexSimulationBody(jsonData []byte, info *relaycommon.RelayInfo, config *dto.CodexSimulationConfig) ([]byte, error) {
	return applyCodexSimulationBody(jsonData, info, config)
}

// CodexClientMetadata returns the simulated client_metadata object.
func CodexClientMetadata(info *relaycommon.RelayInfo, config *dto.CodexSimulationConfig) map[string]any {
	return codexClientMetadata(codexSimulationMetadata(info, config))
}

// CodexTurnMetadataJSON returns the simulated x-codex-turn-metadata header value.
func CodexTurnMetadataJSON(info *relaycommon.RelayInfo, config *dto.CodexSimulationConfig) string {
	return codexTurnMetadataJSON(codexSimulationMetadata(info, config))
}
