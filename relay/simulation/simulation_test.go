package simulation

import (
	"net/http"
	"strings"
	"testing"

	"new-router/common"
	"new-router/constant"
	relaycommon "new-router/relay/common"
	relayconstant "new-router/relay/constant"
	"new-router/relaykit/dto"
	"new-router/relaykit/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeCodeDefaultsMatchCurrentCLI(t *testing.T) {
	headers := defaultClaudeCodeSimulationHeaders()
	assert.Equal(t, "claude-cli/2.1.325 (external, cli)", headers["user-agent"])
	assert.Equal(t, "0.128.0", headers["x-stainless-package-version"])
	assert.Equal(t, "v26.12.1", headers["x-stainless-runtime-version"])
	assert.Equal(t, "2023-06-01", headers["anthropic-version"])
	assert.Equal(t, "cli", headers["x-app"])
	assert.Contains(t, headers["anthropic-beta"], "thinking-token-count-2026-05-13")
	assert.Contains(t, headers["anthropic-beta"], "advanced-tool-use-2025-11-20")
	assert.Contains(t, headers["anthropic-beta"], "interleaved-thinking-2025-05-14")
	assert.NotContains(t, headers["anthropic-beta"], "{{")
	assert.NotContains(t, headers["anthropic-beta"], "context-1m-2025-08-07")
	assert.NotContains(t, headers["anthropic-beta"], "effort-2025-11-24")
	assert.NotContains(t, headers, "Authorization")
}

func TestClaudeHeadersPreserveAdaptorAuthentication(t *testing.T) {
	header := http.Header{}
	header.Set("Authorization", "Bearer gateway-key")
	header.Set("anthropic-beta", "custom-beta-2026-01-01")

	applyClaudeCodeSimulationHeadersForChannel(&header, "gateway-key", nil)

	assert.Equal(t, "Bearer gateway-key", header.Get("Authorization"))
	assert.Empty(t, header.Get("x-api-key"))
	assert.NotEmpty(t, header.Get("user-agent"))
	assert.Contains(t, header.Get("anthropic-beta"), "custom-beta-2026-01-01")
	assert.Contains(t, header.Get("anthropic-beta"), "thinking-token-count-2026-05-13")
}

func TestClaudeBodyAddsMetadataIdentityAndTools(t *testing.T) {
	input := []byte(`{"model":"claude-sonnet-4-5","metadata":{"user_id":""},"system":"custom","tools":[]}`)

	output, err := ApplyClaudeCodeSimulationBody(input, nil)
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, common.Unmarshal(output, &body))

	metadata, ok := body["metadata"].(map[string]any)
	require.True(t, ok)
	userID, ok := metadata["user_id"].(string)
	require.True(t, ok)
	var parsedMetadata map[string]any
	require.NoError(t, common.UnmarshalJsonStr(userID, &parsedMetadata))
	assert.NotEmpty(t, parsedMetadata["device_id"])
	assert.Equal(t, "", parsedMetadata["account_uuid"])
	assert.NotEmpty(t, parsedMetadata["session_id"])

	system, ok := body["system"].([]any)
	require.True(t, ok)
	require.Len(t, system, 4)
	first, ok := system[0].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, first["text"], "x-anthropic-billing-header")
	second, ok := system[1].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, second["text"], "You are Claude Code")
	last, ok := system[3].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "custom", last["text"])

	tools, ok := body["tools"].([]any)
	require.True(t, ok)
	assert.NotEmpty(t, tools)

	again, err := ApplyClaudeCodeSimulationBody(output, nil)
	require.NoError(t, err)
	assert.JSONEq(t, string(output), string(again))
}

func TestClaudeBodyOnlyAddsBillingHeaderWhenIdentityExists(t *testing.T) {
	input := []byte(`{"model":"claude","system":[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude."}]}`)

	output, err := ApplyClaudeCodeSimulationBody(input, nil)
	require.NoError(t, err)

	system := struct {
		System []map[string]any `json:"system"`
	}{}
	require.NoError(t, common.Unmarshal(output, &system))
	require.Len(t, system.System, 2)
	assert.Contains(t, system.System[0]["text"], "x-anthropic-billing-header")
	assert.Contains(t, system.System[1]["text"], "You are Claude Code")
}

