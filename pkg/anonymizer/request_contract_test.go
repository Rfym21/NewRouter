package anonymizer

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"new-router/common"
)

func requestFixture(t *testing.T, source string, transform func([]byte, *Session) ([]byte, error)) (map[string]any, *Session, []byte) {
	t.Helper()
	session := NewSession("protocol", NewVault())
	encoded, err := transform([]byte(source), session)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.UnmarshalWithNumber(encoded, &body))
	return body, session, encoded
}

func TestChatRequestAnonymizesAssistantHistoryAndBothToolCallGenerations(t *testing.T) {
	const private = "alice@tenant.test"
	body, session, encoded := requestFixture(t, `{
	 "model":"alice@tenant.test",
	 "metadata":{"text":"alice@tenant.test"},
	 "tools":[{"type":"function","function":{"name":"alice@tenant.test","parameters":{"properties":{"text":{"default":"alice@tenant.test"}}}}}],
	 "messages":[
	  {"role":"assistant","name":"alice@tenant.test","content":"Contact alice@tenant.test","refusal":"alice@tenant.test","function_call":{"name":"lookup","arguments":"{\"email\":\"alice@tenant.test\",\"n\":9007199254740993}"},"tool_calls":[{"id":"alice@tenant.test","function":{"name":"lookup","arguments":{"email":"alice@tenant.test","n":9007199254740993}}}]},
	  {"role":"function","content":"alice@tenant.test"},
	  {"role":"user","content":[{"type":"text","text":"alice@tenant.test"},{"type":"image_url","image_url":{"url":"https://tenant.test/alice@tenant.test"}}]}
	 ]}`, AnonymizeChatRequest)
	assert.Equal(t, private, body["model"])
	assert.Equal(t, private, body["metadata"].(map[string]any)["text"])
	assert.Contains(t, string(encoded), "9007199254740993")
	messages := body["messages"].([]any)
	assistant := messages[0].(map[string]any)
	assert.NotContains(t, assistant["content"], private)
	assert.NotContains(t, assistant["refusal"], private)
	assert.Equal(t, private, assistant["name"])
	legacy := assistant["function_call"].(map[string]any)
	assert.NotContains(t, legacy["arguments"], private)
	var args map[string]any
	require.NoError(t, common.UnmarshalWithNumber([]byte(legacy["arguments"].(string)), &args))
	assert.Equal(t, json.Number("9007199254740993"), args["n"])
	tool := assistant["tool_calls"].([]any)[0].(map[string]any)
	assert.Equal(t, private, tool["id"])
	assert.IsType(t, map[string]any{}, tool["function"].(map[string]any)["arguments"])
	assert.NotContains(t, messages[1].(map[string]any)["content"], private)
	parts := messages[2].(map[string]any)["content"].([]any)
	assert.NotContains(t, parts[0].(map[string]any)["text"], private)
	assert.Equal(t, "https://tenant.test/alice@tenant.test", parts[1].(map[string]any)["image_url"].(map[string]any)["url"])
	restored, err := DeanonymizeTextFields(encoded, session)
	require.NoError(t, err)
	assert.Contains(t, string(restored), `Contact alice@tenant.test`)
}

func TestResponsesAndCompactRequestsPreserveOpaqueItemsAndMaskToolPayloads(t *testing.T) {
	body, _, _ := requestFixture(t, `{
	 "model":"gpt-test","instructions":"alice@tenant.test","previous_response_id":"alice@tenant.test",
	 "input":[
	  {"type":"message","role":"assistant","id":"alice@tenant.test","content":[{"type":"output_text","text":"alice@tenant.test"}]},
	  {"type":"function_call","call_id":"alice@tenant.test","name":"lookup","arguments":"{\"email\":\"alice@tenant.test\"}"},
	  {"type":"function_call_output","call_id":"alice@tenant.test","output":{"result":{"email":"alice@tenant.test"},"count":9007199254740993}},
	  {"type":"custom_tool_call","name":"lookup","input":"alice@tenant.test"},
	  {"type":"custom_tool_call_output","output":[{"type":"input_text","text":"alice@tenant.test"},{"type":"input_image","image_url":"https://tenant.test/alice@tenant.test"}]},
	  {"type":"reasoning","id":"reasoning-1","encrypted_content":"alice@tenant.test","summary":[{"type":"summary_text","text":"alice@tenant.test"}]},
	  {"type":"compaction","id":"compact-1","encrypted_content":"alice@tenant.test"}
	 ]}`, AnonymizeResponsesRequest)
	assert.NotEqual(t, "alice@tenant.test", body["instructions"])
	assert.Equal(t, "alice@tenant.test", body["previous_response_id"])
	items := body["input"].([]any)
	assert.Equal(t, "alice@tenant.test", items[0].(map[string]any)["id"])
	assert.NotContains(t, items[1].(map[string]any)["arguments"], "alice@tenant.test")
	assert.Equal(t, "alice@tenant.test", items[1].(map[string]any)["call_id"])
	output := items[2].(map[string]any)["output"].(map[string]any)
	assert.NotEqual(t, "alice@tenant.test", output["result"].(map[string]any)["email"])
	assert.Equal(t, json.Number("9007199254740993"), output["count"])
	assert.NotEqual(t, "alice@tenant.test", items[3].(map[string]any)["input"])
	parts := items[4].(map[string]any)["output"].([]any)
	assert.NotEqual(t, "alice@tenant.test", parts[0].(map[string]any)["text"])
	assert.Equal(t, "https://tenant.test/alice@tenant.test", parts[1].(map[string]any)["image_url"])
	assert.Equal(t, "alice@tenant.test", items[5].(map[string]any)["encrypted_content"])
	assert.NotEqual(t, "alice@tenant.test", items[5].(map[string]any)["summary"].([]any)[0].(map[string]any)["text"])
	assert.Equal(t, "alice@tenant.test", items[6].(map[string]any)["encrypted_content"])
}

