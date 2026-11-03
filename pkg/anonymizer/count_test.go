package anonymizer

import (
	"context"
	"errors"
	"testing"

	"new-router/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplacementCountOccurrencesConversionsAndRetries(t *testing.T) {
	session := NewSession("counts", nil)
	raw := []byte(`{"messages":[{"role":"user","content":"alice@private.example alice@private.example"},{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"email\":\"alice@private.example\"}"}}]}]}`)
	masked, err := AnonymizeChatRequest(raw, session)
	require.NoError(t, err)
	assert.EqualValues(t, 3, session.ReplacementCount(), "same value at three positions counts three, preflight contributes zero")
	require.Len(t, session.Snapshot(), 1)

	for _, body := range [][]byte{masked, raw, masked} {
		_, err = AnonymizeChatRequest(body, session)
		require.NoError(t, err)
		assert.EqualValues(t, 3, session.ReplacementCount(), "retries must not accumulate")
	}
	var body map[string]any
	require.NoError(t, common.UnmarshalWithNumber(masked, &body))
	alias := session.Snapshot()[0].Surrogate
	converted, err := common.Marshal(map[string]any{"contents": []any{
		map[string]any{"parts": []any{map[string]any{"text": alias + " " + alias}, map[string]any{"functionCall": map[string]any{"name": "lookup", "args": map[string]any{"email": alias}}}}},
	}, "systemInstruction": map[string]any{"parts": []any{map[string]any{"text": "bob@private.example alice@private.example"}}}})
	require.NoError(t, err)
	converted, err = AnonymizeGeminiRequest(converted, session)
	require.NoError(t, err)
	assert.EqualValues(t, 5, session.ReplacementCount(), "late override counts even if it repeats an earlier entity")
	_, err = AnonymizeGeminiRequest(converted, session)
	require.NoError(t, err)
	assert.EqualValues(t, 5, session.ReplacementCount())
	_, err = DeanonymizeTextFields(converted, session)
	require.NoError(t, err)
	assert.EqualValues(t, 5, session.ReplacementCount(), "response restoration contributes zero")
}

func TestReplacementCountImportedHistoryAndStructuralFields(t *testing.T) {
	session := NewSession("history-counts", nil)
	require.NoError(t, session.Import([]Mapping{{EntityType: "EMAIL_ADDRESS", Original: "alice@private.example", Surrogate: "old-alias@masked.example"}}))
	assert.Zero(t, session.ReplacementCount())
	_, err := AnonymizeResponsesRequest([]byte(`{"input":[{"type":"compaction","encrypted_content":"opaque"}],"metadata":{"email":"alice@private.example"},"tools":[{"type":"function","name":"lookup","parameters":{"type":"object","description":"alice@private.example"}}]}`), session)
	require.NoError(t, err)
	assert.Zero(t, session.ReplacementCount(), "opaque history and structural fields are not processed")
	_, err = AnonymizeResponsesRequest([]byte(`{"input":"alice@private.example"}`), session)
	require.NoError(t, err)
	assert.EqualValues(t, 1, session.ReplacementCount(), "reusing a mapping to actually replace plaintext counts")
	var nilSession *Session
	assert.Zero(t, nilSession.ReplacementCount())
}

type countFailureDetector struct{}

func (countFailureDetector) Detect(_ context.Context, _ string) ([]Match, error) {
	return nil, errors.New("detector unavailable")
}

func TestReplacementCountFailedRequestDoesNotCommit(t *testing.T) {
	session := NewSession("failed-counts", nil).WithDetector(context.Background(), countFailureDetector{})
	_, err := AnonymizeChatRequest([]byte(`{"messages":[{"role":"user","content":"alice@private.example"}]}`), session)
	require.Error(t, err)
	assert.Zero(t, session.ReplacementCount())

	session = NewSession("failed-output", nil)
	session.beginRequestPreflight()
	session.AnonymizeString("alice@private.example")
	session.preflighting = false
	session.AnonymizeString("alice@private.example")
	session.endRequestTransform(false)
	assert.Zero(t, session.ReplacementCount(), "encoding failures must discard a complete transform's pending count")
}

func TestReplacementCountNulsAndNestedAliases(t *testing.T) {
	session := NewSession("nul-counts", nil)
	require.NoError(t, session.Import([]Mapping{
		{EntityType: "IP_ADDRESS", Original: "192.0.2.9", Surrogate: "198.51.100.8"},
		{EntityType: "URL", Original: "https://private.example/path", Surrogate: "https://198.51.100.8/path"},
	}))
	_, err := AnonymizeChatRequest([]byte(`{"messages":[{"role":"user","content":"192.0.2.9 https://private.example/path \u0000"}]}`), session)
	require.NoError(t, err)
	assert.EqualValues(t, 2, session.ReplacementCount(), "IP inside URL alias and original NUL are not extra matches")
}

func TestLogRedactorDoesNotExposeSensitiveValuesOrChangeCount(t *testing.T) {
	session := NewSession("redaction", nil)
	_, err := AnonymizeChatRequest([]byte(`{"messages":[{"role":"user","content":"alice@private.example"}]}`), session)
	require.NoError(t, err)
	alias := session.Snapshot()[0].Surrogate
	redact := session.LogRedactor()
	assert.Equal(t, "set prompt = [REDACTED] / [REDACTED]", redact("set prompt = alice@private.example / "+alias))
	assert.EqualValues(t, 1, session.ReplacementCount())
}
