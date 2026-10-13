package claude

import (
	"bytes"

	"new-router/common"
	"new-router/dto"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	ephemeralCacheControl   = `{"type":"ephemeral"}`
	ephemeralCacheControl1h = `{"type":"ephemeral","ttl":"1h"}`

	// ForceCacheTTL1h writes ttl=1h into injected cache_control blocks.
	ForceCacheTTL1h = "1h"
)

// cacheControlForTTL accepts only "1h"; every other value maps to Claude's default 5 minute cache.
func cacheControlForTTL(ttl string) string {
	if ttl == ForceCacheTTL1h {
		return ephemeralCacheControl1h
	}
	return ephemeralCacheControl
}

// InjectForceCacheControl injects Claude cache_control blocks when the request has none.
// User-provided cache_control must pass through untouched so response usage remains upstream-authored.
func InjectForceCacheControl(jsonData []byte, ttl string) ([]byte, bool) {
	if len(jsonData) == 0 {
		return jsonData, false
	}
	if bytes.Contains(jsonData, []byte(`"cache_control"`)) {
		return jsonData, false
	}

	cacheControl := cacheControlForTTL(ttl)
	jsonStr := string(jsonData)
	jsonStr = injectSystemCache(jsonStr, cacheControl, ttl)
	jsonStr = injectToolsCache(jsonStr, cacheControl)
	jsonStr = injectMessagesCache(jsonStr, cacheControl, ttl)

	return []byte(jsonStr), true
}

// injectSystemCache injects cache_control into system while preserving Claude's accepted array shape.
func injectSystemCache(jsonStr string, cacheControl string, ttl string) string {
	system := gjson.Get(jsonStr, "system")
	if !system.Exists() {
		return jsonStr
	}

	switch {
	case system.IsArray():
		arr := system.Array()
		if len(arr) == 0 {
			return jsonStr
		}
		idx := len(arr) - 1
		if patched, err := sjson.SetRaw(jsonStr, "system."+itoa(idx)+".cache_control", cacheControl); err == nil {
			return patched
		}
	case system.Type == gjson.String:
		text := system.String()
		if text == "" {
			return jsonStr
		}
		block := buildTextBlockWithCache(text, ttl)
		raw, err := common.Marshal([]interface{}{block})
		if err != nil {
			return jsonStr
		}
		if patched, err := sjson.SetRawBytes([]byte(jsonStr), "system", raw); err == nil {
			return string(patched)
		}
	}
	return jsonStr
}

// injectToolsCache marks the last tool definition because Claude only needs one tools cache anchor.
func injectToolsCache(jsonStr string, cacheControl string) string {
	tools := gjson.Get(jsonStr, "tools")
	if !tools.Exists() || !tools.IsArray() {
		return jsonStr
	}
	arr := tools.Array()
	if len(arr) == 0 {
		return jsonStr
	}
	idx := len(arr) - 1
	if patched, err := sjson.SetRaw(jsonStr, "tools."+itoa(idx)+".cache_control", cacheControl); err == nil {
		return patched
	}
	return jsonStr
}

// injectMessagesCache marks the newest message blocks and normalizes string content to text blocks.
func injectMessagesCache(jsonStr string, cacheControl string, ttl string) string {
	messages := gjson.Get(jsonStr, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return jsonStr
	}
	arr := messages.Array()
	if len(arr) == 0 {
		return jsonStr
	}

	marked := 0
	for i := len(arr) - 1; i >= 0 && marked < 2; i-- {
		path := "messages." + itoa(i)
		content := gjson.Get(jsonStr, path+".content")
		if !content.Exists() {
			continue
		}
		switch {
		case content.IsArray():
			items := content.Array()
			if len(items) == 0 {
				continue
			}
			lastIdx := len(items) - 1
			blockPath := path + ".content." + itoa(lastIdx) + ".cache_control"
			if patched, err := sjson.SetRaw(jsonStr, blockPath, cacheControl); err == nil {
				jsonStr = patched
				marked++
			}
		case content.Type == gjson.String:
			text := content.String()
			if text == "" {
				continue
			}
			block := buildTextBlockWithCache(text, ttl)
			raw, err := common.Marshal([]interface{}{block})
			if err != nil {
				continue
			}
			if patched, err := sjson.SetRawBytes([]byte(jsonStr), path+".content", raw); err == nil {
				jsonStr = string(patched)
				marked++
			}
		}
	}
	return jsonStr
}

