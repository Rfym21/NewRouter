package claude

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"new-router/common"
	"new-router/constant"
	"new-router/pkg/anonymizer"
	relaycommon "new-router/relay/common"
	"new-router/relaykit/dto"
	"new-router/relaykit/types"
)

func claudePrivacyFixture(t *testing.T, format types.RelayFormat) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo, *ClaudeResponseInfo, []anonymizer.Mapping) {
	t.Helper()
	vault := anonymizer.NewVault()
	mappings := []anonymizer.Mapping{
		vault.GetOrCreate("privacy", "alice\"line\n@example.test", "EMAIL"),
		vault.GetOrCreate("privacy", "bob@example.test", "EMAIL"),
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	anonymizer.Attach(c, anonymizer.NewSession("privacy", vault))
	info := &relaycommon.RelayInfo{
		OriginModelName: "client-model", RelayFormat: format, IsStream: true,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"},
	}
	usage := &ClaudeResponseInfo{ResponseId: "msg-test", Model: "claude-test", Usage: &dto.Usage{}}
	return c, recorder, info, usage, mappings
}

func TestClaudeMalformedStreamFlushesValidatedCarryWithoutCompletion(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	c, recorder, info, _, mappings := claudePrivacyFixture(t, types.RelayFormatClaude)
	info.DisablePing = true
	prefix := mappings[0].Surrogate[:len(mappings[0].Surrogate)/2]
	encoded, err := common.Marshal(prefix)
	require.NoError(t, err)
	body := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg-error","model":"claude-test","usage":{"input_tokens":2,"output_tokens":0}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":` + string(encoded) + `}}`,
		`data: not-json`, "",
	}, "\n")
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	_, apiErr := ClaudeStreamHandler(c, response, info)
	require.NotNil(t, apiErr)
	var restored strings.Builder
	for _, line := range strings.Split(recorder.Body.String(), "\n") {
		if strings.HasPrefix(line, "data: ") {
			restored.WriteString(gjson.Get(strings.TrimPrefix(line, "data: "), "delta.text").String())
		}
	}
	require.Equal(t, prefix, restored.String(), "valid held output must survive a later malformed frame")
	require.NotContains(t, recorder.Body.String(), "[DONE]")
	require.NotContains(t, recorder.Body.String(), mappings[0].Original)
}

func TestClaudeNativeToolJSONStreamsAreIsolatedAndEscaped(t *testing.T) {
	c, recorder, info, usage, mappings := claudePrivacyFixture(t, types.RelayFormatClaude)
	args := make([]string, len(mappings))
	for i, m := range mappings {
		body, err := common.Marshal(map[string]string{"email": m.Surrogate})
		require.NoError(t, err)
		args[i] = string(body)
		start := fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"tool_use","id":"tool%d","name":"lookup","input":{}}}`, i, i)
		require.Nil(t, HandleStreamResponseData(c, info, usage, start))
	}
	// Alternate one byte of each tool's arguments to exercise real cross-event carry.
	for offset := 0; offset < max(len(args[0]), len(args[1])); offset++ {
		for i, arg := range args {
			if offset >= len(arg) {
				continue
			}
			fragment, err := common.Marshal(arg[offset : offset+1])
			require.NoError(t, err)
			event := fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%s}}`, i, fragment)
			require.Nil(t, HandleStreamResponseData(c, info, usage, event))
		}
	}
	for i := range mappings {
		require.Nil(t, HandleStreamResponseData(c, info, usage, fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, i)))
	}
	var restored [2]strings.Builder
	for _, line := range strings.Split(recorder.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if gjson.Get(payload, "delta.type").String() == "input_json_delta" {
			restored[gjson.Get(payload, "index").Int()].WriteString(gjson.Get(payload, "delta.partial_json").String())
		}
	}
	for i, m := range mappings {
		var decoded map[string]string
		require.NoError(t, common.Unmarshal([]byte(restored[i].String()), &decoded))
		require.Equal(t, m.Original, decoded["email"])
		require.NotContains(t, restored[i].String(), m.Surrogate)
	}
	// Local usage estimation sees the original upstream arguments, not restored PII.
	require.NotContains(t, usage.ResponseText.String(), mappings[0].Original)
}

func TestClaudeSignedThinkingRemainsUnchangedInNativeAndConvertedStreams(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI} {
		t.Run(string(format), func(t *testing.T) {
			c, recorder, info, usage, mappings := claudePrivacyFixture(t, format)
			require.Nil(t, HandleStreamResponseData(c, info, usage, `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`))
			payload, err := common.Marshal(mappings[0].Surrogate)
			require.NoError(t, err)
			require.Nil(t, HandleStreamResponseData(c, info, usage, fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":%s}}`, payload)))
			require.Nil(t, HandleStreamResponseData(c, info, usage, `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"signed-value"}}`))
			require.Nil(t, HandleStreamResponseData(c, info, usage, `{"type":"content_block_stop","index":0}`))
			require.NotContains(t, recorder.Body.String(), mappings[0].Original)
			require.Contains(t, recorder.Body.String(), mappings[0].Surrogate)
			if format == types.RelayFormatClaude {
				require.Contains(t, recorder.Body.String(), `"signature":"signed-value"`)
			}
		})
	}
	c, _, _, _, mappings := claudePrivacyFixture(t, types.RelayFormatGemini)
	restorer := anonymizer.FromContext(c).NewStreamRestorer()
	thinking := mappings[0].Surrogate
	response := &dto.ClaudeResponse{Type: "content_block_delta", Index: common.GetPointer(0), Delta: &dto.ClaudeMediaMessage{Type: "thinking_delta", Thinking: &thinking}}
	prepared := prepareConvertedClaudeChunk(c, response, restorer)
	require.Len(t, prepared, 1)
	require.Equal(t, thinking, *prepared[0].Delta.Thinking, "native signed thinking stays unchanged before Gemini/Responses conversion")
}

func TestClaudeConvertedToolFlushPrecedesBlockStopAndKeepsRawAccounting(t *testing.T) {
	c, _, _, _, mappings := claudePrivacyFixture(t, types.RelayFormatOpenAIResponses)
	restorer := anonymizer.FromContext(c).NewStreamRestorer()
	text := `{"email":"` + mappings[0].Surrogate[:len(mappings[0].Surrogate)/2]
	response := &dto.ClaudeResponse{Type: "content_block_delta", Index: common.GetPointer(7), Delta: &dto.ClaudeMediaMessage{Type: "input_json_delta", PartialJson: &text}}
	chunks := prepareConvertedClaudeChunk(c, response, restorer)
	require.Len(t, chunks, 1)
	require.Equal(t, text, *response.Delta.PartialJson, "client restoration must not mutate the upstream accounting event")
	stop := &dto.ClaudeResponse{Type: "content_block_stop", Index: common.GetPointer(7)}
	chunks = prepareConvertedClaudeChunk(c, stop, restorer)
	require.Len(t, chunks, 2)
	require.Equal(t, "input_json_delta", chunks[0].Delta.Type)
	require.Equal(t, "content_block_stop", chunks[1].Type)
	require.Empty(t, restorer.PendingJSONKeys())
}
