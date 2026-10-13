package plugins_test

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
	"new-router/common"
	"new-router/pkg/jsplugin"
	builtinplugins "new-router/plugins"
)

func TestLegacyTaskProviderChannelAndModelRouting(t *testing.T) {
	cases := []struct {
		key     string
		channel int
		models  []string
	}{
		{"alibaba", 17, []string{"wan2.7-i2v", "wan2.7-t2v", "wan2.5-i2v-preview", "wan2.2-i2v-flash", "wan2.2-i2v-plus", "wanx2.1-i2v-plus", "wanx2.1-i2v-turbo"}},
		{"google", 24, []string{"veo-3.0-generate-001", "veo-3.0-fast-generate-001", "veo-3.1-generate-preview", "veo-3.1-fast-generate-preview"}},
		{"vertex-ai", 41, []string{"veo-3.0-generate-001", "veo-3.0-fast-generate-001", "veo-3.1-generate-preview", "veo-3.1-fast-generate-preview"}},
		{"hailuo", 35, []string{"MiniMax-Hailuo-2.3", "MiniMax-Hailuo-2.3-Fast", "MiniMax-Hailuo-02", "T2V-01-Director", "T2V-01", "I2V-01-Director", "I2V-01-live", "I2V-01", "S2V-01"}},
		{"jimeng", 51, []string{"jimeng_vgfm_t2v_l20"}},
		{"vidu", 52, []string{"viduq2", "viduq1", "vidu2.0", "vidu1.5"}},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			plugin, ok := jsplugin.DefaultRegistry.Generation().GetByChannelType(tc.channel)
			require.True(t, ok)
			require.Equal(t, tc.key, plugin.Meta.Key)
			for _, model := range tc.models {
				require.Contains(t, plugin.Meta.Models, model)
			}
		})
	}
}

func TestVeoConfiguredEndpointsAndPersistedOperationIDs(t *testing.T) {
	for _, key := range []string{"google", "vertex-ai"} {
		t.Run(key, func(t *testing.T) {
			source, err := builtinplugins.Source(key)
			require.NoError(t, err)
			plugin, err := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{Key: key})
			require.NoError(t, err)
			ctx := map[string]any{
				"baseUrl": "https://proxy.example", "apiKey": "test-key", "authHeader": "Bearer test-token", "auth": map[string]any{"projectId": "project-1"},
				"upstreamModel": "veo-3.1-generate-preview", "userSetting": map[string]any{"geminiVersion": "v1alpha", "vertexRegion": map[string]any{"default": "us-central1", "veo-3.1-generate-preview": "europe-west4"}},
				"requestBody": map[string]any{"prompt": "test", "duration": 8, "metadata": map[string]any{"durationSeconds": 6}},
			}
			value, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", ctx)
			require.NoError(t, err)
			encoded, err := common.Marshal(value)
			require.NoError(t, err)
			var submit map[string]any
			require.NoError(t, common.Unmarshal(encoded, &submit))
			if key == "google" {
				require.Contains(t, submit["url"], "/v1alpha/models/")
			} else {
				require.Contains(t, submit["url"], "/locations/europe-west4/")
			}
			require.Equal(t, float64(6), submit["body"].(map[string]any)["parameters"].(map[string]any)["durationSeconds"])
			operation := "projects/project-1/locations/asia-east1/publishers/google/models/veo-3.1-generate-preview/operations/old-id"
			if key == "google" {
				operation = "models/veo-3.1-generate-preview/operations/old-id"
			}
			ctx["taskId"] = base64.RawURLEncoding.EncodeToString([]byte(operation))
			value, err = plugin.Engine.Call(t.Context(), "buildQueryRequest", ctx)
			require.NoError(t, err)
			encoded, err = common.Marshal(value)
			require.NoError(t, err)
			var query map[string]any
			require.NoError(t, common.Unmarshal(encoded, &query))
			if key == "google" {
				require.Equal(t, "https://proxy.example/v1alpha/"+operation, query["url"])
			} else {
				require.Contains(t, query["url"], "/locations/asia-east1/")
				require.Equal(t, operation, query["body"].(map[string]any)["operationName"])
			}
		})
	}
}
