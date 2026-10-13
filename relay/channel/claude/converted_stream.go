package claude

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"new-router/common"
	"new-router/constant"
	"new-router/pkg/anonymizer"
	"new-router/relaykit/dto"
)

// prepareConvertedClaudeChunk applies channel controls before the new native
// Claude-to-Responses/Gemini converters capture their billing and text state.
func prepareConvertedClaudeChunk(c *gin.Context, response *dto.ClaudeResponse, restorer *anonymizer.StreamRestorer) []*dto.ClaudeResponse {
	if response == nil {
		return nil
	}
	forceCache := common.GetContextKeyBool(c, constant.ContextKeyClaudeForceCacheInjected)
	cache1hAs5m := common.GetContextKeyBool(c, constant.ContextKeyClaudeCache1hAs5m)
	cacheReadMinRatio := common.GetContextKeyFloat64(c, constant.ContextKeyClaudeCacheReadMinRatio)
	if forceCache || cache1hAs5m || cacheReadMinRatio > 0 {
		if response.Message != nil {
			applyClaudeUsageRewrites(response.Message.Usage, forceCache, cache1hAs5m, cacheReadMinRatio)
		}
		applyClaudeUsageRewrites(response.Usage, forceCache, cache1hAs5m, cacheReadMinRatio)
	}
	if restorer == nil {
		return []*dto.ClaudeResponse{response}
	}
	index := 0
	if response.Index != nil {
		index = *response.Index
	}
	var chunks []*dto.ClaudeResponse
	if response.Type == "content_block_stop" {
		chunks = flushConvertedClaudeChunks(restorer, strconv.Itoa(index)+":")
	}
	if response.Type == "content_block_delta" && response.Delta != nil {
		if response.Delta.Type == "text_delta" && response.Delta.Text != nil {
			text := restorer.Push(strconv.Itoa(index)+":text", *response.Delta.Text)
			response.Delta.Text = &text
		}
		if response.Delta.Type == "thinking_delta" && response.Delta.Thinking != nil {
			text := restorer.Push(strconv.Itoa(index)+":thinking", *response.Delta.Thinking)
			response.Delta.Thinking = &text
		}
	}
	if data, err := common.Marshal(response); err == nil {
		if data, err = deanonymizeResponseBytes(c, data); err == nil {
			_ = common.Unmarshal(data, response)
		}
	}
	return append(chunks, response)
}

func flushConvertedClaudeChunks(restorer *anonymizer.StreamRestorer, prefix string) []*dto.ClaudeResponse {
	if restorer == nil {
		return nil
	}
	var chunks []*dto.ClaudeResponse
	for _, key := range restorer.PendingKeys() {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		text := restorer.Flush(key)
		if text == "" {
			continue
		}
		indexText, field, ok := strings.Cut(key, ":")
		if !ok {
			continue
		}
		index, err := strconv.Atoi(indexText)
		if err != nil {
			continue
		}
		delta := &dto.ClaudeMediaMessage{Type: "text_delta", Text: &text}
		if field == "thinking" {
			delta = &dto.ClaudeMediaMessage{Type: "thinking_delta", Thinking: &text}
		}
		chunks = append(chunks, &dto.ClaudeResponse{Type: "content_block_delta", Index: &index, Delta: delta})
	}
	return chunks
}
