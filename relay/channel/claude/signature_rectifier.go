package claude

import "new-router/relay/channel/requestrepair"

// IsSignatureRelatedError identifies rejected Claude thinking signatures.
func IsSignatureRelatedError(body []byte) bool {
	return requestrepair.IsThinkingSignatureError(body)
}

// StripThinkingFromClaudeRequest removes signed thinking while preserving tool history.
func StripThinkingFromClaudeRequest(body []byte) ([]byte, bool) {
	return requestrepair.StripClaudeThinking(body)
}