func TestClaudeNativeRequestCoversToolInputsResultsAndProtectsThinking(t *testing.T) {
	body, _, _ := requestFixture(t, `{
	 "system":[{"type":"text","text":"alice@tenant.test","cache_control":{"type":"ephemeral"}}],
	 "metadata":{"user_id":"alice@tenant.test"},
	 "tools":[{"name":"lookup","input_schema":{"properties":{"text":{"default":"alice@tenant.test"}}}}],
	 "messages":[{"role":"assistant","content":[
	  {"type":"text","text":"alice@tenant.test"},
	  {"type":"tool_use","id":"tool-1","name":"lookup","input":{"email":"alice@tenant.test"}},
	  {"type":"thinking","thinking":"alice@tenant.test","signature":"signed"},
	  {"type":"redacted_thinking","data":"alice@tenant.test"}
	 ]},{"role":"user","content":[
	  {"type":"tool_result","tool_use_id":"tool-1","content":{"email":"alice@tenant.test"}},
	  {"type":"image","source":{"type":"base64","data":"alice@tenant.test"}}
	 ]}]}`, AnonymizeClaudeRequest)
	assert.NotEqual(t, "alice@tenant.test", body["system"].([]any)[0].(map[string]any)["text"])
	assert.Equal(t, "alice@tenant.test", body["metadata"].(map[string]any)["user_id"])
	messages := body["messages"].([]any)
	parts := messages[0].(map[string]any)["content"].([]any)
	assert.NotEqual(t, "alice@tenant.test", parts[0].(map[string]any)["text"])
	assert.NotEqual(t, "alice@tenant.test", parts[1].(map[string]any)["input"].(map[string]any)["email"])
	assert.Equal(t, "alice@tenant.test", parts[2].(map[string]any)["thinking"])
	assert.Equal(t, "alice@tenant.test", parts[3].(map[string]any)["data"])
	results := messages[1].(map[string]any)["content"].([]any)
	assert.NotEqual(t, "alice@tenant.test", results[0].(map[string]any)["content"].(map[string]any)["email"])
	assert.Equal(t, "alice@tenant.test", results[1].(map[string]any)["source"].(map[string]any)["data"])
}

func TestGeminiNativeRequestAndResponseHandleFunctionPayloads(t *testing.T) {
	body, session, encoded := requestFixture(t, `{
	 "systemInstruction":{"parts":[{"text":"alice@tenant.test"}]},
	 "contents":[{"role":"model","parts":[
	  {"text":"alice@tenant.test"},
	  {"functionCall":{"name":"lookup","args":{"email":"alice@tenant.test"}}},
	  {"functionResponse":{"name":"lookup","response":{"result":{"email":"alice@tenant.test"}}}},
	  {"text":"alice@tenant.test","thoughtSignature":"signed"},
	  {"inlineData":{"mimeType":"image/png","data":"alice@tenant.test"}}
	 ]}]}`, AnonymizeGeminiRequest)
	parts := body["contents"].([]any)[0].(map[string]any)["parts"].([]any)
	assert.NotEqual(t, "alice@tenant.test", parts[0].(map[string]any)["text"])
	assert.NotEqual(t, "alice@tenant.test", parts[1].(map[string]any)["functionCall"].(map[string]any)["args"].(map[string]any)["email"])
	assert.NotEqual(t, "alice@tenant.test", parts[2].(map[string]any)["functionResponse"].(map[string]any)["response"].(map[string]any)["result"].(map[string]any)["email"])
	assert.Equal(t, "alice@tenant.test", parts[3].(map[string]any)["text"])
	assert.Equal(t, "alice@tenant.test", parts[4].(map[string]any)["inlineData"].(map[string]any)["data"])
	restored, err := DeanonymizeTextFields(encoded, session)
	require.NoError(t, err)
	var plain map[string]any
	require.NoError(t, common.UnmarshalWithNumber(restored, &plain))
	function := plain["contents"].([]any)[0].(map[string]any)["parts"].([]any)[1].(map[string]any)["functionCall"].(map[string]any)
	assert.Equal(t, "alice@tenant.test", function["args"].(map[string]any)["email"])
}

