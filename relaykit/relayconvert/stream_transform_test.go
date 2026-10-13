package relayconvert

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"new-router/relaykit/dto"
	"new-router/relaykit/types"
)

func TestStreamTransformFeedsNextProtocolConverter(t *testing.T) {
	seenChat := false
	state, err := NewResponseStreamState(types.RelayFormatGemini, types.RelayFormatOpenAIResponses, ResponseStreamOptions{
		ID: "resp_1", Model: "test",
		Transform: func(step ResponseStep, value any) (any, error) {
			if step.To != types.RelayFormatOpenAI {
				return value, nil
			}
			chunk, ok := value.(*dto.ChatCompletionsStreamResponse)
			if !ok {
				return value, nil
			}
			for i := range chunk.Choices {
				if chunk.Choices[i].Delta.GetContentString() == "surrogate" {
					chunk.Choices[i].Delta.SetContentString("original")
					seenChat = true
				}
			}
			return chunk, nil
		},
	})
	require.NoError(t, err)
	var input dto.GeminiChatResponse
	require.NoError(t, json.Unmarshal([]byte(`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"surrogate"}]}}]}`), &input))
	results, err := ConvertStreamResponseChunk(context.Background(), nil, state, &input)
	require.NoError(t, err)
	require.True(t, seenChat)
	var text strings.Builder
	for _, result := range results {
		event, ok := result.Value.(ChatToResponsesStreamEvent)
		if ok && event.Type == "response.output_text.delta" {
			text.WriteString(event.Payload.Delta)
		}
	}
	require.Equal(t, "original", text.String())
}