// buildTextBlockWithCache creates a text block with a cache_control object for normalized string content.
func buildTextBlockWithCache(text string, ttl string) map[string]interface{} {
	cache := map[string]interface{}{"type": "ephemeral"}
	if ttl == ForceCacheTTL1h {
		cache["ttl"] = "1h"
	}
	return map[string]interface{}{
		"type":          "text",
		"text":          text,
		"cache_control": cache,
	}
}

// ForceCacheControlTTLTo1h rewrites every supported Claude cache_control object to ttl=1h.
// It covers system, tools, and messages[*].content blocks.
func ForceCacheControlTTLTo1h(jsonData []byte) ([]byte, bool) {
	if len(jsonData) == 0 {
		return jsonData, false
	}
	if !bytes.Contains(jsonData, []byte(`"cache_control"`)) {
		return jsonData, false
	}
	jsonStr := string(jsonData)
	changed := false

	rewriteAt := func(parentPath string) {
		parent := gjson.Get(jsonStr, parentPath)
		if !parent.Exists() || !parent.IsArray() {
			return
		}
		for i, item := range parent.Array() {
			if item.Get("cache_control").Exists() {
				path := parentPath + "." + itoa(i) + ".cache_control.ttl"
				if patched, err := sjson.Set(jsonStr, path, "1h"); err == nil {
					if patched != jsonStr {
						changed = true
					}
					jsonStr = patched
				}
			}
		}
	}

	rewriteAt("system")
	rewriteAt("tools")

	messages := gjson.Get(jsonStr, "messages")
	if messages.Exists() && messages.IsArray() {
		for i := range messages.Array() {
			rewriteAt("messages." + itoa(i) + ".content")
		}
	}

	return []byte(jsonStr), changed
}

// itoa avoids an extra strconv dependency in request rewriting hot paths.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// ApplyForceCacheUsageZeroing folds Claude cache tokens back into input_tokens for forced-cache billing.
func ApplyForceCacheUsageZeroing(usage *dto.ClaudeUsage) {
	if usage == nil {
		return
	}
	extra := usage.CacheCreationInputTokens + usage.CacheReadInputTokens
	if extra > 0 {
		usage.InputTokens += extra
	}
	usage.CacheCreationInputTokens = 0
	usage.CacheReadInputTokens = 0
	if usage.CacheCreation != nil {
		usage.CacheCreation.Ephemeral5mInputTokens = 0
		usage.CacheCreation.Ephemeral1hInputTokens = 0
	}
	usage.ClaudeCacheCreation5mTokens = 0
	usage.ClaudeCacheCreation1hTokens = 0
}

// ApplyCache1hAs5mUsage moves Claude 1h cache creation tokens into the 5m bucket without changing totals.
func ApplyCache1hAs5mUsage(usage *dto.ClaudeUsage) {
	if usage == nil || usage.CacheCreation == nil {
		return
	}
	if usage.CacheCreation.Ephemeral1hInputTokens > 0 {
		usage.CacheCreation.Ephemeral5mInputTokens += usage.CacheCreation.Ephemeral1hInputTokens
		usage.CacheCreation.Ephemeral1hInputTokens = 0
	}
}

// RewriteClaudeResponseUsageBytes rewrites non-stream Claude response usage for forced-cache billing.
func RewriteClaudeResponseUsageBytes(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	usagePath := "usage"
	if !gjson.GetBytes(data, usagePath).Exists() {
		return data
	}
	return []byte(rewriteUsageInString(string(data), usagePath))
}

// RewriteClaudeStreamUsageString rewrites usage in one Claude SSE JSON event.
// It also handles message_start events where usage is nested under message.usage.
func RewriteClaudeStreamUsageString(data string) string {
	if data == "" {
		return data
	}
	if gjson.Get(data, "usage").Exists() {
		data = rewriteUsageInString(data, "usage")
	}
	if gjson.Get(data, "message.usage").Exists() {
		data = rewriteUsageInString(data, "message.usage")
	}
	return data
}

// RewriteClaudeResponseUsage1hAs5mBytes rewrites non-stream Claude usage from the 1h cache bucket to 5m.
func RewriteClaudeResponseUsage1hAs5mBytes(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	if !gjson.GetBytes(data, "usage").Exists() {
		return data
	}
	return []byte(rewrite1hAs5mInString(string(data), "usage"))
}

