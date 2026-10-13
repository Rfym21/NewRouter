package openai

import (
	"strconv"

	"new-router/common"
	"new-router/dto"
	"new-router/logger"
	"new-router/pkg/anonymizer"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// deanonymizeChatStreamChunk restores anonymized values in a typed
// chat.completion.chunk, reassembling surrogates split across SSE chunks via the
// per-stream carry buffer (keyed by choice index + field). Tool-call arguments use
// the stateless whole-value restore. When restorer is nil (anonymization disabled)
// or no session is attached, the chunk is left byte-identical.
func deanonymizeChatStreamChunk(c *gin.Context, chunk *dto.ChatCompletionsStreamResponse, restorer *anonymizer.StreamRestorer) {
	if restorer == nil || chunk == nil {
		return
	}
	session := anonymizer.FromContext(c)
	if session == nil {
		return
	}
	for i := range chunk.Choices {
		choice := &chunk.Choices[i]
		key := strconv.Itoa(choice.Index)
		if choice.Delta.Content != nil {
			restored := restorer.Push(key+":content", *choice.Delta.Content)
			choice.Delta.Content = &restored
		}
		if choice.Delta.ReasoningContent != nil {
			restored := restorer.Push(key+":reasoning_content", *choice.Delta.ReasoningContent)
			choice.Delta.ReasoningContent = &restored
		}
		if choice.Delta.Reasoning != nil {
			restored := restorer.Push(key+":reasoning", *choice.Delta.Reasoning)
			choice.Delta.Reasoning = &restored
		}
		for j := range choice.Delta.ToolCalls {
			args := choice.Delta.ToolCalls[j].Function.Arguments
			if args != "" {
				choice.Delta.ToolCalls[j].Function.Arguments = session.DeanonymizeString(args)
			}
		}
	}
}

func deanonymizeResponseBytes(c *gin.Context, data []byte) ([]byte, error) {
	session := anonymizer.FromContext(c)
	if session == nil {
		return data, nil
	}
	restored, err := anonymizer.DeanonymizeTextFields(data, session)
	if err != nil {
		// Best-effort: an unparseable chunk (e.g. a non-JSON keepalive line) has
		// no restorable content — pass it through rather than aborting the stream.
		if common.DebugEnabled {
			logger.LogDebug(c, "deanonymize response skipped (unparseable): %s", err.Error())
		}
		return data, nil
	}
	return restored, nil
}

func deanonymizeResponseString(c *gin.Context, data string) (string, error) {
	if data == "" {
		return data, nil
	}
	out, err := deanonymizeResponseBytes(c, []byte(data))
	if err != nil {
		return data, err
	}
	return string(out), nil
}

// streamDeltaTextFields are the incremental-text fields of a chat.completion.chunk
// delta that are reassembled across SSE chunks by the carry buffer.
var streamDeltaTextFields = []string{"content", "reasoning_content", "reasoning"}

// deanonymizeStreamChunk restores anonymized values in one chat.completion.chunk,
// reassembling surrogates that are split across SSE chunks via the per-stream
// carry buffer. Incremental text fields are routed through restorer.Push (keyed
// per choice index + field); tool-call arguments keep the stateless whole-value
// restore (cross-chunk JSON-fragment reassembly is out of scope). It edits only
// those string fields via sjson so all other bytes (id/created/usage/…) are
// preserved verbatim. When restorer is nil (anonymization disabled) it falls back
// to the stateless whole-chunk restore, keeping behavior byte-identical.
func deanonymizeStreamChunk(c *gin.Context, data string, restorer *anonymizer.StreamRestorer) (string, error) {
	if restorer == nil {
		return deanonymizeResponseString(c, data)
	}
	session := anonymizer.FromContext(c)
	if session == nil {
		return data, nil
	}
	choices := gjson.Get(data, "choices")
	if !choices.IsArray() {
		// Unexpected shape (e.g. a non-chunk line) — fall back to stateless restore.
		return deanonymizeResponseString(c, data)
	}
	out := data
	for pos, choice := range choices.Array() {
		logicalIdx := pos
		if v := choice.Get("index"); v.Exists() {
			logicalIdx = int(v.Int())
		}
		delta := choice.Get("delta")
		if !delta.Exists() {
			continue
		}
		posStr := strconv.Itoa(pos)
		for _, field := range streamDeltaTextFields {
			fv := delta.Get(field)
			if fv.Type != gjson.String {
				continue
			}
			flushed := restorer.Push(strconv.Itoa(logicalIdx)+":"+field, fv.String())
			if patched, err := sjson.Set(out, "choices."+posStr+".delta."+field, flushed); err == nil {
				out = patched
			}
		}
		toolCalls := delta.Get("tool_calls")
		if !toolCalls.IsArray() {
			continue
		}
		for tpos, tc := range toolCalls.Array() {
			args := tc.Get("function.arguments")
			if args.Type != gjson.String {
				continue
			}
			restored := session.DeanonymizeString(args.String())
			if restored == args.String() {
				continue
			}
			path := "choices." + posStr + ".delta.tool_calls." + strconv.Itoa(tpos) + ".function.arguments"
			if patched, err := sjson.Set(out, path, restored); err == nil {
				out = patched
			}
		}
	}
	return out, nil
}
