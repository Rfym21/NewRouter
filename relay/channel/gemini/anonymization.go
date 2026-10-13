package gemini

import (
	"strconv"

	"new-router/common"
	"new-router/dto"
	"new-router/logger"
	"new-router/pkg/anonymizer"

	"github.com/gin-gonic/gin"
)

func deanonymizeResponseBytes(c *gin.Context, data []byte) ([]byte, error) {
	session := anonymizer.FromContext(c)
	if session == nil {
		return data, nil
	}
	restored, err := anonymizer.DeanonymizeTextFields(data, session)
	if err != nil {
		// Best-effort: an unparseable chunk has no restorable content — pass it
		// through rather than aborting the stream.
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

// deanonymizeGeminiOpenAIChunk restores anonymized values in a Gemini-to-OpenAI
// converted stream chunk, routing incremental content/reasoning through the
// per-stream carry buffer (keyed by choice index + field) so surrogates split
// across chunks are reassembled. Tool-call arguments use the stateless whole-value
// restore. A nil restorer or absent session leaves the chunk unchanged.
func deanonymizeGeminiOpenAIChunk(c *gin.Context, chunk *dto.ChatCompletionsStreamResponse, restorer *anonymizer.StreamRestorer) {
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
			restored := restorer.Push(key+":reasoning", *choice.Delta.ReasoningContent)
			choice.Delta.ReasoningContent = &restored
		}
		for j := range choice.Delta.ToolCalls {
			args := choice.Delta.ToolCalls[j].Function.Arguments
			if args != "" {
				choice.Delta.ToolCalls[j].Function.Arguments = session.DeanonymizeString(args)
			}
		}
	}
}
