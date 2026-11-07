package openai

import "new-router/relay/channel/requestrepair"

// IsEncryptedContentError identifies rejected Responses encrypted reasoning history.
func IsEncryptedContentError(body []byte) bool {
	return requestrepair.IsEncryptedContentError(body)
}

// StripEncryptedReasoning removes unverifiable reasoning while preserving remaining input.
func StripEncryptedReasoning(body []byte) ([]byte, bool) {
	return requestrepair.StripEncryptedReasoning(body)
}
