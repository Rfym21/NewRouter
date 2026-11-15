package relay

import (
	"context"
	"fmt"
	"new-router/common"
	"new-router/model"
	"new-router/service"

	"new-router/pkg/anonymizer"
	relaycommon "new-router/relay/common"
	relayconstant "new-router/relay/constant"
	"new-router/setting"
	"new-router/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const anonymizationDecisionKey = "new-router.anonymizer.decision"

type anonymizationDecision struct {
	userID, tokenID, channelID int
	enabled                    bool
}

func shouldAnonymizeAttempt(c *gin.Context, info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	if c != nil {
		if value, exists := c.Get(anonymizationDecisionKey); exists {
			if decision, ok := value.(anonymizationDecision); ok && decision.userID == info.UserId && decision.tokenID == info.TokenId && decision.channelID == info.ChannelId {
				return decision.enabled
			}
		}
	}
	enabledDefault := model.CurrentRequestPolicy().Options["AnonymizationEnabled"] == "true"
	if managed, err := service.ManagedAnonymizationPolicyFor(info.ChannelId); err == nil && managed.Managed {
		enabledDefault = managed.EnabledByDefault
	}
	enabled := info.ChannelSetting.ShouldAnonymize(enabledDefault)
	if c != nil {
		c.Set(anonymizationDecisionKey, anonymizationDecision{userID: info.UserId, tokenID: info.TokenId, channelID: info.ChannelId, enabled: enabled})
	}
	return enabled
}

func anonymizerSession(c *gin.Context, info *relaycommon.RelayInfo) *anonymizer.Session {
	if !shouldAnonymizeAttempt(c, info) {
		anonymizer.ClearContextForDisabledChannel(c)
		return nil
	}
	if session := anonymizer.FromContext(c); session != nil {
		return session
	}
	// A request-local vault imports only authenticated continuation snapshots.
	// Responses/compact and signed histories can reuse their own mappings without
	// sharing mutable vaults or originals with another user/token.
	policy := model.CurrentRequestPolicy()
	stable := policy.Options["AnonymizationStableSurrogates"] == "true"
	strict := policy.Options["AnonymizationStrictContextEnabled"] == "true"
	rules := policy.AnonymizationRules
	managed, managedErr := service.ManagedAnonymizationPolicyFor(info.ChannelId)
	if managedErr != nil {
		// A broken managed instance falls back to the legacy options so a bad
		// edit does not stop every anonymized request.
		common.SysError(common.LogText("failed to read anonymization policy: %s", managedErr.Error()))
	} else if managed.Managed {
		stable = managed.StableSurrogates
		strict = managed.StrictContext
		if managed.Rules != nil {
			rules = managed.Rules
		}
	}
	session := anonymizer.NewSession(anonymizerEngagement(info), anonymizer.NewVault()).
		WithRules(rules).
		WithStableSurrogates(stable).
		WithStrictContinuation(strict)
	anonymizer.Attach(c, session)
	anonymizer.AttachContinuation(c, info.UserId, info.TokenId)
	return session
}

func anonymizerEngagement(info *relaycommon.RelayInfo) string {
	if info == nil {
		return "default"
	}
	return fmt.Sprintf("user:%d:token:%d", info.UserId, info.TokenId)
}

func anonymizeRelayRequestJSON(c *gin.Context, info *relaycommon.RelayInfo, format types.RelayFormat, data []byte) (output []byte, resultErr error) {
	session := anonymizerSession(c, info)
	if session == nil {
		return data, nil
	}
	if err := anonymizer.RestoreRequestContext(c, data); err != nil {
		return nil, err
	}
	detector, err := setting.AnonymizationDetector()
	if err != nil {
		return nil, fmt.Errorf("invalid anonymization analyzer configuration")
	}
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	session.WithDetector(ctx, detector)
	if info.RelayMode == relayconstant.RelayModeChatCompletions ||
		info.RelayMode == relayconstant.RelayModeResponses ||
		info.RelayMode == relayconstant.RelayModeResponsesCompact || format == types.RelayFormatClaude {
		session.WithDetectionHistory(fmt.Sprintf("channel:%d:model:%s:format:%s", info.ChannelId, info.UpstreamModelName, format))
	} else {
		session.WithDetectionHistory("")
	}

	switch {
	case format == types.RelayFormatClaude:
		return anonymizer.AnonymizeClaudeRequest(data, session)
	case format == types.RelayFormatGemini:
		return anonymizer.AnonymizeGeminiRequest(data, session)
	case gjson.GetBytes(data, "messages").IsArray():
		return anonymizer.AnonymizeChatRequest(data, session)
	case gjson.GetBytes(data, "prompt").Exists():
		return anonymizer.AnonymizeCompletionRequest(data, session)
	case format == types.RelayFormatEmbedding || info.RelayMode == relayconstant.RelayModeEmbeddings:
		return anonymizer.AnonymizeEmbeddingRequest(data, session)
	case format == types.RelayFormatOpenAIResponses ||
		format == types.RelayFormatOpenAIResponsesCompaction ||
		info.RelayMode == relayconstant.RelayModeResponses ||
		info.RelayMode == relayconstant.RelayModeResponsesCompact:
		return anonymizer.AnonymizeResponsesRequest(data, session)
	case format == types.RelayFormatOpenAI && info.RelayMode == relayconstant.RelayModeChatCompletions:
		return anonymizer.AnonymizeChatRequest(data, session)
	case format == types.RelayFormatOpenAI && info.RelayMode == relayconstant.RelayModeCompletions:
		return anonymizer.AnonymizeCompletionRequest(data, session)
	default:
		return anonymizer.AnonymizeTextFields(data, session)
	}
}
