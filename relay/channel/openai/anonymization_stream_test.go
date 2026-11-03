package openai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"new-router/common"
	"new-router/constant"
	"new-router/pkg/anonymizer"
	relaycommon "new-router/relay/common"
	relayconstant "new-router/relay/constant"
	"new-router/relaykit/dto"
	"new-router/relaykit/types"
	"new-router/service"
)

func chatAnonymizationFixture(t *testing.T, original string) (*gin.Context, *httptest.ResponseRecorder, *anonymizer.Session, anonymizer.Mapping) {
	t.Helper()
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	vault := anonymizer.NewVault()
	mapping := vault.GetOrCreate("chat-test", original, "EMAIL")
	session := anonymizer.NewSession("chat-test", vault)
	anonymizer.Attach(c, session)
	return c, writer, session, mapping
}

func chatAnonymizationJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}

func TestChatAnonymizationParallelToolsAndLegacyJSON(t *testing.T) {
	c, _, session, mapping := chatAnonymizationFixture(t, "private \"line\"\npath\\folder")
	args := chatAnonymizationJSON(t, map[string]string{"value": mapping.Surrogate})
	split := strings.Index(args, mapping.Surrogate) + len(mapping.Surrogate)/2
	for _, typed := range []bool{false, true} {
		t.Run(fmt.Sprintf("typed=%t", typed), func(t *testing.T) {
			restorer := session.NewStreamRestorer()
			arguments := map[string]string{}
			for _, fragment := range []string{args[:split], args[split:]} {
				for _, indices := range [][]int{{0, 1}, {1, 0}} {
					choice, tool := indices[0], indices[1]
					payload := chatAnonymizationJSON(t, map[string]any{
						"id": "chat-test", "object": "chat.completion.chunk", "created": 1, "model": "client",
						"choices": []any{map[string]any{"index": choice, "delta": map[string]any{
							"tool_calls":    []any{map[string]any{"index": tool, "function": map[string]any{"arguments": fragment}}},
							"function_call": map[string]any{"arguments": fragment},
						}}},
						"usage": map[string]any{"completion_tokens": 17},
					})
					var restored string
					if typed {
						var chunk dto.ChatCompletionsStreamResponse
						require.NoError(t, common.UnmarshalJsonStr(payload, &chunk))
						deanonymizeChatStreamChunk(c, &chunk, restorer)
						restored = chatAnonymizationJSON(t, chunk)
					} else {
						var err error
						restored, err = deanonymizeStreamChunk(c, payload, restorer)
						require.NoError(t, err)
					}
					require.EqualValues(t, 17, gjson.Get(restored, "usage.completion_tokens").Int())
					key := fmt.Sprintf("%d:%d", choice, tool)
					arguments[key] += gjson.Get(restored, "choices.0.delta.tool_calls.0.function.arguments").String()
					arguments[fmt.Sprintf("%d:legacy", choice)] += gjson.Get(restored, "choices.0.delta.function_call.arguments").String()
				}
			}
			for key, argument := range arguments {
				var body map[string]string
				require.NoError(t, json.Unmarshal([]byte(argument), &body), key)
				require.Equal(t, mapping.Original, body["value"], key)
			}
			require.Empty(t, restorer.PendingJSONKeys())
		})
	}
}

func TestLegacyCompletionsAndChoiceFinishRestoreTails(t *testing.T) {
	c, _, session, mapping := chatAnonymizationFixture(t, "alice@private.example")
	for _, field := range []string{"text", "delta.content", "delta.refusal", "delta.reasoning", "delta.reasoning_content"} {
		t.Run(field, func(t *testing.T) {
			restorer := session.NewStreamRestorer()
			part := mapping.Surrogate[:len(mapping.Surrogate)/2]
			choice := map[string]any{"index": 2}
			if field == "text" {
				choice["text"] = part
			} else {
				choice["delta"] = map[string]any{strings.TrimPrefix(field, "delta."): part}
			}
			first, err := deanonymizeStreamChunk(c, chatAnonymizationJSON(t, map[string]any{"choices": []any{choice}, "object": "text_completion"}), restorer)
			require.NoError(t, err)
			require.Empty(t, gjson.Get(first, "choices.0."+field).String())
			finish := chatAnonymizationJSON(t, map[string]any{"choices": []any{map[string]any{"index": 2, "finish_reason": "stop"}}, "usage": map[string]any{"completion_tokens": 19}})
			last, err := deanonymizeStreamChunk(c, finish, restorer)
			require.NoError(t, err)
			require.Equal(t, part, gjson.Get(last, "choices.0."+field).String())
			require.Equal(t, "stop", gjson.Get(last, "choices.0.finish_reason").String())
			require.Empty(t, restorer.PendingKeys())
		})
	}
}

