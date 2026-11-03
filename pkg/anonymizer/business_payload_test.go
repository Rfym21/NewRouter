package anonymizer

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"new-router/common"
)

const businessPayloadPrivate = "customer@private.example"

func businessPayload() map[string]any {
	return map[string]any{
		"metadata":  map[string]any{"email": businessPayloadPrivate},
		"signature": businessPayloadPrivate,
		"model":     businessPayloadPrivate,
		"nested": []any{
			map[string]any{"type": "thinking", "data": map[string]any{"text": businessPayloadPrivate}},
			map[string]any{"type": "image", "data": map[string]any{"text": businessPayloadPrivate}},
			businessPayloadPrivate,
		},
		businessPayloadPrivate: "ordinary value",
		"count":                json.Number("9007199254740993"),
		"enabled":              true,
		"optional":             nil,
	}
}

func encodedBusinessPayload(t *testing.T) string {
	t.Helper()
	encoded, err := common.Marshal(businessPayload())
	require.NoError(t, err)
	return string(encoded)
}

func decodedBusinessPayload(t *testing.T, value any) any {
	t.Helper()
	if encoded, ok := value.(string); ok {
		var decoded any
		require.NoError(t, common.UnmarshalWithNumber([]byte(encoded), &decoded))
		return decoded
	}
	return value
}

// Check the values and structure independently: private-looking business keys
// remain keys, while string values are never treated as protocol metadata.
func assertBusinessPayloadMasked(t *testing.T, original, masked any) {
	t.Helper()
	switch original := original.(type) {
	case map[string]any:
		actual, ok := masked.(map[string]any)
		require.True(t, ok)
		require.Len(t, actual, len(original))
		for key, value := range original {
			actualValue, exists := actual[key]
			require.True(t, exists, "business key must be preserved: %s", key)
			assertBusinessPayloadMasked(t, value, actualValue)
		}
	case []any:
		actual, ok := masked.([]any)
		require.True(t, ok)
		require.Len(t, actual, len(original))
		for index, value := range original {
			assertBusinessPayloadMasked(t, value, actual[index])
		}
	case string:
		if original == businessPayloadPrivate {
			assert.NotContains(t, masked, businessPayloadPrivate)
			assert.NotEqual(t, "", masked)
		} else {
			assert.Equal(t, original, masked)
		}
	default:
		assert.Equal(t, original, masked)
	}
}

