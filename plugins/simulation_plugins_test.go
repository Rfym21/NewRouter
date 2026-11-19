package plugins_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"new-router/pkg/jsplugin"
	"new-router/pkg/requestpatch"
	"new-router/relay/requestplugin"
	"new-router/setting/request_plugin_setting"
)

// runSimulationPlugin runs the outbound hook of a built-in simulation plugin
// with the plugin-level shared config defaults applied, then applies its
// effects through the host's own decoder and applier.
func runSimulationPlugin(t *testing.T, key, format, body string, client *jsplugin.TransformClient) (string, http.Header) {
	t.Helper()
	return runSimulationPluginOnChannel(t, key, format, body, client, 0)
}

func runSimulationPluginOnChannel(t *testing.T, key, format, body string, client *jsplugin.TransformClient, channelType int) (string, http.Header) {
	t.Helper()
	plugin, found := jsplugin.DefaultRegistry.Generation().Get(key)
	require.True(t, found, key)
	config, err := plugin.ValidateConfig(nil)
	require.NoError(t, err)
	ctx := jsplugin.TransformContext{
		Stage:         "outbound",
		Format:        format,
		Path:          "/v1/messages",
		Model:         "claude-sonnet-4-5",
		UpstreamModel: "claude-sonnet-4-5",
		Group:         "default",
		Stream:        false,
		Instance:      jsplugin.TransformInstance{ID: "rp_sim00001", Name: "simulation"},
		Config:        config,
		SharedConfig:  plugin.SharedConfigFor(nil),
		Client:        client,
	}
	if channelType > 0 {
		ctx.Channel = &jsplugin.TransformChannel{ID: 1, Type: channelType}
	}
	result, err := plugin.CallTransform(t.Context(), ctx, jsplugin.TransformRequest{Body: json.RawMessage(body)})
	require.NoError(t, err)
	if result.Effects == nil {
		return body, http.Header{}
	}
	effects, err := requestpatch.DecodeTransformEffects(result.Effects, requestpatch.EffectJSONPatch, requestpatch.EffectHeaderPatch)
	require.NoError(t, err)
	patched, header, err := requestpatch.Apply([]byte(body), http.Header{}, effects, requestpatch.IsProtectedHeader)
	require.NoError(t, err)
	return string(patched), header
}

func TestSimulationPluginsThroughDryRun(t *testing.T) {
	claudeBody := `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}]}`
	claude := requestplugin.DryRun(context.Background(), requestplugin.DryRunInput{
		Format: "claude", Path: "/v1/messages", Model: "claude-sonnet-4-5",
		Body: []byte(claudeBody),
		Instance: &request_plugin_setting.Instance{
			ID: "sim-claude", Name: "Simulation", Plugin: "claude-code-simulation",
			Enabled: true, Config: json.RawMessage("{}"),
		},
	})
	require.Nil(t, claude.Rejected)
	require.Nil(t, claude.Failed)
	assert.Contains(t, string(claude.Body), "x-anthropic-billing-header")
	assert.Len(t, gjson.GetBytes(claude.Body, "tools").Array(), 6)
	assert.Contains(t, claude.Headers.Set["User-Agent"], "claude-cli/")

	codexBody := `{"model":"gpt-5.5","input":"hello"}`
	codex := requestplugin.DryRun(context.Background(), requestplugin.DryRunInput{
		Format: "openai_responses", Path: "/v1/responses", Model: "gpt-5.5",
		Body: []byte(codexBody),
		Instance: &request_plugin_setting.Instance{
			ID: "sim-codex", Name: "Simulation", Plugin: "codex-simulation",
			Enabled: true, Config: json.RawMessage("{}"),
		},
	})
	require.Nil(t, codex.Rejected)
	require.Nil(t, codex.Failed)
	assert.Equal(t, false, gjson.GetBytes(codex.Body, "store").Bool())
	assert.NotEmpty(t, gjson.GetBytes(codex.Body, "client_metadata.session_id").String())
	assert.Contains(t, codex.Headers.Set["User-Agent"], "codex-tui/")

	realBody := `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"tools":[]}`
	real := requestplugin.DryRun(context.Background(), requestplugin.DryRunInput{
		Format: "claude", Path: "/v1/messages", Model: "claude-sonnet-4-5",
		Body: []byte(realBody),
		Client: &jsplugin.TransformClient{
			Kind: "claude_code",
			Headers: map[string]string{
				"user-agent":               "claude-cli/9.9.9",
				"x-claude-code-session-id": "client-session",
			},
		},
		Instance: &request_plugin_setting.Instance{
			ID: "sim-real", Name: "Simulation", Plugin: "claude-code-simulation",
			Enabled: true, Config: json.RawMessage("{}"),
		},
	})
	require.Nil(t, real.Rejected)
	require.Nil(t, real.Failed)
	assert.JSONEq(t, realBody, string(real.Body))
	assert.Equal(t, "claude-cli/9.9.9", real.Headers.Set["User-Agent"])
	assert.Equal(t, "client-session", real.Headers.Set["X-Claude-Code-Session-Id"])
}

