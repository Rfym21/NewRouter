package simulation

import (
	"net/http"

	"new-router/constant"
	relaycommon "new-router/relay/common"
	relayconstant "new-router/relay/constant"
	"new-router/relaykit/types"
)

// Kind identifies the client fingerprint a request should carry upstream.
type Kind uint8

const (
	KindNone Kind = iota
	KindClaudeCode
	KindCodex
)

// KindFor selects the simulation from the final upstream protocol, not the
// client-facing path: a Claude request converted to chat completions must not
// receive Claude Messages body fields.
func KindFor(info *relaycommon.RelayInfo) Kind {
	if info == nil || info.ChannelMeta == nil {
		return KindNone
	}
	switch info.GetFinalRequestRelayFormat() {
	case types.RelayFormatClaude:
		if info.ChannelOtherSettings.ClaudeCodeSimulationEnabled {
			return KindClaudeCode
		}
	case types.RelayFormatOpenAIResponses:
		if info.ChannelOtherSettings.CodexSimulationEnabled {
			return KindCodex
		}
	}
	return KindNone
}

// claudeSimulationHandledByAdaptor reports whether the channel adaptor has
// already applied the Claude Code fingerprint. AWS and Vertex can only accept
// the beta tokens, and the native Anthropic adaptor applies the full header set.
func claudeSimulationHandledByAdaptor(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	switch info.ChannelType {
	case constant.ChannelTypeAnthropic, constant.ChannelTypeAws, constant.ChannelTypeVertexAi:
		return true
	default:
		return false
	}
}

// NeedsHeaderRewrite reports whether the shared request pipeline should add
// simulation headers for this request.
func NeedsHeaderRewrite(info *relaycommon.RelayInfo) bool {
	switch KindFor(info) {
	case KindClaudeCode:
		return !claudeSimulationHandledByAdaptor(info)
	case KindCodex:
		return true
	default:
		return false
	}
}

// NeedsBodyRewrite reports whether the shared request pipeline should rewrite
// the JSON body for this request.
func NeedsBodyRewrite(info *relaycommon.RelayInfo) bool {
	switch KindFor(info) {
	case KindClaudeCode:
		return !claudeSimulationHandledByAdaptor(info)
	case KindCodex:
		return info.RelayMode == relayconstant.RelayModeResponses
	default:
		return false
	}
}

// ApplyHeaders applies the selected simulation headers. Authentication headers
// are left to the channel adaptor so SigV4, api-key, and custom auth survive.
func ApplyHeaders(info *relaycommon.RelayInfo, header *http.Header) {
	if header == nil || !NeedsHeaderRewrite(info) {
		return
	}
	switch KindFor(info) {
	case KindClaudeCode:
		applyClaudeCodeSimulationHeadersForChannel(header, info.ApiKey, info.ChannelOtherSettings.ClaudeCodeSimulation)
	case KindCodex:
		applyCodexSimulationHeaders(header, info, info.ChannelOtherSettings.CodexSimulation)
	}
}

// ApplyBody rewrites a JSON request body for the selected simulation.
func ApplyBody(info *relaycommon.RelayInfo, body []byte) ([]byte, error) {
	if info == nil || !NeedsBodyRewrite(info) {
		return body, nil
	}
	switch KindFor(info) {
	case KindClaudeCode:
		return ApplyClaudeCodeSimulationBody(body, info.ChannelOtherSettings.ClaudeCodeSimulation)
	case KindCodex:
		return ApplyCodexSimulationBody(body, info, info.ChannelOtherSettings.CodexSimulation)
	default:
		return body, nil
	}
}
