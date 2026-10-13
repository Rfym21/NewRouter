package claude

import (
	"strings"

	"new-router/common"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	// StripStageThinkingOnly removes only thinking-related structures.
	StripStageThinkingOnly = "thinking-only"
	// StripStageThinkingAndTools also removes tool_use/tool_result blocks when thinking-only retry fails.
	StripStageThinkingAndTools = "thinking+tools"
)

// IsSignatureRelatedError reports whether a 4xx response looks like a Claude thinking signature error.
func IsSignatureRelatedError(respBody []byte) bool {
	if len(respBody) == 0 {
		return false
	}
	msg := strings.ToLower(string(respBody))
	if strings.Contains(msg, "thought_signature") || strings.Contains(msg, "signature") {
		return true
	}
	if strings.Contains(msg, "expected") &&
		(strings.Contains(msg, "thinking") || strings.Contains(msg, "redacted_thinking")) {
		return true
	}
	return false
}

// StripThinkingFromClaudeRequest removes Claude thinking fields without changing tool blocks.
// Redacted thinking is dropped because its encrypted content cannot be represented as plain text.
func StripThinkingFromClaudeRequest(jsonData []byte) ([]byte, bool) {
	if len(jsonData) == 0 {
		return jsonData, false
	}
	jsonStr := string(jsonData)
	changed := false

	if gjson.Get(jsonStr, "thinking").Exists() {
		if patched, err := sjson.Delete(jsonStr, "thinking"); err == nil {
			jsonStr = patched
			changed = true
		}
	}

	if mutated, ok := transformMessageBlocks(jsonStr, false); ok {
		jsonStr = mutated
		changed = true
	}

	return []byte(jsonStr), changed
}

// StripSignatureSensitiveBlocksFromClaudeRequest removes thinking blocks and tool blocks that can break signature validation.
// tool_use is preserved as text while tool_result is dropped because it depends on the removed tool_use_id.
func StripSignatureSensitiveBlocksFromClaudeRequest(jsonData []byte) ([]byte, bool) {
	if len(jsonData) == 0 {
		return jsonData, false
	}
	jsonStr := string(jsonData)
	changed := false

	if gjson.Get(jsonStr, "thinking").Exists() {
		if patched, err := sjson.Delete(jsonStr, "thinking"); err == nil {
			jsonStr = patched
			changed = true
		}
	}

	if mutated, ok := transformMessageBlocks(jsonStr, true); ok {
		jsonStr = mutated
		changed = true
	}

	return []byte(jsonStr), changed
}

// transformMessageBlocks rewrites messages.[*].content arrays while preserving unknown block fields.
func transformMessageBlocks(jsonStr string, stripTools bool) (string, bool) {
	messages := gjson.Get(jsonStr, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return jsonStr, false
	}

	changed := false
	msgArr := messages.Array()
	for i := 0; i < len(msgArr); i++ {
		path := "messages." + itoa(i) + ".content"
		content := gjson.Get(jsonStr, path)
		if !content.Exists() || !content.IsArray() {
			continue
		}

		blocks := content.Array()
		newBlocks := make([]interface{}, 0, len(blocks))
		blockChanged := false
		for _, block := range blocks {
			blockType := block.Get("type").String()
			switch blockType {
			case "thinking":
				text := block.Get("thinking").String()
				if text == "" {
					text = block.Get("text").String()
				}
				if text == "" {
					blockChanged = true
					continue
				}
				newBlocks = append(newBlocks, map[string]interface{}{
					"type": "text",
					"text": text,
				})
				blockChanged = true
			case "redacted_thinking":
				blockChanged = true
				continue
			case "tool_use":
				if !stripTools {
					newBlocks = append(newBlocks, parseRawBlock(block.Raw))
					continue
				}
				newBlocks = append(newBlocks, map[string]interface{}{
					"type": "text",
					"text": summarizeToolUse(block),
				})
				blockChanged = true
			case "tool_result":
				if !stripTools {
					newBlocks = append(newBlocks, parseRawBlock(block.Raw))
					continue
				}
				blockChanged = true
				continue
			default:
				newBlocks = append(newBlocks, parseRawBlock(block.Raw))
			}
		}

		if !blockChanged {
			continue
		}

		raw, err := common.Marshal(newBlocks)
		if err != nil {
			continue
		}
		patched, err := sjson.SetRawBytes([]byte(jsonStr), path, raw)
		if err != nil {
			continue
		}
		jsonStr = string(patched)
		changed = true
	}

	return jsonStr, changed
}

// summarizeToolUse compresses a tool_use block into text so the retry keeps useful context.
func summarizeToolUse(block gjson.Result) string {
	name := block.Get("name").String()
	id := block.Get("id").String()
	input := block.Get("input").Raw
	if input == "" {
		input = "{}"
	}
	var b strings.Builder
	b.WriteString("(tool_use)")
	if name != "" {
		b.WriteString(" name=")
		b.WriteString(name)
	}
	if id != "" {
		b.WriteString(" id=")
		b.WriteString(id)
	}
	b.WriteString(" input=")
	b.WriteString(input)
	return b.String()
}

// parseRawBlock preserves unknown JSON block fields when no signature-sensitive rewrite is needed.
func parseRawBlock(raw string) map[string]interface{} {
	if raw == "" {
		return nil
	}
	out := make(map[string]interface{})
	if err := common.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}
