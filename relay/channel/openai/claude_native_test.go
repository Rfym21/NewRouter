package openai

import (
	"testing"

	"new-router/constant"
	relaycommon "new-router/relay/common"
	"new-router/relaykit/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func claudeInfo(channelType int, baseURL, model string) *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelType:       channelType,
		ChannelBaseUrl:    baseURL,
		UpstreamModelName: model,
	}}
	info.RelayFormat = types.RelayFormatClaude
	return info
}

func TestOpenRouterClaudeUsesNativeMessagesEndpoint(t *testing.T) {
	info := claudeInfo(constant.ChannelTypeOpenRouter, "https://openrouter.ai/api", "anthropic/claude-sonnet-4.5")
	require.True(t, isNativeClaudeTarget(info))

	url, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://openrouter.ai/api/v1/messages", url)
}

func TestOpenRouterNonClaudeKeepsChatConversion(t *testing.T) {
	info := claudeInfo(constant.ChannelTypeOpenRouter, "https://openrouter.ai/api", "openai/gpt-5")
	assert.False(t, isNativeClaudeTarget(info))

	url, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://openrouter.ai/api/v1/chat/completions", url)
}

func TestAzureFoundryAnthropicUsesNativeMessagesEndpoint(t *testing.T) {
	tests := []struct {
		baseURL string
		want    string
	}{
		{"https://resource.services.ai.azure.com", "https://resource.services.ai.azure.com/anthropic/v1/messages"},
		{"https://resource.services.ai.azure.com/anthropic", "https://resource.services.ai.azure.com/anthropic/v1/messages"},
		{"https://resource.services.ai.azure.com/anthropic/v1", "https://resource.services.ai.azure.com/anthropic/v1/messages"},
		{"https://resource.services.ai.azure.com/anthropic/v1/messages", "https://resource.services.ai.azure.com/anthropic/v1/messages"},
	}
	for _, test := range tests {
		info := claudeInfo(constant.ChannelTypeAzure, test.baseURL, "claude-sonnet-4-5")
		require.True(t, isNativeClaudeTarget(info), test.baseURL)

		url, err := (&Adaptor{}).GetRequestURL(info)
		require.NoError(t, err)
		assert.Equal(t, test.want, url)
	}
}

func TestAzureOpenAIEndpointDoesNotUseAnthropicRoute(t *testing.T) {
	assert.False(t, isAzureAnthropicBaseURL("https://resource.openai.azure.com"))
	assert.False(t, isAzureAnthropicBaseURL("https://resource.services.ai.azure.com/openai"))

	info := claudeInfo(constant.ChannelTypeAzure, "https://resource.openai.azure.com", "claude-sonnet-4-5")
	assert.False(t, isNativeClaudeTarget(info))
}