func TestSimulationPluginsDeclareSharedConfig(t *testing.T) {
	for _, key := range []string{"claude-code-simulation", "codex-simulation"} {
		t.Run(key, func(t *testing.T) {
			plugin, found := jsplugin.DefaultRegistry.Generation().Get(key)
			require.True(t, found, key)
			require.NotNil(t, plugin.Meta.SharedConfigSchema)
			validated, err := plugin.ValidateSharedConfig(nil)
			require.NoError(t, err)
			assert.NotEmpty(t, gjson.GetBytes(validated, "headers").Raw)
			shared := plugin.SharedConfigFor(nil)
			assert.NotEmpty(t, gjson.GetBytes(shared, "headers").Raw)
		})
	}
}

func TestClaudeCodeSimulationPluginInjectsFingerprint(t *testing.T) {
	body := `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}]}`
	patched, header := runSimulationPlugin(t, "claude-code-simulation", "claude", body, nil)

	assert.Contains(t, header.Get("user-agent"), "claude-cli/")
	assert.Equal(t, "e9361f50-9e0e-467e-9558-9549344798f1", header.Get("x-claude-code-session-id"))
	assert.Contains(t, header.Get("anthropic-beta"), "advanced-tool-use-2025-11-20")
	assert.Empty(t, header.Get("connection"))

	parsed := gjson.Parse(patched)
	userID := parsed.Get("metadata.user_id").String()
	require.NotEmpty(t, userID)
	assert.True(t, gjson.Valid(userID))
	assert.NotEmpty(t, gjson.Get(userID, "device_id").String())
	assert.NotEmpty(t, gjson.Get(userID, "session_id").String())
	assert.Contains(t, parsed.Get("system").Raw, "x-anthropic-billing-header")
	assert.Contains(t, parsed.Get("system").Raw, "You are Claude Code")
	assert.Len(t, parsed.Get("tools").Array(), 6)
}

func TestClaudeCodeSimulationPluginKeepsRealClient(t *testing.T) {
	body := `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"tools":[]}`
	client := &jsplugin.TransformClient{
		Kind: "claude_code",
		Headers: map[string]string{
			"user-agent":               "claude-cli/9.9.9 (external, cli)",
			"x-claude-code-session-id": "client-session",
			"x-stainless-os":           "Darwin",
			"anthropic-beta":           "[redacted]-custom",
		},
	}
	patched, header := runSimulationPlugin(t, "claude-code-simulation", "claude", body, client)

	assert.Equal(t, "claude-cli/9.9.9 (external, cli)", header.Get("user-agent"))
	assert.Equal(t, "client-session", header.Get("x-claude-code-session-id"))
	assert.Equal(t, "Darwin", header.Get("x-stainless-os"))
	assert.Equal(t, "[redacted]-custom", header.Get("anthropic-beta"))
	assert.Empty(t, header.Get("x-stainless-runtime-version"))
	assert.JSONEq(t, body, patched)
}

func TestClaudeCodeSimulationPluginSkipsProviderHeaders(t *testing.T) {
	body := `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}]}`
	patched, header := runSimulationPluginOnChannel(t, "claude-code-simulation", "claude", body, nil, 33)

	assert.Empty(t, header)
	assert.Contains(t, patched, "x-anthropic-billing-header")
	assert.Len(t, gjson.Get(patched, "tools").Array(), 6)
}

func TestClaudeCodeSimulationPluginRespectsExplicitEmptySystemAndTools(t *testing.T) {
	body := `{"model":"claude-sonnet-4-5","system":"","tools":[],"messages":[{"role":"user","content":"hi"}]}`
	patched, _ := runSimulationPlugin(t, "claude-code-simulation", "claude", body, nil)

	parsed := gjson.Parse(patched)
	assert.Equal(t, "", parsed.Get("system").String())
	assert.Len(t, parsed.Get("tools").Array(), 0)
	assert.NotEmpty(t, parsed.Get("metadata.user_id").String())
}

