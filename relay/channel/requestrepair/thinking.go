// Package requestrepair repairs rejected request history without changing tool calls.
package requestrepair

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// IsThinkingSignatureError identifies failures involving signed thinking history.
func IsThinkingSignatureError(body []byte) bool {
	message := errorDescription(body)
	thinking := strings.Contains(message, "thinking") || strings.Contains(message, "thought_signature")
	invalidSignature := strings.Contains(message, "signature") &&
		(strings.Contains(message, "invalid") || strings.Contains(message, "not valid")) &&
		(thinking || strings.Contains(message, "thought signature"))
	missingPrefix := strings.Contains(message, "must start with a thinking block")
	expectedToolPrefix := thinking && strings.Contains(message, "expected") &&
		strings.Contains(message, "found") && strings.Contains(message, "tool_use")
	invalidSignatureField := strings.Contains(message, "signature") &&
		(strings.Contains(message, "field required") || strings.Contains(message, "extra inputs are not permitted"))
	modifiedThinking := thinking && strings.Contains(message, "cannot be modified")
	return invalidSignature || missingPrefix || expectedToolPrefix || invalidSignatureField || modifiedThinking
}

// IsEncryptedContentError identifies rejected Responses encrypted reasoning history.
func IsEncryptedContentError(body []byte) bool {
	message := errorDescription(body)
	return strings.Contains(message, "encrypted_content") ||
		strings.Contains(message, "encrypted content") ||
		(strings.Contains(message, "reasoning") && (strings.Contains(message, "could not be decrypted") || strings.Contains(message, "was not issued to this caller"))) ||
		isForeignResourceRejection(message)
}

func isForeignResourceRejection(message string) bool {
	return strings.Contains(message, "created under a different") &&
		(strings.Contains(message, "resource") || (strings.Contains(message, "item") && strings.Contains(message, "account")))
}

func errorDescription(body []byte) string {
	if !gjson.ValidBytes(body) {
		return strings.ToLower(string(body))
	}
	var description strings.Builder
	for _, path := range []string{"error.message", "error.code", "error.type", "message", "detail", "error"} {
		if value := gjson.GetBytes(body, path); value.Type == gjson.String {
			description.WriteByte(' ')
			description.WriteString(value.String())
		}
	}
	return strings.ToLower(description.String())
}

// RepairThinking repairs the final wire request after a matching rejection.
func RepairThinking(body, errorBody []byte, claudeEnabled, responsesEnabled bool) ([]byte, bool) {
	if !gjson.ValidBytes(body) {
		return body, false
	}
	if claudeEnabled && IsThinkingSignatureError(errorBody) {
		if repaired, changed := StripClaudeThinking(body); changed {
			return repaired, true
		}
	}
	if responsesEnabled && IsEncryptedContentError(errorBody) {
		return stripResponsesReasoning(body, isForeignResourceRejection(errorDescription(errorBody)))
	}
	return body, false
}

// StripClaudeThinking removes signature-dependent thinking while preserving tools and history.
func StripClaudeThinking(body []byte) ([]byte, bool) {
	if !gjson.ValidBytes(body) || !gjson.GetBytes(body, "messages").IsArray() {
		return body, false
	}
	out := body
	changed := false
	for index, message := range gjson.GetBytes(body, "messages").Array() {
		content := message.Get("content")
		if !content.IsArray() {
			continue
		}
		blocks := make([]json.RawMessage, 0, len(content.Array()))
		modified := false
		for _, block := range content.Array() {
			switch block.Get("type").String() {
			case "thinking":
				text := block.Get("thinking").String()
				if text == "" {
					text = block.Get("text").String()
				}
				if text != "" {
					encoded, err := json.Marshal(map[string]string{"type": "text", "text": text})
					if err != nil {
						return body, false
					}
					blocks = append(blocks, encoded)
				}
				modified = true
			case "redacted_thinking":
				modified = true
			default:
				raw := []byte(block.Raw)
				if block.Get("signature").Exists() {
					patched, err := sjson.DeleteBytes(raw, "signature")
					if err != nil {
						return body, false
					}
					raw, modified = patched, true
				}
				blocks = append(blocks, json.RawMessage(raw))
			}
		}
		if !modified {
			continue
		}
		// Empty message content is invalid after removing redacted history.
		if len(blocks) == 0 {
			blocks = append(blocks, json.RawMessage(`{"type":"text","text":"[Thinking omitted]"}`))
		}
		encoded, err := json.Marshal(blocks)
		if err != nil {
			return body, false
		}
		patched, err := sjson.SetRawBytes(out, "messages."+strconv.Itoa(index)+".content", encoded)
		if err != nil {
			return body, false
		}
		out, changed = patched, true
	}
	// Adaptive mode remains valid after legacy history is cleaned. Enabled mode
	// requires a thinking prefix on the last assistant tool turn.
	if gjson.GetBytes(out, "thinking.type").String() == "enabled" {
		messages := gjson.GetBytes(out, "messages").Array()
		for index := len(messages) - 1; index >= 0; index-- {
			if messages[index].Get("role").String() != "assistant" {
				continue
			}
			content := messages[index].Get("content").Array()
			if len(content) == 0 || content[0].Get("type").String() == "thinking" || content[0].Get("type").String() == "redacted_thinking" {
				break
			}
			for _, block := range content {
				if block.Get("type").String() == "tool_use" {
					patched, err := sjson.DeleteBytes(out, "thinking")
					if err != nil {
						return body, false
					}
					out, changed = patched, true
					break
				}
			}
			break
		}
	}
	return out, changed
}

// StripEncryptedReasoning removes unverifiable encrypted reasoning from Responses input.
func StripEncryptedReasoning(body []byte) ([]byte, bool) {
	return stripResponsesReasoning(body, false)
}

func stripResponsesReasoning(body []byte, stripIDs bool) ([]byte, bool) {
	if !gjson.ValidBytes(body) {
		return body, false
	}
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return body, false
	}
	items := input.Array()
	remaining := make([]json.RawMessage, 0, len(items))
	changed := false
	for _, item := range items {
		// Foreign-resource reasoning can contain only an ID. Removing that ID
		// would leave an invalid item instead of replayable history.
		if item.Get("type").String() == "reasoning" && (item.Get("encrypted_content").Exists() || stripIDs) {
			changed = true
			continue
		}
		raw := []byte(item.Raw)
		if stripIDs && item.Get("type").String() != "item_reference" && item.Get("id").Exists() {
			patched, err := sjson.DeleteBytes(raw, "id")
			if err != nil {
				return body, false
			}
			raw, changed = patched, true
		}
		remaining = append(remaining, json.RawMessage(raw))
	}
	if !changed || len(remaining) == 0 {
		return body, false
	}
	encoded, err := json.Marshal(remaining)
	if err != nil {
		return body, false
	}
	out, err := sjson.SetRawBytes(body, "input", encoded)
	if err != nil {
		return body, false
	}
	return out, true
}
