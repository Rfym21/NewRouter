package relay

import (
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"new-router/common"
	"new-router/pkg/anonymizer"
	relaycommon "new-router/relay/common"
	relayconstant "new-router/relay/constant"
	"new-router/relaykit/dto"
	"new-router/relaykit/types"
	"new-router/setting"
	"new-router/setting/config"
	"new-router/setting/runtime_setting"
)

func setAnonymizationAnalyzerConfig(t *testing.T, modelDir, endpoint string) {
	t.Helper()
	previousDir, previousURL := runtime_setting.GetAnonymizationAnalyzerConfig()
	update := func(directory, url string) {
		require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("runtime_setting"), map[string]string{
			"anonymization_local_ner_model_dir": directory,
			"anonymization_presidio_url":        url,
		}))
		runtime_setting.ApplyToRuntime()
	}
	update(modelDir, endpoint)
	t.Cleanup(func() { update(previousDir, previousURL) })
}

func TestLocalNERThroughRelayPreservesHistoryAndRestoresNames(t *testing.T) {
	directory := os.Getenv("ANONYMIZATION_TEST_LOCAL_MODEL_DIR")
	if directory == "" {
		t.Skip("ANONYMIZATION_TEST_LOCAL_MODEL_DIR is not set")
	}
	setAnonymizationAnalyzerConfig(t, directory, "")
	detector, err := setting.AnonymizationDetector()
	require.NoError(t, err)
	require.IsType(t, &anonymizer.LocalDetector{}, detector)
	again, err := setting.AnonymizationDetector()
	require.NoError(t, err)
	require.Same(t, detector, again, "model weights must be reused across requests")
	const name = "\x41lice \x4aohnson"
	const payload = `{"messages":[{"role":"user","content":"My name is \u0041lice \u004aohnson. My email address is alice.johnson@example.com."}]}`
	var first string
	for range 2 {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
		info := &relaycommon.RelayInfo{
			UserId: 98765, TokenId: 43210, RelayMode: relayconstant.RelayModeChatCompletions,
			ChannelMeta: &relaycommon.ChannelMeta{
				ChannelId: 43210, UpstreamModelName: "local-ner-test",
				ChannelSetting: dto.ChannelSettings{AnonymizationEnabled: common.GetPointer(true)},
			},
		}
		session := anonymizerSession(c, info).WithStableSurrogates(true)
		masked, err := anonymizeRelayRequestJSON(c, info, types.RelayFormatOpenAI, []byte(payload))
		require.NoError(t, err)
		require.NotContains(t, string(masked), name)
		require.NotContains(t, string(masked), "alice.johnson@example.com")
		var foundName bool
		for _, mapping := range session.Snapshot() {
			foundName = foundName || mapping.EntityType == "PERSON" && mapping.Original == name
		}
		require.True(t, foundName, "the configured local model must contribute PERSON spans")
		restored, err := anonymizer.DeanonymizeTextFields(masked, session)
		require.NoError(t, err)
		require.JSONEq(t, payload, string(restored))
		if first == "" {
			first = string(masked)
		}
		require.Equal(t, first, string(masked))
	}
}

func TestAnonymizationDispatchUsesActualUpstreamFormat(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format types.RelayFormat
		mode   int
		body   string
	}{
		{"responses converted to Claude", types.RelayFormatClaude, relayconstant.RelayModeResponses, `{"messages":[{"role":"user","content":[{"type":"text","text":"alice@private.example"}]}]}`},
		{"responses converted to Gemini", types.RelayFormatGemini, relayconstant.RelayModeResponses, `{"contents":[{"parts":[{"text":"alice@private.example"}]}]}`},
		{"responses converted to Chat", types.RelayFormatOpenAI, relayconstant.RelayModeResponses, `{"messages":[{"role":"assistant","content":"alice@private.example"}]}`},
		{"legacy completion", types.RelayFormatOpenAI, relayconstant.RelayModeCompletions, `{"prompt":"alice@private.example"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			info := &relaycommon.RelayInfo{UserId: 1, TokenId: 2, RelayMode: tc.mode, ChannelMeta: &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{AnonymizationEnabled: common.GetPointer(true)}}}
			body, err := anonymizeRelayRequestJSON(c, info, tc.format, []byte(tc.body))
			require.NoError(t, err)
			assert.NotContains(t, string(body), "alice@private.example")
			session := anonymizer.FromContext(c)
			require.NotNil(t, session)
			assert.EqualValues(t, 1, session.ReplacementCount())
			assert.Equal(t, "alice@private.example", session.Snapshot()[0].Original)
			info.ChannelSetting.AnonymizationEnabled = common.GetPointer(false)
			info.ChannelId++
			unmodified, err := anonymizeRelayRequestJSON(c, info, tc.format, []byte(tc.body))
			require.NoError(t, err)
			assert.JSONEq(t, tc.body, string(unmodified))
			assert.Nil(t, anonymizer.FromContext(c), "a disabled retry cannot reuse another attempt's restoration context")
			assert.Zero(t, anonymizer.FromContext(c).ReplacementCount(), "a successful disabled attempt cannot inherit an earlier count")
		})
	}
}

func TestAnonymizationContinuationDefaultFallbackThroughRelay(t *testing.T) {
	setAnonymizationAnalyzerConfig(t, "", "")
	for _, tc := range []struct {
		name   string
		format types.RelayFormat
		body   string
	}{
		{"responses", types.RelayFormatOpenAIResponses, `{"previous_response_id":"unavailable-response","input":[{"type":"reasoning","encrypted_content":"opaque-history"},{"role":"user","content":"alice@private.example"}]}`},
		{"claude", types.RelayFormatClaude, `{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"old thought","signature":"unavailable-signature"}]},{"role":"user","content":"alice@private.example"}]}`},
		{"gemini", types.RelayFormatGemini, `{"contents":[{"parts":[{"thoughtSignature":"unavailable-signature","text":"old thought"},{"text":"alice@private.example"}]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			info := &relaycommon.RelayInfo{UserId: 1, TokenId: 2, RelayMode: relayconstant.RelayModeResponses, ChannelMeta: &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{AnonymizationEnabled: common.GetPointer(true)}}}
			masked, err := anonymizeRelayRequestJSON(c, info, tc.format, []byte(tc.body))
			require.NoError(t, err)
			assert.NotContains(t, string(masked), "alice@private.example")
			session := anonymizer.FromContext(c)
			restored, err := anonymizer.DeanonymizeTextFields(masked, session)
			require.NoError(t, err)
			assert.JSONEq(t, tc.body, string(restored))
			strictContext, _ := gin.CreateTestContext(httptest.NewRecorder())
			strictContext.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			anonymizerSession(strictContext, info).WithStrictContinuation(true)
			_, err = anonymizeRelayRequestJSON(strictContext, info, tc.format, []byte(tc.body))
			assert.ErrorIs(t, err, anonymizer.ErrContinuationUnavailable)
		})
	}
}