// RewriteClaudeStreamUsage1hAs5mString rewrites 1h cache usage in one Claude SSE JSON event.
// It also handles message_start events where usage is nested under message.usage.
func RewriteClaudeStreamUsage1hAs5mString(data string) string {
	if data == "" {
		return data
	}
	if gjson.Get(data, "usage").Exists() {
		data = rewrite1hAs5mInString(data, "usage")
	}
	if gjson.Get(data, "message.usage").Exists() {
		data = rewrite1hAs5mInString(data, "message.usage")
	}
	return data
}

// rewriteUsageInString folds cache usage into input_tokens at the requested usage path.
func rewriteUsageInString(data string, usagePath string) string {
	usage := gjson.Get(data, usagePath)
	if !usage.Exists() {
		return data
	}
	inputTokens := usage.Get("input_tokens").Int()
	cacheCreation := usage.Get("cache_creation_input_tokens").Int()
	cacheRead := usage.Get("cache_read_input_tokens").Int()
	newInput := inputTokens + cacheCreation + cacheRead

	if patched, err := sjson.Set(data, usagePath+".input_tokens", newInput); err == nil {
		data = patched
	}
	if patched, err := sjson.Set(data, usagePath+".cache_creation_input_tokens", 0); err == nil {
		data = patched
	}
	if patched, err := sjson.Set(data, usagePath+".cache_read_input_tokens", 0); err == nil {
		data = patched
	}
	if usage.Get("cache_creation").Exists() {
		if patched, err := sjson.Set(data, usagePath+".cache_creation.ephemeral_5m_input_tokens", 0); err == nil {
			data = patched
		}
		if patched, err := sjson.Set(data, usagePath+".cache_creation.ephemeral_1h_input_tokens", 0); err == nil {
			data = patched
		}
	}
	return data
}

// rewrite1hAs5mInString moves ephemeral_1h_input_tokens to ephemeral_5m_input_tokens without changing totals.
func rewrite1hAs5mInString(data string, usagePath string) string {
	cacheCreation := gjson.Get(data, usagePath+".cache_creation")
	if !cacheCreation.Exists() {
		return data
	}
	oneHour := cacheCreation.Get("ephemeral_1h_input_tokens").Int()
	if oneHour <= 0 {
		return data
	}
	fiveMin := cacheCreation.Get("ephemeral_5m_input_tokens").Int()
	if patched, err := sjson.Set(data, usagePath+".cache_creation.ephemeral_5m_input_tokens", fiveMin+oneHour); err == nil {
		data = patched
	}
	if patched, err := sjson.Set(data, usagePath+".cache_creation.ephemeral_1h_input_tokens", 0); err == nil {
		data = patched
	}
	return data
}

// NormalizeCacheReadMinRatio normalizes the minimum cache-read ratio to [0, 1).
// Values <= 0 disable the controller; values >= 1 are capped below 1 to avoid divide-by-zero behavior.
func NormalizeCacheReadMinRatio(ratio float64) float64 {
	if ratio != ratio {
		return 0
	}
	if ratio <= 0 {
		return 0
	}
	if ratio >= 1 {
		return 0.999999
	}
	return ratio
}

