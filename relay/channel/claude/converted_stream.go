package claude

import (
	"sort"
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
	// Restore a client copy; usage estimation must retain upstream text and JSON.
	clientResponse := *response
	response = &clientResponse
	if response.Delta != nil {
		delta := *response.Delta
		response.Delta = &delta
	}
	index := 0
	if response.Index != nil {
		index = *response.Index
	}
	var chunks []*dto.ClaudeResponse
	if response.Type == "content_block_stop" {
		chunks = flushConvertedClaudeChunks(restorer, strconv.Itoa(index)+":")
	} else if response.Type == "message_stop" || response.Type == "message_delta" {
		chunks = flushConvertedClaudeChunks(restorer, "")
	}
	if response.Type == "content_block_delta" && response.Delta != nil {
		if response.Delta.Type == "text_delta" && response.Delta.Text != nil {
			text := restorer.Push(strconv.Itoa(index)+":text", *response.Delta.Text)
			response.Delta.Text = &text
		}
		if response.Delta.Type == "input_json_delta" && response.Delta.PartialJson != nil {
			text := restorer.PushJSON(strconv.Itoa(index)+":arguments", *response.Delta.PartialJson)
			response.Delta.PartialJson = &text
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
	keys := append(restorer.PendingKeys(), restorer.PendingJSONKeys()...)
	sort.Strings(keys)
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
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
		text := ""
		if field == "arguments" {
			text = restorer.FlushJSON(key)
		} else {
			text = restorer.Flush(key)
		}
		if text == "" {
			continue
		}
		delta := &dto.ClaudeMediaMessage{Type: "text_delta", Text: &text}
		if field == "arguments" {
			delta = &dto.ClaudeMediaMessage{Type: "input_json_delta", PartialJson: &text}
		}
		chunks = append(chunks, &dto.ClaudeResponse{Type: "content_block_delta", Index: &index, Delta: delta})
	}
	return chunks
}