func TestCodexHeadersUseCurrentCLIIdentity(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelType:       constant.ChannelTypeOpenAI,
		ApiKey:            "sk-test",
		UpstreamModelName: "gpt-5.5",
	}}
	info.IsStream = true
	info.ChannelOtherSettings.CodexSimulationEnabled = true

	header := http.Header{}
	header.Set("Authorization", "Bearer sk-test")
	header.Set("api-key", "sk-test")
	ApplyCodexSimulationHeaders(&header, info, nil)

	assert.Equal(t, "Bearer sk-test", header.Get("Authorization"))
	assert.Equal(t, "sk-test", header.Get("api-key"))
	assert.Equal(t, "codex-tui", header.Get("originator"))
	assert.Contains(t, header.Get("user-agent"), "codex-tui/0.167.0")
	assert.Equal(t, "text/event-stream", header.Get("accept"))
	assert.Equal(t, codexSessionID, header.Get("session-id"))
	assert.Equal(t, codexSessionID, header.Get("thread-id"))
	assert.Equal(t, codexSessionID+":0", header.Get("x-codex-window-id"))
	assert.Contains(t, header.Get("x-codex-turn-metadata"), `"turn_trigger":"user"`)
	assert.Equal(t, codexBetaFeatures, header.Get("x-codex-beta-features"))
}

func TestCodexBodyAddsMetadataDefaultsAndDeveloperMessage(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelType:       constant.ChannelTypeOpenAI,
		UpstreamModelName: "gpt-5.5",
	}}
	info.IsStream = true
	info.ChannelOtherSettings.CodexSimulationEnabled = true

	input := []byte(`{"model":"gpt-5.5","input":"hello","include":["custom.include"]}`)
	output, err := ApplyCodexSimulationBody(input, info, nil)
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, common.Unmarshal(output, &body))
	assert.Equal(t, false, body["store"])
	assert.Equal(t, true, body["stream"])
	assert.Equal(t, codexSessionID, body["prompt_cache_key"])
	assert.Equal(t, true, body["parallel_tool_calls"])
	assert.Equal(t, "auto", body["tool_choice"])
	assert.Equal(t, map[string]any{"verbosity": "low"}, body["text"])

	include, ok := body["include"].([]any)
	require.True(t, ok)
	assert.Contains(t, include, "custom.include")
	assert.Contains(t, include, "reasoning.encrypted_content")

	clientMetadata, ok := body["client_metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, codexInstallationID, clientMetadata["x-codex-installation-id"])
	assert.Equal(t, codexSessionID, clientMetadata["session_id"])
	assert.Equal(t, codexSessionID, clientMetadata["thread_id"])
	assert.Equal(t, codexTurnID, clientMetadata["turn_id"])
	assert.Contains(t, clientMetadata["x-codex-turn-metadata"], `"turn_trigger":"user"`)

	messages, ok := body["input"].([]any)
	require.True(t, ok)
	require.Len(t, messages, 2)
	first, ok := messages[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "developer", first["role"])
	assert.Contains(t, first["content"], map[string]any{
		"type": "input_text",
		"text": "You are Codex, a coding agent based on GPT-5. You and the user share one workspace, and your job is to collaborate with them until their goal is genuinely handled.",
	})
	second, ok := messages[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "user", second["role"])

	again, err := ApplyCodexSimulationBody(output, info, nil)
	require.NoError(t, err)
	var repeated map[string]any
	require.NoError(t, common.Unmarshal(again, &repeated))
	repeatedMessages, ok := repeated["input"].([]any)
	require.True(t, ok)
	assert.Len(t, repeatedMessages, 2)
}