// ApplyCacheReadMinRatioUsage enforces a minimum read share for Claude cache usage.
// It keeps read+write totals stable and rescales cache_creation sub-buckets to match the new write total.
func ApplyCacheReadMinRatioUsage(usage *dto.ClaudeUsage, ratio float64) {
	if usage == nil || ratio <= 0 {
		return
	}
	read := usage.CacheReadInputTokens
	write := usage.CacheCreationInputTokens
	total := read + write
	if total <= 0 {
		return
	}
	if float64(read) >= float64(total)*ratio {
		return
	}
	newRead := int(float64(total)*ratio + 0.5)
	if newRead > total {
		newRead = total
	}
	newWrite := total - newRead

	usage.CacheReadInputTokens = newRead
	usage.CacheCreationInputTokens = newWrite

	if usage.CacheCreation != nil {
		oldFive := usage.CacheCreation.Ephemeral5mInputTokens
		oldOne := usage.CacheCreation.Ephemeral1hInputTokens
		oldSum := oldFive + oldOne
		if oldSum <= 0 {
			usage.CacheCreation.Ephemeral5mInputTokens = newWrite
			usage.CacheCreation.Ephemeral1hInputTokens = 0
		} else if newWrite == 0 {
			usage.CacheCreation.Ephemeral5mInputTokens = 0
			usage.CacheCreation.Ephemeral1hInputTokens = 0
		} else {
			scaledFive := int(float64(oldFive)*float64(newWrite)/float64(oldSum) + 0.5)
			if scaledFive > newWrite {
				scaledFive = newWrite
			}
			usage.CacheCreation.Ephemeral5mInputTokens = scaledFive
			usage.CacheCreation.Ephemeral1hInputTokens = newWrite - scaledFive
		}
	}

	if usage.ClaudeCacheCreation5mTokens != 0 || usage.ClaudeCacheCreation1hTokens != 0 {
		oldFive := usage.ClaudeCacheCreation5mTokens
		oldOne := usage.ClaudeCacheCreation1hTokens
		oldSum := oldFive + oldOne
		if oldSum <= 0 {
			usage.ClaudeCacheCreation5mTokens = newWrite
			usage.ClaudeCacheCreation1hTokens = 0
		} else if newWrite == 0 {
			usage.ClaudeCacheCreation5mTokens = 0
			usage.ClaudeCacheCreation1hTokens = 0
		} else {
			scaledFive := int(float64(oldFive)*float64(newWrite)/float64(oldSum) + 0.5)
			if scaledFive > newWrite {
				scaledFive = newWrite
			}
			usage.ClaudeCacheCreation5mTokens = scaledFive
			usage.ClaudeCacheCreation1hTokens = newWrite - scaledFive
		}
	}
}

// RewriteClaudeResponseUsageRatioBytes enforces the minimum cache-read ratio in non-stream Claude usage JSON.
func RewriteClaudeResponseUsageRatioBytes(data []byte, ratio float64) []byte {
	if len(data) == 0 || ratio <= 0 {
		return data
	}
	if !gjson.GetBytes(data, "usage").Exists() {
		return data
	}
	return []byte(rewriteUsageRatioInString(string(data), "usage", ratio))
}

// RewriteClaudeStreamUsageRatioString enforces the minimum cache-read ratio in one Claude SSE JSON event.
// It also handles message_start events where usage is nested under message.usage.
func RewriteClaudeStreamUsageRatioString(data string, ratio float64) string {
	if data == "" || ratio <= 0 {
		return data
	}
	if gjson.Get(data, "usage").Exists() {
		data = rewriteUsageRatioInString(data, "usage", ratio)
	}
	if gjson.Get(data, "message.usage").Exists() {
		data = rewriteUsageRatioInString(data, "message.usage", ratio)
	}
	return data
}

// rewriteUsageRatioInString rewrites cache read/write split while preserving the combined token total.
func rewriteUsageRatioInString(data string, usagePath string, ratio float64) string {
	usage := gjson.Get(data, usagePath)
	if !usage.Exists() {
		return data
	}
	read := usage.Get("cache_read_input_tokens").Int()
	write := usage.Get("cache_creation_input_tokens").Int()
	total := read + write
	if total <= 0 {
		return data
	}
	if float64(read) >= float64(total)*ratio {
		return data
	}
	newRead := int64(float64(total)*ratio + 0.5)
	if newRead > total {
		newRead = total
	}
	newWrite := total - newRead

	if patched, err := sjson.Set(data, usagePath+".cache_read_input_tokens", newRead); err == nil {
		data = patched
	}
	if patched, err := sjson.Set(data, usagePath+".cache_creation_input_tokens", newWrite); err == nil {
		data = patched
	}

	cacheCreation := gjson.Get(data, usagePath+".cache_creation")
	if cacheCreation.Exists() {
		oldFive := cacheCreation.Get("ephemeral_5m_input_tokens").Int()
		oldOne := cacheCreation.Get("ephemeral_1h_input_tokens").Int()
		oldSum := oldFive + oldOne
		var newFive, newOne int64
		switch {
		case newWrite == 0:
			newFive, newOne = 0, 0
		case oldSum <= 0:
			newFive, newOne = newWrite, 0
		default:
			scaled := int64(float64(oldFive)*float64(newWrite)/float64(oldSum) + 0.5)
			if scaled > newWrite {
				scaled = newWrite
			}
			newFive = scaled
			newOne = newWrite - scaled
		}
		if patched, err := sjson.Set(data, usagePath+".cache_creation.ephemeral_5m_input_tokens", newFive); err == nil {
			data = patched
		}
		if patched, err := sjson.Set(data, usagePath+".cache_creation.ephemeral_1h_input_tokens", newOne); err == nil {
			data = patched
		}
	}
	return data
}
