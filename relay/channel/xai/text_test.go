package xai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"new-router/constant"
	relaycommon "new-router/relay/common"
)

func TestXAIStreamUsagePreservesCacheAndReasoningDetails(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 5
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	output := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(output)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(t.Context())
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "grok-test"}}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: " + `{"id":"chat-test","object":"chat.completion.chunk","model":"grok-test","choices":[{"index":0,"delta":{"content":"hello"}}]}` + "\n\n" +
				"data: " + `{"id":"chat-test","object":"chat.completion.chunk","model":"grok-test","choices":[],"usage":{"prompt_tokens":100,"total_tokens":125,"prompt_tokens_details":{"cached_tokens":80},"completion_tokens_details":{"reasoning_tokens":8,"text_tokens":17}}}` + "\n\n" +
				"data: [DONE]\n\n")),
	}

	usage, apiErr := xAIStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 100, usage.PromptTokens)
	assert.Equal(t, 25, usage.CompletionTokens)
	assert.Equal(t, 125, usage.TotalTokens)
	assert.Equal(t, 80, usage.PromptTokensDetails.CachedTokens, "billing must receive the upstream cache hit count")
	assert.Equal(t, 8, usage.CompletionTokenDetails.ReasoningTokens)
	assert.Equal(t, 17, usage.CompletionTokenDetails.TextTokens)
	assert.Contains(t, output.Body.String(), `"cached_tokens":80`)
	assert.Contains(t, output.Body.String(), `"reasoning_tokens":8`)
}
