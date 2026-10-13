package jsplugin

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	pluginruntime "new-router/pkg/jsplugin"
	"new-router/plugins"
	relaycommon "new-router/relay/common"
	"new-router/setting/model_setting"
)

func TestVeoTaskPluginsReceiveExistingProviderConfiguration(t *testing.T) {
	previous := model_setting.GetGeminiSettings().VersionSettings
	model_setting.GetGeminiSettings().VersionSettings = map[string]string{"default": "v1", "veo-3.1-generate-preview": "v1alpha"}
	t.Cleanup(func() { model_setting.GetGeminiSettings().VersionSettings = previous })
	for _, key := range []string{"google", "vertex-ai"} {
		source, err := plugins.Source(key)
		require.NoError(t, err)
		plugin, err := pluginruntime.NewRegistry().RegisterFactory(source, pluginruntime.Options{Key: key})
		require.NoError(t, err)
		adaptor := New(plugin)
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiVersion: `{"default":"us-central1","veo-3.1-generate-preview":"europe-west4"}`}}
		settings := adaptor.providerTaskSettings(info, "veo-3.1-generate-preview")
		if key == "google" {
			require.Equal(t, "v1alpha", settings["geminiVersion"])
			query, err := adaptor.queryContext(nil, "test-key", "https://example.com", "")
			require.NoError(t, err)
			require.Equal(t, "v1", query["userSetting"].(map[string]any)["geminiVersion"])
		} else {
			require.Equal(t, "europe-west4", settings["vertexRegion"].(map[string]any)["veo-3.1-generate-preview"])
			info.ApiVersion = "asia-east1"
			require.Equal(t, "asia-east1", adaptor.providerTaskSettings(info, "veo-3.1-generate-preview")["vertexRegion"])
		}
	}
}

func TestVeoLegacyFlatBillingKeepsMetadataDurationAnd4KMultiplier(t *testing.T) {
	for _, key := range []string{"google", "vertex-ai"} {
		t.Run(key, func(t *testing.T) {
			source, err := plugins.Source(key)
			require.NoError(t, err)
			plugin, err := pluginruntime.NewRegistry().RegisterFactory(source, pluginruntime.Options{Key: key})
			require.NoError(t, err)
			adaptor := New(plugin)
			require.NoError(t, adaptor.validateResolvedUsageRequest(map[string]any{"metadata": map[string]any{"resolution": "4K"}}))
			require.Error(t, adaptor.validateResolvedUsageRequest(map[string]any{"metadata": map[string]any{"resolution": "8K"}}))
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "veo-3.1-fast-generate-preview"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
			c.Set("task_request", relaycommon.TaskSubmitReq{Model: info.UpstreamModelName, Duration: 8, Metadata: map[string]any{"durationSeconds": 6, "resolution": "4k"}})
			ratios, err := adaptor.EstimateBillingValidated(c, info)
			require.NoError(t, err)
			require.Equal(t, float64(6), ratios["seconds"])
			require.Equal(t, 2.333333, ratios["resolution_multiplier"])
			facts, err := adaptor.ExtractUsageFactsValidated(c, info)
			require.NoError(t, err)
			require.Equal(t, float64(6), facts["seconds"])
			require.Equal(t, "4k", facts["resolution"])
			require.NotContains(t, facts, "resolution_multiplier")
		})
	}
}
