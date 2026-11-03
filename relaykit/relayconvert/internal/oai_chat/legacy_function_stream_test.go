package oaichat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"new-router/relaykit/dto"
	"new-router/relaykit/relayconvert/convmeta"
)

func legacyFunctionChunks(t *testing.T) ([]*dto.ChatCompletionsStreamResponse, string) {
	t.Helper()
	arguments, err := json.Marshal(map[string]string{"value": "private \"line\"\npath\\folder"})
	require.NoError(t, err)
	raw := string(arguments)
	split := strings.Index(raw, "\\\"") + 1 // Split the inner JSON escape itself.
	chunks := make([]*dto.ChatCompletionsStreamResponse, 0, 3)
	for i, fragment := range []string{raw[:split], raw[split:]} {
		name := ""
		if i == 0 {
			name = "lookup"
		}
		chunks = append(chunks, &dto.ChatCompletionsStreamResponse{
			Id: "chat_legacy", Model: "gpt-test", Choices: []dto.ChatCompletionsStreamResponseChoice{{
				Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					FunctionCall: &dto.FunctionResponse{Name: name, Arguments: fragment},
				},
			}},
		})
	}
	reason := "function_call"
	chunks = append(chunks, &dto.ChatCompletionsStreamResponse{
		Id: "chat_legacy", Model: "gpt-test", Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0, FinishReason: &reason,
		}},
		Usage: &dto.Usage{PromptTokens: 17, CompletionTokens: 3, TotalTokens: 20},
	})
	return chunks, raw
}

func TestLegacyFunctionArgumentFragmentsConvertToClaudeWithoutMutatingSource(t *testing.T) {
	chunks, want := legacyFunctionChunks(t)
	info := &convmeta.Values{}
	var arguments strings.Builder
	var starts, stops int
	var callID, stopReason string
	for _, chunk := range chunks {
		before, err := json.Marshal(chunk)
		require.NoError(t, err)
		info.SendResponseCount++
		for _, response := range StreamResponseOpenAI2Claude(chunk, info) {
			switch response.Type {
			case "content_block_start":
				if response.ContentBlock.Type == "tool_use" {
					starts++
					callID = response.ContentBlock.Id
					assert.Equal(t, "lookup", response.ContentBlock.Name)
				}
			case "content_block_delta":
				if response.Delta.PartialJson != nil {
					arguments.WriteString(*response.Delta.PartialJson)
				}
			case "content_block_stop":
				stops++
			case "message_delta":
				stopReason = *response.Delta.StopReason
				require.NotNil(t, response.Usage)
				assert.Equal(t, 3, response.Usage.OutputTokens)
			}
		}
		after, err := json.Marshal(chunk)
		require.NoError(t, err)
		assert.Equal(t, string(before), string(after), "native schema and upstream billing snapshot are immutable")
	}
	assert.Equal(t, 1, starts)
	assert.Equal(t, 1, stops)
	assert.Equal(t, "call_chat_legacy_legacy_0", callID)
	assert.Equal(t, "tool_use", stopReason)
	assert.Equal(t, want, arguments.String())
}

func TestLegacyFunctionArgumentFragmentsConvertToGeminiWithoutMutatingSource(t *testing.T) {
	chunks, want := legacyFunctionChunks(t)
	state := NewChatToGeminiStreamState()
	var calls []*dto.FunctionCall
	for _, chunk := range chunks {
		before, err := json.Marshal(chunk)
		require.NoError(t, err)
		responses, err := state.ConvertChunk(chunk, &convmeta.Values{})
		require.NoError(t, err)
		for _, response := range responses {
			for _, candidate := range response.Candidates {
				for _, part := range candidate.Content.Parts {
					if part.FunctionCall != nil {
						calls = append(calls, part.FunctionCall)
					}
				}
			}
		}
		after, err := json.Marshal(chunk)
		require.NoError(t, err)
		assert.Equal(t, string(before), string(after), "native schema and upstream billing snapshot are immutable")
	}
	require.Len(t, calls, 1)
	assert.Equal(t, "lookup", calls[0].FunctionName)
	assert.Equal(t, "call_chat_legacy_legacy_0", calls[0].ID)
	var arguments map[string]any
	require.NoError(t, json.Unmarshal([]byte(want), &arguments))
	assert.Equal(t, arguments, calls[0].Arguments)
	require.NotNil(t, state.Usage())
	assert.Equal(t, 3, state.Usage().CompletionTokens)
}

func TestLegacyToolConversionGetterKeepsModernCallsAuthoritative(t *testing.T) {
	choice := dto.ChatCompletionsStreamResponseChoice{Index: 2, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
		FunctionCall: &dto.FunctionResponse{Name: "legacy", Arguments: "{}"},
		ToolCalls:    []dto.ToolCallResponse{{ID: "modern", Function: dto.FunctionResponse{Name: "modern", Arguments: "{}"}}},
	}}
	calls := choice.ToolCallsForConversion("response")
	require.Len(t, calls, 1)
	assert.Equal(t, "modern", calls[0].ID)
	assert.NotNil(t, choice.Delta.FunctionCall)
}
