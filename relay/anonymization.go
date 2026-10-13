package relay

import (
	"fmt"

	"new-router/pkg/anonymizer"
	relaycommon "new-router/relay/common"
	relayconstant "new-router/relay/constant"
	"new-router/setting"
	"new-router/types"

	"github.com/gin-gonic/gin"
)

func anonymizerSession(c *gin.Context, info *relaycommon.RelayInfo) *anonymizer.Session {
	if info == nil || !info.ChannelSetting.ShouldAnonymize(setting.AnonymizationEnabled) {
		return nil
	}
	if session := anonymizer.FromContext(c); session != nil {
		return session
	}
	// 每个请求使用独立 vault，随 gin.Context 生命周期回收，避免进程级 DefaultVault
	// 无界累积（内存泄漏）；同时使代理表按请求隔离，消除跨请求的还原污染与真值误跳过。
	session := anonymizer.NewSession(anonymizerEngagement(info), anonymizer.NewVault())
	anonymizer.Attach(c, session)
	return session
}

func anonymizerEngagement(info *relaycommon.RelayInfo) string {
	if info == nil {
		return "default"
	}
	return fmt.Sprintf("user:%d:token:%d:channel:%d", info.UserId, info.TokenId, info.ChannelId)
}

func anonymizeRelayRequestJSON(c *gin.Context, info *relaycommon.RelayInfo, format types.RelayFormat, data []byte) ([]byte, error) {
	session := anonymizerSession(c, info)
	if session == nil {
		return data, nil
	}

	switch {
	case format == types.RelayFormatEmbedding || info.RelayMode == relayconstant.RelayModeEmbeddings:
		return anonymizer.AnonymizeEmbeddingRequest(data, session)
	case format == types.RelayFormatOpenAIResponses ||
		format == types.RelayFormatOpenAIResponsesCompaction ||
		info.RelayMode == relayconstant.RelayModeResponses ||
		info.RelayMode == relayconstant.RelayModeResponsesCompact:
		return anonymizer.AnonymizeResponsesRequest(data, session)
	case format == types.RelayFormatOpenAI && info.RelayMode == relayconstant.RelayModeChatCompletions:
		return anonymizer.AnonymizeChatRequest(data, session)
	default:
		return anonymizer.AnonymizeTextFields(data, session)
	}
}
