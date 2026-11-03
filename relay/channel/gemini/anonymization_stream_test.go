package gemini

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"new-router/common"
	"new-router/pkg/anonymizer"
)

func geminiPrivacyFixture(t *testing.T) (*gin.Context, []anonymizer.Mapping, *anonymizer.StreamRestorer) {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:streamGenerateContent", nil)
	vault := anonymizer.NewVault()
	mappings := []anonymizer.Mapping{
		vault.GetOrCreate("privacy", "alice@example.test", "EMAIL"),
		vault.GetOrCreate("privacy", "bob@example.test", "EMAIL"),
	}
	session := anonymizer.NewSession("privacy", vault)
	anonymizer.Attach(c, session)
	return c, mappings, session.NewStreamRestorer()
}

func TestGeminiNativeTextPartChannelsDoNotCrossRestore(t *testing.T) {
	c, mappings, restorer := geminiPrivacyFixture(t)
	chunks := make([]string, 2)
	for pass := 0; pass < 2; pass++ {
		parts := make([]map[string]any, 2)
		for i, mapping := range mappings {
			middle := len(mapping.Surrogate) / 2
			text := mapping.Surrogate[:middle]
			if pass == 1 {
				text = mapping.Surrogate[middle:]
			}
			parts[i] = map[string]any{"text": text}
		}
		payload, err := common.Marshal(map[string]any{"candidates": []any{map[string]any{"index": 3, "content": map[string]any{"parts": parts}}}})
		require.NoError(t, err)
		chunks[pass], err = deanonymizeGeminiNativeChunk(c, string(payload), restorer)
		require.NoError(t, err)
	}
	for i, mapping := range mappings {
		path := "candidates.0.content.parts." + strconv.Itoa(i) + ".text"
		got := gjson.Get(chunks[0], path).String() + gjson.Get(chunks[1], path).String()
		require.Equal(t, mapping.Original, got)
	}
	require.Empty(t, restorer.PendingKeys())
}

func TestGeminiPartialToolStringsRestoreWithPersistentCallIdentity(t *testing.T) {
	c, mappings, restorer := geminiPrivacyFixture(t)
	_, err := deanonymizeGeminiNativeChunk(c, `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"a","name":"lookup"}},{"functionCall":{"id":"b","name":"lookup"}}]}}]}`, restorer)
	require.NoError(t, err)
	var output [2]strings.Builder
	for pass := 0; pass < 2; pass++ {
		parts := make([]map[string]any, 2)
		for i, mapping := range mappings {
			middle := len(mapping.Surrogate) / 2
			fragment := mapping.Surrogate[:middle]
			if pass == 1 {
				fragment = mapping.Surrogate[middle:]
			}
			parts[i] = map[string]any{"functionCall": map[string]any{"willContinue": pass == 0, "partialArgs": []any{map[string]any{"jsonPath": "$.email", "stringValue": fragment, "willContinue": pass == 0}}}}
		}
		payload, err := common.Marshal(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": parts}}}})
		require.NoError(t, err)
		restored, err := deanonymizeGeminiNativeChunk(c, string(payload), restorer)
		require.NoError(t, err)
		for i := range mappings {
			output[i].WriteString(gjson.Get(restored, "candidates.0.content.parts."+strconv.Itoa(i)+".functionCall.partialArgs.0.stringValue").String())
		}
	}
	for i, mapping := range mappings {
		require.Equal(t, mapping.Original, output[i].String())
	}
	require.Empty(t, restorer.PendingKeys())
}

func TestGeminiNativeToolValuesRestoreAndSignedPartsAreOpaque(t *testing.T) {
	c, mappings, restorer := geminiPrivacyFixture(t)
	payload, err := common.Marshal(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{
		map[string]any{"functionCall": map[string]any{"name": "lookup", "args": map[string]any{"email": mappings[0].Surrogate, "count": 9007199254740993}}},
		map[string]any{"functionResponse": map[string]any{"name": "lookup", "response": map[string]any{"email": mappings[1].Surrogate}}},
		map[string]any{"text": mappings[0].Surrogate, "thoughtSignature": "signature", "functionCall": map[string]any{"name": "signed", "args": map[string]any{"email": mappings[1].Surrogate}}},
	}}}}})
	require.NoError(t, err)
	out, err := deanonymizeGeminiNativeChunk(c, string(payload), restorer)
	require.NoError(t, err)
	require.Equal(t, mappings[0].Original, gjson.Get(out, "candidates.0.content.parts.0.functionCall.args.email").String())
	require.Equal(t, "9007199254740993", gjson.Get(out, "candidates.0.content.parts.0.functionCall.args.count").Raw)
	require.Equal(t, mappings[1].Original, gjson.Get(out, "candidates.0.content.parts.1.functionResponse.response.email").String())
	require.Equal(t, mappings[0].Surrogate, gjson.Get(out, "candidates.0.content.parts.2.text").String())
	require.Equal(t, mappings[1].Surrogate, gjson.Get(out, "candidates.0.content.parts.2.functionCall.args.email").String())
	require.Equal(t, "signature", gjson.Get(out, "candidates.0.content.parts.2.thoughtSignature").String())
}

func TestGeminiFinishFrameFlushesCarryBeforeTerminalAndEOFResidualKeepsPart(t *testing.T) {
	c, mappings, restorer := geminiPrivacyFixture(t)
	prefix := mappings[0].Surrogate[:len(mappings[0].Surrogate)/2]
	encoded, err := common.Marshal(prefix)
	require.NoError(t, err)
	out, err := deanonymizeGeminiNativeChunk(c, `{"candidates":[{"index":2,"content":{"parts":[{}, {"text":`+string(encoded)+`}]}}]}`, restorer)
	require.NoError(t, err)
	require.Empty(t, gjson.Get(out, "candidates.0.content.parts.1.text").String())
	out, err = deanonymizeGeminiNativeChunk(c, `{"candidates":[{"index":2,"finishReason":"STOP"}]}`, restorer)
	require.NoError(t, err)
	require.Equal(t, prefix, gjson.Get(out, "candidates.0.content.parts.1.text").String())
	require.Empty(t, restorer.PendingKeys())
	residual, ok := buildGeminiNativeResidual("2:1:reasoning", "kept tail")
	require.True(t, ok)
	require.Equal(t, int64(2), gjson.Get(residual, "candidates.0.index").Int())
	require.Equal(t, "kept tail", gjson.Get(residual, "candidates.0.content.parts.1.text").String())
	require.True(t, gjson.Get(residual, "candidates.0.content.parts.1.thought").Bool())
	require.False(t, strings.Contains(residual, mappings[0].Original))
}
