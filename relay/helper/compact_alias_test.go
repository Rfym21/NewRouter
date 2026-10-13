package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	relaycommon "new-router/relay/common"
	relayconstant "new-router/relay/constant"
	"new-router/relaykit/dto"
)

func TestLegacyCompactAliasKeepsBillingIdentityAfterMapping(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("model_mapping", `{"friendly":"gpt-5.5"}`)
	info := &relaycommon.RelayInfo{
		OriginModelName: "friendly-openai-compact",
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "friendly-openai-compact"},
	}
	request := &dto.OpenAIResponsesCompactionRequest{Model: info.OriginModelName}
	require.NoError(t, ModelMappedHelper(c, info, request))
	require.NoError(t, ApplyReasoningModelSuffix(c, info, request))
	require.Equal(t, "gpt-5.5", request.Model)
	require.Equal(t, "gpt-5.5", info.UpstreamModelName)
	require.Equal(t, "gpt-5.5-openai-compact", info.GetBillingModelName())
	require.Equal(t, "friendly-openai-compact", info.OriginModelName)
}

func TestOrdinaryCompactRequestKeepsOfficialModelIdentity(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5.5",
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.5"},
	}
	request := &dto.OpenAIResponsesCompactionRequest{Model: info.OriginModelName}
	require.NoError(t, ModelMappedHelper(c, info, request))
	require.Equal(t, "gpt-5.5", info.GetBillingModelName())
	require.Equal(t, "gpt-5.5", request.Model)
}