func TestOpenAIStreamingEstimatesUpstreamTextBeforeRestoration(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	c, writer, _, mapping := chatAnonymizationFixture(t, strings.Repeat("private company information ", 120))
	info := &relaycommon.RelayInfo{
		IsStream: true, DisablePing: true, StartTime: time.Now(),
		RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeChatCompletions,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o"},
	}
	info.SetEstimatePromptTokens(12)
	first := chatAnonymizationJSON(t, map[string]any{"id": "chat", "object": "chat.completion.chunk", "created": 1, "model": "gpt-4o", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": mapping.Surrogate}}}})
	last := chatAnonymizationJSON(t, map[string]any{"id": "chat", "object": "chat.completion.chunk", "created": 1, "model": "gpt-4o", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}})
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + first + "\n\ndata: " + last + "\n\ndata: [DONE]\n\n"))}
	usage, apiErr := OaiStreamHandler(c, info, response)
	require.Nil(t, apiErr)
	want := service.ResponseText2Usage(c, mapping.Surrogate, "gpt-4o", 12)
	require.Equal(t, want.CompletionTokens, usage.CompletionTokens)
	require.Contains(t, writer.Body.String(), mapping.Original)
	require.NotContains(t, writer.Body.String(), mapping.Surrogate)
}

func TestLegacyFunctionCallToResponsesRestoresStreamedArguments(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	c, writer, _, mapping := chatAnonymizationFixture(t, "private \"line\"\npath\\folder")
	info := &relaycommon.RelayInfo{
		IsStream: true, DisablePing: true, StartTime: time.Now(),
		RelayFormat: types.RelayFormatOpenAIResponses, RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o"},
	}
	info.SetEstimatePromptTokens(12)
	arguments := chatAnonymizationJSON(t, map[string]string{"value": mapping.Surrogate})
	split := strings.Index(arguments, mapping.Surrogate) + len(mapping.Surrogate)/2
	var frames strings.Builder
	for index, fragment := range []string{arguments[:split], arguments[split:]} {
		function := map[string]any{"arguments": fragment}
		if index == 0 {
			function["name"] = "inspect"
		}
		payload := chatAnonymizationJSON(t, map[string]any{
			"id": "chat", "object": "chat.completion.chunk", "created": 1, "model": "gpt-4o",
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"function_call": function}}},
		})
		frames.WriteString("data: " + payload + "\n\n")
	}
	last := chatAnonymizationJSON(t, map[string]any{
		"id": "chat", "object": "chat.completion.chunk", "created": 1, "model": "gpt-4o",
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "function_call"}},
		"usage":   map[string]any{"prompt_tokens": 12, "completion_tokens": 8, "total_tokens": 20},
	})
	frames.WriteString("data: " + last + "\n\ndata: [DONE]\n\n")
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(frames.String()))}
	usage, apiErr := OaiChatToResponsesStreamHandler(c, info, response)
	require.Nil(t, apiErr)
	require.Equal(t, 8, usage.CompletionTokens)
	var restored strings.Builder
	var doneArguments string
	for _, line := range strings.Split(writer.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		event := strings.TrimPrefix(line, "data: ")
		switch gjson.Get(event, "type").String() {
		case "response.function_call_arguments.delta":
			restored.WriteString(gjson.Get(event, "delta").String())
		case "response.function_call_arguments.done":
			doneArguments = gjson.Get(event, "arguments").String()
		}
	}
	for _, argument := range []string{restored.String(), doneArguments} {
		var body map[string]string
		require.NoError(t, json.Unmarshal([]byte(argument), &body))
		require.Equal(t, mapping.Original, body["value"])
	}
}