func TestLegacyCompletionAnonymizationPreservesIntegerTokenInputs(t *testing.T) {
	body, _, _ := requestFixture(t, `{"model":"test","prompt":["alice@tenant.test",[1,2,3]],"suffix":"alice@tenant.test"}`, AnonymizeCompletionRequest)
	assert.NotEqual(t, "alice@tenant.test", body["prompt"].([]any)[0])
	assert.Equal(t, []any{json.Number("1"), json.Number("2"), json.Number("3")}, body["prompt"].([]any)[1])
	assert.NotEqual(t, "alice@tenant.test", body["suffix"])
}

func TestJSONArgumentsRestoreEscapesAndRetainOriginalType(t *testing.T) {
	const original = "private\"quote\\path\n姓名"
	vault := NewVault()
	vault.byEngage["test"] = []Mapping{{Original: original, Surrogate: "[CUSTOM_FIXED]"}}
	session := NewSession("test", vault)
	encoded, err := DeanonymizeTextFields([]byte(`{
	 "choices":[{"message":{"function_call":{"name":"lookup","arguments":"{\"secret\":\"[CUSTOM_FIXED]\"}"},"tool_calls":[{"function":{"arguments":{"secret":"[CUSTOM_FIXED]"}}}]}}],
	 "metadata":{"text":"[CUSTOM_FIXED]"},
	 "content":[{"type":"thinking","thinking":"[CUSTOM_FIXED]","signature":"sig"},{"type":"compaction","encrypted_content":"[CUSTOM_FIXED]"}]
	}`), session)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.UnmarshalWithNumber(encoded, &body))
	message := body["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	legacy := message["function_call"].(map[string]any)["arguments"].(string)
	var arguments map[string]any
	require.NoError(t, common.UnmarshalWithNumber([]byte(legacy), &arguments))
	assert.Equal(t, original, arguments["secret"])
	structured := message["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["arguments"].(map[string]any)
	assert.Equal(t, original, structured["secret"])
	assert.Equal(t, "[CUSTOM_FIXED]", body["metadata"].(map[string]any)["text"])
	assert.Equal(t, "[CUSTOM_FIXED]", body["content"].([]any)[0].(map[string]any)["thinking"])
	assert.Equal(t, "[CUSTOM_FIXED]", body["content"].([]any)[1].(map[string]any)["encrypted_content"])
}

type failedRequestDetector struct{}

func (failedRequestDetector) Detect(context.Context, string) ([]Match, error) {
	return nil, errors.New("analyzer unavailable")
}

func TestRequestHelpersRejectDetectionFailuresAndMalformedJSON(t *testing.T) {
	for _, transform := range []func([]byte, *Session) ([]byte, error){AnonymizeCompletionRequest, AnonymizeChatRequest, AnonymizeResponsesRequest, AnonymizeClaudeRequest, AnonymizeGeminiRequest, AnonymizeTextFields} {
		invalid := []byte(`{"input":"secret"} trailing`)
		_, err := transform(invalid, NewSession("test", NewVault()))
		require.Error(t, err)
		unchanged, err := transform(invalid, nil)
		require.NoError(t, err)
		assert.Equal(t, invalid, unchanged)
	}
	session := NewSession("test", NewVault()).WithDetector(context.Background(), failedRequestDetector{})
	_, err := AnonymizeChatRequest([]byte(`{"messages":[{"role":"user","content":"sensitive example"}]}`), session)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "sensitive example")
}

func TestGenericRequestDoesNotTransformSchemaPropertiesOrMedia(t *testing.T) {
	_, _, encoded := requestFixture(t, `{"text":"alice@tenant.test","metadata":{"text":"alice@tenant.test"},"schema":{"properties":{"text":{"default":"alice@tenant.test"}}},"content":[{"type":"input_image","image_url":"https://tenant.test/alice@tenant.test"}]}`, AnonymizeTextFields)
	assert.Equal(t, 3, strings.Count(string(encoded), "alice@tenant.test"))
}
