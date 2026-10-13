package claude

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"new-router/common"
	"new-router/constant"
	"new-router/pkg/anonymizer"
	relaycommon "new-router/relay/common"
	"new-router/relaykit/dto"
	"new-router/relaykit/types"
)

func TestClaudeResponsesStreamPreservesAnonymizationAndForcedCacheBilling(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	vault := anonymizer.NewVault()
	mapping := vault.GetOrCreate("migration", "alice@company.example", "EMAIL")
	middle := len(mapping.Surrogate) / 2
	body := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[],"usage":{"input_tokens":5,"cache_creation_input_tokens":3,"cache_read_input_tokens":7,"output_tokens":0}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		fmt.Sprintf(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%q}}`, mapping.Surrogate[:middle]),
		fmt.Sprintf(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%q}}`, mapping.Surrogate[middle:]),
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`,
		`data: {"type":"message_stop"}`, `data: [DONE]`, "",
	}, "\n")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	anonymizer.Attach(c, anonymizer.NewSession("migration", vault))
	common.SetContextKey(c, constant.ContextKeyClaudeForceCacheInjected, true)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	info := &relaycommon.RelayInfo{
		OriginModelName: "client-model", IsStream: true, DisablePing: true, RelayFormat: types.RelayFormatOpenAIResponses,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test", ChannelSetting: dto.ChannelSettings{UnifyRequestResponseModelNameEnabled: true}},
	}
	usage, apiErr := ClaudeResponsesStreamHandler(c, resp, info)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 15, usage.PromptTokens)
	require.Zero(t, usage.PromptTokensDetails.CachedTokens)
	require.Zero(t, usage.PromptTokensDetails.CacheCreationTokensTotal())
	require.NotNil(t, usage.BillingUsage)
	canonical, ok := usage.BillingUsage.CanonicalUsage()
	require.True(t, ok)
	require.Equal(t, 15, canonical.PromptTokens)
	require.Zero(t, canonical.PromptTokensDetails.CachedTokens)
	var content strings.Builder
	for line := range strings.SplitSeq(recorder.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if gjson.Get(data, "type").String() == "response.output_text.delta" {
			content.WriteString(gjson.Get(data, "delta").String())
		}
	}
	require.Equal(t, mapping.Original, content.String())
	require.Contains(t, recorder.Body.String(), `"model":"client-model"`)
	require.NotContains(t, recorder.Body.String(), mapping.Surrogate)
}

func TestForcedCacheRewriteUpdatesAuthoritativeBillingUsage(t *testing.T) {
	usage := &dto.ClaudeUsage{InputTokens: 5, OutputTokens: 4, CacheReadInputTokens: 7, CacheCreationInputTokens: 3}
	usage.BillingUsage = dto.NewClaudeMessagesBillingUsage(usage)
	applyClaudeUsageRewrites(usage, true, false, 0)
	canonical, ok := usage.BillingUsage.CanonicalUsage()
	require.True(t, ok)
	require.Equal(t, 15, canonical.PromptTokens)
	require.Zero(t, canonical.PromptTokensDetails.CachedTokens)
	require.Zero(t, canonical.PromptTokensDetails.CacheCreationTokensTotal())
}