func TestToolBusinessPayloadsDoNotInheritProtocolFieldExemptions(t *testing.T) {
	tests := []struct {
		name      string
		request   func(*testing.T) map[string]any
		transform func([]byte, *Session) ([]byte, error)
		payloads  func(map[string]any) []any
	}{
		{
			name: "chat legacy and current arguments",
			request: func(t *testing.T) map[string]any {
				return map[string]any{
					"model": businessPayloadPrivate,
					"messages": []any{map[string]any{
						"role": "assistant", "content": "ordinary text",
						"function_call": map[string]any{"name": businessPayloadPrivate, "arguments": encodedBusinessPayload(t)},
						"tool_calls":    []any{map[string]any{"id": businessPayloadPrivate, "type": "function", "function": map[string]any{"name": businessPayloadPrivate, "arguments": businessPayload()}}},
					}},
				}
			},
			transform: AnonymizeChatRequest,
			payloads: func(body map[string]any) []any {
				message := body["messages"].([]any)[0].(map[string]any)
				tool := message["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
				return []any{message["function_call"].(map[string]any)["arguments"], tool["arguments"]}
			},
		},
		{
			name: "responses tool inputs and outputs",
			request: func(t *testing.T) map[string]any {
				return map[string]any{"model": businessPayloadPrivate, "input": []any{
					map[string]any{"type": "function_call", "name": businessPayloadPrivate, "call_id": businessPayloadPrivate, "arguments": encodedBusinessPayload(t)},
					map[string]any{"type": "custom_tool_call", "name": businessPayloadPrivate, "call_id": businessPayloadPrivate, "input": businessPayload()},
					map[string]any{"type": "function_call_output", "call_id": businessPayloadPrivate, "output": encodedBusinessPayload(t)},
					map[string]any{"type": "custom_tool_call_output", "call_id": businessPayloadPrivate, "output": businessPayload()},
				}}
			},
			transform: AnonymizeResponsesRequest,
			payloads: func(body map[string]any) []any {
				items := body["input"].([]any)
				return []any{items[0].(map[string]any)["arguments"], items[1].(map[string]any)["input"], items[2].(map[string]any)["output"], items[3].(map[string]any)["output"]}
			},
		},
		{
			name: "claude input and JSON tool results",
			request: func(t *testing.T) map[string]any {
				return map[string]any{"model": businessPayloadPrivate, "messages": []any{
					map[string]any{"role": "assistant", "content": []any{
						map[string]any{"type": "tool_use", "id": businessPayloadPrivate, "name": businessPayloadPrivate, "input": businessPayload()},
					}},
					map[string]any{"role": "user", "content": []any{
						map[string]any{"type": "tool_result", "tool_use_id": businessPayloadPrivate, "content": encodedBusinessPayload(t)},
						map[string]any{"type": "tool_result", "tool_use_id": businessPayloadPrivate, "content": businessPayload()},
					}},
				}}
			},
			transform: AnonymizeClaudeRequest,
			payloads: func(body map[string]any) []any {
				messages := body["messages"].([]any)
				assistant := messages[0].(map[string]any)["content"].([]any)
				results := messages[1].(map[string]any)["content"].([]any)
				return []any{assistant[0].(map[string]any)["input"], results[0].(map[string]any)["content"], results[1].(map[string]any)["content"]}
			},
		},
		{
			name: "gemini function business data",
			request: func(t *testing.T) map[string]any {
				return map[string]any{"model": businessPayloadPrivate, "contents": []any{map[string]any{"role": "model", "parts": []any{
					map[string]any{"functionCall": map[string]any{"name": businessPayloadPrivate, "args": businessPayload()}},
					map[string]any{"functionResponse": map[string]any{"name": businessPayloadPrivate, "response": businessPayload()}},
					map[string]any{"function_call": map[string]any{"name": businessPayloadPrivate, "args": businessPayload()}},
					map[string]any{"function_response": map[string]any{"name": businessPayloadPrivate, "response": businessPayload()}},
				}}}}
			},
			transform: AnonymizeGeminiRequest,
			payloads: func(body map[string]any) []any {
				parts := body["contents"].([]any)[0].(map[string]any)["parts"].([]any)
				return []any{parts[0].(map[string]any)["functionCall"].(map[string]any)["args"], parts[1].(map[string]any)["functionResponse"].(map[string]any)["response"], parts[2].(map[string]any)["function_call"].(map[string]any)["args"], parts[3].(map[string]any)["function_response"].(map[string]any)["response"]}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original, err := common.Marshal(test.request(t))
			require.NoError(t, err)
			session := NewSession("business", nil)
			masked, err := test.transform(original, session)
			require.NoError(t, err)
			var body map[string]any
			require.NoError(t, common.UnmarshalWithNumber(masked, &body))
			assert.Equal(t, businessPayloadPrivate, body["model"], "protocol model stays structural")
			for _, payload := range test.payloads(body) {
				assertBusinessPayloadMasked(t, businessPayload(), decodedBusinessPayload(t, payload))
			}
			restored, err := DeanonymizeTextFields(masked, session)
			require.NoError(t, err)
			assert.JSONEq(t, string(original), string(restored))
		})
	}
}

func TestSignedProtocolBlocksStayOpaqueBesideToolBusinessData(t *testing.T) {
	tests := []struct {
		name      string
		request   string
		transform func([]byte, *Session) ([]byte, error)
		protected func(map[string]any) []any
	}{
		{
			name: "claude signed and redacted thoughts",
			request: `{"messages":[{"role":"assistant","content":[
			 {"type":"tool_use","id":"call-1","name":"lookup","input":{"email":"customer@private.example"}},
			 {"type":"thinking","thinking":"customer@private.example","signature":"customer@private.example"},
			 {"type":"redacted_thinking","data":"customer@private.example"},
			 {"type":"text","text":"customer@private.example","signature":"customer@private.example"}
			]}]}`,
			transform: AnonymizeClaudeRequest,
			protected: func(body map[string]any) []any {
				return body["messages"].([]any)[0].(map[string]any)["content"].([]any)[1:]
			},
		},
		{
			name: "gemini signed text and function parts",
			request: `{"contents":[{"role":"model","parts":[
			 {"functionCall":{"name":"lookup","args":{"email":"customer@private.example"}}},
			 {"text":"customer@private.example","thoughtSignature":"customer@private.example"},
			 {"functionCall":{"name":"lookup","args":{"email":"customer@private.example"}},"thoughtSignature":"customer@private.example"},
			 {"text":"customer@private.example","thought_signature":"customer@private.example"}
			]}]}`,
			transform: AnonymizeGeminiRequest,
			protected: func(body map[string]any) []any {
				return body["contents"].([]any)[0].(map[string]any)["parts"].([]any)[1:]
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var original map[string]any
			require.NoError(t, common.UnmarshalWithNumber([]byte(test.request), &original))
			session := NewSession("signed-protocol", nil)
			masked, err := test.transform([]byte(test.request), session)
			require.NoError(t, err)
			var body map[string]any
			require.NoError(t, common.UnmarshalWithNumber(masked, &body))
			assert.Equal(t, test.protected(original), test.protected(body))
			restored, err := DeanonymizeTextFields(masked, session)
			require.NoError(t, err)
			assert.JSONEq(t, test.request, string(restored))
		})
	}
}

func TestGeminiEmbeddingRequestsAnonymizeTextAndPreserveNativeStructure(t *testing.T) {
	content := func() map[string]any {
		return map[string]any{"role": "user", "parts": []any{
			map[string]any{"text": businessPayloadPrivate},
			map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": businessPayloadPrivate}},
		}}
	}
	tests := []struct {
		name     string
		request  map[string]any
		contents func(map[string]any) []map[string]any
	}{
		{
			name: "single embedding",
			request: map[string]any{
				"model": businessPayloadPrivate, "content": content(), "taskType": "RETRIEVAL_DOCUMENT", "outputDimensionality": json.Number("768"),
			},
			contents: func(body map[string]any) []map[string]any {
				return []map[string]any{body["content"].(map[string]any)}
			},
		},
		{
			name: "batch embeddings",
			request: map[string]any{"requests": []any{
				map[string]any{"model": businessPayloadPrivate, "content": content(), "taskType": "RETRIEVAL_QUERY"},
				map[string]any{"model": businessPayloadPrivate, "content": content(), "outputDimensionality": json.Number("768")},
			}},
			contents: func(body map[string]any) []map[string]any {
				requests := body["requests"].([]any)
				return []map[string]any{requests[0].(map[string]any)["content"].(map[string]any), requests[1].(map[string]any)["content"].(map[string]any)}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original, err := common.Marshal(test.request)
			require.NoError(t, err)
			session := NewSession("embedding", nil)
			masked, err := AnonymizeGeminiRequest(original, session)
			require.NoError(t, err)
			var body map[string]any
			require.NoError(t, common.UnmarshalWithNumber(masked, &body))
			for _, content := range test.contents(body) {
				parts := content["parts"].([]any)
				assert.NotEqual(t, businessPayloadPrivate, parts[0].(map[string]any)["text"])
				assert.Equal(t, businessPayloadPrivate, parts[1].(map[string]any)["inlineData"].(map[string]any)["data"])
			}
			restored, err := DeanonymizeTextFields(masked, session)
			require.NoError(t, err)
			assert.JSONEq(t, string(original), string(restored))
		})
	}
}
