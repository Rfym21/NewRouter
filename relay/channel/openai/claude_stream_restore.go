package openai

import (
	"strconv"
	"strings"

	"new-router/pkg/anonymizer"
	"new-router/relaykit/dto"
)

// restoreConvertedClaudeChunk covers the direct Responses-to-Claude converter,
// whose output no longer passes through an intermediate Chat completion chunk.
func restoreConvertedClaudeChunk(response *dto.ClaudeResponse, restorer *anonymizer.StreamRestorer) []*dto.ClaudeResponse {
	if response == nil {
		return nil
	}
	if restorer == nil {
		return []*dto.ClaudeResponse{response}
	}
	index := 0
	if response.Index != nil {
		index = *response.Index
	}
	prefix := "claude:" + strconv.Itoa(index) + ":"
	var chunks []*dto.ClaudeResponse
	if response.Type == "content_block_stop" {
		for _, field := range []string{"text", "thinking"} {
			if chunk := flushConvertedClaudeResidual(restorer, prefix+field); chunk != nil {
				chunks = append(chunks, chunk)
			}
		}
	}
	if response.Type == "content_block_delta" && response.Delta != nil {
		if response.Delta.Type == "text_delta" && response.Delta.Text != nil {
			text := restorer.Push(prefix+"text", *response.Delta.Text)
			response.Delta.Text = &text
		}
		if response.Delta.Type == "thinking_delta" && response.Delta.Thinking != nil {
			text := restorer.Push(prefix+"thinking", *response.Delta.Thinking)
			response.Delta.Thinking = &text
		}
	}
	return append(chunks, response)
}

func flushConvertedClaudeResidual(restorer *anonymizer.StreamRestorer, key string) *dto.ClaudeResponse {
	if restorer == nil || !strings.HasPrefix(key, "claude:") {
		return nil
	}
	text := restorer.Flush(key)
	if text == "" {
		return nil
	}
	indexText, field, ok := strings.Cut(strings.TrimPrefix(key, "claude:"), ":")
	if !ok {
		return nil
	}
	index, err := strconv.Atoi(indexText)
	if err != nil {
		return nil
	}
	delta := &dto.ClaudeMediaMessage{Type: "text_delta", Text: &text}
	if field == "thinking" {
		delta = &dto.ClaudeMediaMessage{Type: "thinking_delta", Thinking: &text}
	}
	return &dto.ClaudeResponse{Type: "content_block_delta", Index: &index, Delta: delta}
}