func TestClaudeCodeSimulationPluginKeepsClientUserId(t *testing.T) {
	body := `{"model":"claude-sonnet-4-5","metadata":{"user_id":"customer-42"},"messages":[{"role":"user","content":"hi"}]}`
	patched, _ := runSimulationPlugin(t, "claude-code-simulation", "claude", body, nil)
	assert.Equal(t, "customer-42", gjson.Get(patched, "metadata.user_id").String())
}

func TestClaudeCodeSimulationPluginUsesBodySession(t *testing.T) {
	body := `{"model":"claude-sonnet-4-5","metadata":{"user_id":"{\"device_id\":\"device\",\"account_uuid\":\"\",\"session_id\":\"body-session\"}"},"messages":[{"role":"user","content":"hi"}]}`
	patched, header := runSimulationPlugin(t, "claude-code-simulation", "claude", body, nil)
	assert.Equal(t, "body-session", header.Get("x-claude-code-session-id"))
	assert.Contains(t, patched, "body-session")
}

func TestCodexSimulationPluginInjectsFingerprint(t *testing.T) {
	body := `{"model":"gpt-5.5","input":"hello"}`
	patched, header := runSimulationPlugin(t, "codex-simulation", "openai_responses", body, nil)

	assert.Equal(t, "codex-tui", header.Get("originator"))
	assert.Contains(t, header.Get("user-agent"), "codex-tui/")
	assert.Equal(t, "01a1dec3-6e18-7193-a48f-e1c4667583c5", header.Get("session-id"))
	assert.Equal(t, "01a1dec3-6e18-7193-a48f-e1c4667583c5", header.Get("thread-id"))

	parsed := gjson.Parse(patched)
	assert.Equal(t, false, parsed.Get("store").Bool())
	assert.Equal(t, "01a1dec3-6e18-7193-a48f-e1c4667583c5", parsed.Get("prompt_cache_key").String())
	assert.Equal(t, "01a1dec3-6e18-7193-a48f-e1c4667583c5", parsed.Get("client_metadata.session_id").String())
	assert.Contains(t, parsed.Get("client_metadata.x-codex-turn-metadata").String(), `"turn_trigger":"user"`)
	input := parsed.Get("input").Array()
	require.Len(t, input, 2)
	assert.Equal(t, "developer", input[0].Get("role").String())
	assert.Contains(t, input[0].Raw, "You are Codex")
	assert.Equal(t, "user", input[1].Get("role").String())
}

func TestCodexSimulationPluginKeepsRealClient(t *testing.T) {
	body := `{"model":"gpt-5.5","client_metadata":{"session_id":"client-session"},"input":[{"type":"message","role":"developer","content":[{"type":"input_text","text":"You are Codex"}]}]}`
	client := &jsplugin.TransformClient{
		Kind: "codex",
		Headers: map[string]string{
			"user-agent":        "codex-tui/9.9.9",
			"originator":        "codex-tui",
			"session-id":        "client-session",
			"thread-id":         "client-thread",
			"x-codex-window-id": "client-thread:0",
		},
	}
	patched, header := runSimulationPlugin(t, "codex-simulation", "openai_responses", body, client)

	assert.Equal(t, "codex-tui/9.9.9", header.Get("user-agent"))
	assert.Equal(t, "client-session", header.Get("session-id"))
	assert.Equal(t, "client-thread", header.Get("thread-id"))
	assert.JSONEq(t, body, patched)
}

func TestCodexSimulationPluginKeepsValidClientMetadata(t *testing.T) {
	body := `{"model":"gpt-5.5","client_metadata":{"session_id":"client-session","thread_id":"client-thread"},"input":"hello"}`
	patched, _ := runSimulationPlugin(t, "codex-simulation", "openai_responses", body, nil)

	parsed := gjson.Parse(patched)
	assert.Equal(t, "client-session", parsed.Get("client_metadata.session_id").String())
	assert.Equal(t, "client-thread", parsed.Get("client_metadata.thread_id").String())
	assert.Equal(t, "client-session", parsed.Get("prompt_cache_key").String())
}

func TestSimulationPluginsIgnoreOtherFormats(t *testing.T) {
	body := `{"model":"gpt-5.5","input":"hello"}`
	patched, header := runSimulationPlugin(t, "claude-code-simulation", "openai_responses", body, nil)
	assert.JSONEq(t, body, patched)
	assert.Empty(t, header)
	patched, header = runSimulationPlugin(t, "codex-simulation", "claude", body, nil)
	assert.JSONEq(t, body, patched)
	assert.Empty(t, header)
}
