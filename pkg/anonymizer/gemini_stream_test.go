package anonymizer

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"new-router/common"
)

func TestGeminiStreamFlushGroupsArgumentsIntoOneFunction(t *testing.T) {
	vault := NewVault()
	first := vault.GetOrCreate("gemini", "alice@example.test", "EMAIL")
	second := vault.GetOrCreate("gemini", "bob@example.test", "EMAIL")
	restore := NewSession("gemini", vault).NewGeminiStreamRestorer()
	_, err := restore.Restore([]byte(`{"candidates":[{"index":2,"content":{"parts":[{"functionCall":{"id":"call-2","name":"lookup"}}]}}]}`))
	require.NoError(t, err)
	firstPrefix := first.Surrogate[:len(first.Surrogate)/2]
	secondPrefix := second.Surrogate[:len(second.Surrogate)/2]
	argumentsInput := []any{
		map[string]any{"jsonPath": "$.first", "stringValue": firstPrefix, "willContinue": true},
		map[string]any{"jsonPath": "$.second", "stringValue": secondPrefix, "willContinue": true},
	}
	part := map[string]any{"functionCall": map[string]any{"willContinue": true, "partialArgs": argumentsInput}}
	candidate := map[string]any{"index": 2, "content": map[string]any{"parts": []any{part}}}
	frame, err := common.Marshal(map[string]any{"candidates": []any{candidate}})
	require.NoError(t, err)
	_, err = restore.Restore(frame)
	require.NoError(t, err)
	output, err := restore.Flush()
	require.NoError(t, err)
	require.Len(t, output, 1, "all argument tails must complete one function together")
	call := gjson.GetBytes(output[0], "candidates.0.content.parts.0.functionCall")
	require.Equal(t, "call-2", call.Get("id").String())
	require.Equal(t, "lookup", call.Get("name").String())
	require.False(t, call.Get("willContinue").Bool())
	arguments := make(map[string]string)
	for _, argument := range call.Get("partialArgs").Array() {
		arguments[argument.Get("jsonPath").String()] = argument.Get("stringValue").String()
	}
	require.Equal(t, map[string]string{"$.first": firstPrefix, "$.second": secondPrefix}, arguments)
	require.Empty(t, restore.stream.PendingKeys())
}

func TestGeminiStreamHelpersValidateJSONAndRemainNilSafe(t *testing.T) {
	var session *Session
	restore := session.NewGeminiStreamRestorer()
	input := []byte(`{"candidates":[]}`)
	output, err := restore.Restore(input)
	require.NoError(t, err)
	require.Equal(t, [][]byte{input}, output)
	_, err = NewSession("json", NewVault()).NewGeminiStreamRestorer().Restore([]byte(`{"candidates":`))
	require.Error(t, err)
}