func TestCodexBodyHandlesEmptyInputArray(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelType:       constant.ChannelTypeOpenAI,
		UpstreamModelName: "gpt-5.5",
	}}
	input := []byte(`{"model":"gpt-5.5","input":[]}`)

	output, err := ApplyCodexSimulationBody(input, info, nil)
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, common.Unmarshal(output, &body))
	messages, ok := body["input"].([]any)
	require.True(t, ok)
	require.Len(t, messages, 1)
	first, ok := messages[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "developer", first["role"])
}

func TestKindForUsesFinalWireFormat(t *testing.T) {
	newInfo := func(format types.RelayFormat) *relaycommon.RelayInfo {
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
		}}
		info.RelayFormat = format
		return info
	}

	claude := newInfo(types.RelayFormatClaude)
	claude.ChannelOtherSettings.ClaudeCodeSimulationEnabled = true
	assert.Equal(t, KindClaudeCode, KindFor(claude))
	assert.True(t, NeedsHeaderRewrite(claude))
	assert.True(t, NeedsBodyRewrite(claude))

	converted := newInfo(types.RelayFormatClaude)
	converted.ChannelOtherSettings.ClaudeCodeSimulationEnabled = true
	converted.RequestConversionChain = []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI}
	assert.Equal(t, KindNone, KindFor(converted))

	codex := newInfo(types.RelayFormatOpenAIResponses)
	codex.ChannelOtherSettings.CodexSimulationEnabled = true
	codex.RelayMode = relayconstant.RelayModeResponses
	assert.Equal(t, KindCodex, KindFor(codex))
	assert.True(t, NeedsHeaderRewrite(codex))

	var nilMeta *relaycommon.RelayInfo
	assert.Equal(t, KindNone, KindFor(nilMeta))
}

func TestKindForSkipsAdaptorOwnedClaudeChannels(t *testing.T) {
	for _, channelType := range []int{
		constant.ChannelTypeAnthropic,
		constant.ChannelTypeAws,
		constant.ChannelTypeVertexAi,
	} {
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: channelType}}
		info.RelayFormat = types.RelayFormatClaude
		info.ChannelOtherSettings.ClaudeCodeSimulationEnabled = true
		assert.False(t, NeedsHeaderRewrite(info), channelType)
		assert.False(t, NeedsBodyRewrite(info), channelType)
	}
}

func TestCodexBetaFeaturesMergesExistingValue(t *testing.T) {
	header := http.Header{}
	header.Set("x-codex-beta-features", "other_feature")
	applyCodexSimulationHeaders(&header, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}, nil)
	value := header.Get("x-codex-beta-features")
	assert.True(t, strings.HasPrefix(value, "other_feature"))
	assert.Contains(t, value, codexBetaFeatures)
}

func TestCodexSimulationConfigValidation(t *testing.T) {
	valid := &dto.CodexSimulationConfig{
		Headers:  map[string]string{"User-Agent": "codex-tui/0.167.0"},
		Metadata: map[string]any{"session_id": "session", "window_number": float64(0), "analytics_enabled": true},
		System:   []any{map[string]any{"type": "message"}},
	}
	require.NoError(t, valid.Validate())

	invalidHeader := &dto.CodexSimulationConfig{Headers: map[string]string{"bad header": "x"}}
	assert.Error(t, invalidHeader.Validate())

	invalidMetadata := &dto.CodexSimulationConfig{Metadata: map[string]any{"nested": map[string]any{}}}
	assert.Error(t, invalidMetadata.Validate())

	emptySession := &dto.CodexSimulationConfig{Metadata: map[string]any{"session_id": "  "}}
	assert.Error(t, emptySession.Validate())

	nonStringIdentity := &dto.CodexSimulationConfig{Metadata: map[string]any{"turn_id": float64(1)}}
	assert.Error(t, nonStringIdentity.Validate())

	invalidSystem := &dto.CodexSimulationConfig{System: []any{map[string]any{}}}
	assert.Error(t, invalidSystem.Validate())
}
