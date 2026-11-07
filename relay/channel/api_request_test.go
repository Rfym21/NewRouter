package channel

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"new-router/relay/channel/requestrepair"
	relaycommon "new-router/relay/common"
	"new-router/relaykit/dto"
)

type repairRoundTripper func(*http.Request) (*http.Response, error)

// RoundTrip allows repair tests to observe transport deadlines without a real upstream.
func (transport repairRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return transport(req)
}

func TestRequestRepairSharesTimeoutAndPreservesDeadline(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	requestContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestContext, http.MethodPost, "https://local.invalid/v1/messages", strings.NewReader(`{"messages":[{"role":"user","content":[{"type":"image","source":{"data":"abc"}}]}]}`))
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{ImageFallbackEnabled: true}}}
	var deadlines []time.Time
	client := &http.Client{
		Timeout: 200 * time.Millisecond,
		Transport: repairRoundTripper(func(req *http.Request) (*http.Response, error) {
			deadline, ok := req.Context().Deadline()
			require.True(t, ok)
			deadlines = append(deadlines, deadline)
			status, responseBody := http.StatusOK, `{"ok":true}`
			if len(deadlines) == 1 {
				time.Sleep(30 * time.Millisecond)
				status, responseBody = http.StatusBadRequest, `{"error":{"message":"images are not supported"}}`
			}
			require.NoError(t, req.Body.Close())
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(responseBody)), Request: req}, nil
		}),
	}
	resp, err := doRequestWithRepairs(c, client, req, info, nil)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Len(t, deadlines, 2)
	require.InDelta(t, 0, float64(deadlines[1].Sub(deadlines[0])), float64(10*time.Millisecond))
	require.Equal(t, 200*time.Millisecond, client.Timeout)
	replayed := requestWithBody(req, []byte(`{}`))
	originalDeadline, ok := req.Context().Deadline()
	require.True(t, ok)
	replayedDeadline, ok := replayed.Context().Deadline()
	require.True(t, ok)
	require.Equal(t, originalDeadline, replayedDeadline)
}

func TestRequestRepairAcrossMessageProtocols(t *testing.T) {
	const chat = `{"model":"text-model","messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}}]}]}`
	const chatRepaired = `{"model":"text-model","messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"text","text":"[Unsupported Image]"}]}]}`
	const claude = `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"image","source":{"type":"base64","data":"abc"},"cache_control":{"type":"ephemeral"}}]}]}]}`
	const claudeRepaired = `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"text","text":"[Unsupported Image]","cache_control":{"type":"ephemeral"}}]}]}]}`
	const responses = `{"input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/image.png"}]},{"type":"function_call_output","call_id":"call_1","output":[{"type":"input_image","image_url":"data:image/png;base64,abc"}]}],"stream":true}`
	const responsesRepaired = `{"input":[{"role":"user","content":[{"type":"input_text","text":"[Unsupported Image]"}]},{"type":"function_call_output","call_id":"call_1","output":[{"type":"input_text","text":"[Unsupported Image]"}]}],"stream":true}`
	const thinking = `{"thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"plan","signature":"old"},{"type":"tool_use","id":"call_1","name":"run","input":{"number":9007199254740993}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"done"}]}]}`
	const thinkingRepaired = `{"messages":[{"role":"assistant","content":[{"type":"text","text":"plan"},{"type":"tool_use","id":"call_1","name":"run","input":{"number":9007199254740993}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"done"}]}]}`
	const encrypted = `{"input":[{"type":"reasoning","encrypted_content":"old"},{"type":"message","role":"user","content":"hello"}]}`
	const encryptedRepaired = `{"input":[{"type":"message","role":"user","content":"hello"}]}`
	const imageError = `{"error":{"message":"This model does not support image input"}}`
	tests := []struct {
		name       string
		body       string
		wantBody   string
		image      bool
		thinking   bool
		encrypted  bool
		status     int
		errorBody  string
		lastStatus int
		wantCalls  int
	}{
		{name: "chat completions", body: chat, wantBody: chatRepaired, image: true, status: 400, errorBody: imageError, wantCalls: 2},
		{name: "anthropic nested tool image", body: claude, wantBody: claudeRepaired, image: true, status: 415, errorBody: imageError, wantCalls: 2},
		{name: "responses and function output", body: responses, wantBody: responsesRepaired, image: true, status: 422, errorBody: imageError, wantCalls: 2},
		{name: "disabled", body: chat, wantBody: chat, status: 400, errorBody: imageError, wantCalls: 1},
		{name: "invalid base64", body: chat, wantBody: chat, image: true, status: 400, errorBody: `{"error":{"message":"invalid base64 image data"}}`, wantCalls: 1},
		{name: "unsupported image format", body: chat, wantBody: chat, image: true, status: 400, errorBody: `{"error":{"message":"unsupported image format"}}`, wantCalls: 1},
		{name: "rate limit", body: chat, wantBody: chat, image: true, status: 429, errorBody: imageError, wantCalls: 1},
		{name: "unrelated 404", body: chat, wantBody: chat, image: true, status: 404, errorBody: imageError, wantCalls: 1},
		{name: "router image capability 404", body: chat, wantBody: chatRepaired, image: true, status: 404, errorBody: `{"error":{"message":"No endpoints found that support image input"}}`, wantCalls: 2},
		{name: "latest failure", body: chat, wantBody: chatRepaired, image: true, status: 400, errorBody: imageError, lastStatus: 503, wantCalls: 2},
		{name: "single image retry", body: chat, wantBody: chatRepaired, image: true, status: 400, errorBody: imageError, lastStatus: 400, wantCalls: 2},
		{name: "thinking with tools", body: thinking, wantBody: thinkingRepaired, thinking: true, status: 400, errorBody: `{"error":{"message":"Invalid signature in thinking block"}}`, wantCalls: 2},
		{name: "encrypted responses", body: encrypted, wantBody: encryptedRepaired, encrypted: true, status: 400, errorBody: `{"error":{"code":"invalid_encrypted_content"}}`, wantCalls: 2},
		{name: "unrelated authentication signature", body: thinking, wantBody: thinking, thinking: true, status: 400, errorBody: `{"error":{"message":"invalid API authentication signature"}}`, wantCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			type capturedRequest struct {
				body          []byte
				length        int64
				authorization string
				err           error
			}
			received := make(chan capturedRequest, 4)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				received <- capturedRequest{body, r.ContentLength, r.Header.Get("Authorization"), err}
				call := calls.Add(1)
				w.Header().Set("X-Attempt", fmt.Sprint(call))
				if call == 1 {
					w.WriteHeader(test.status)
					fmt.Fprint(w, test.errorBody)
					return
				}
				if test.lastStatus != 0 {
					w.WriteHeader(test.lastStatus)
					fmt.Fprint(w, test.errorBody)
					return
				}
				fmt.Fprint(w, `{"ok":true}`)
			}))
			defer server.Close()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			req, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(test.body))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer original")
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
				ChannelSetting: dto.ChannelSettings{ImageFallbackEnabled: test.image},
				ChannelOtherSettings: dto.ChannelOtherSettings{
					ThinkingSignatureRectifierEnabled:         test.thinking,
					ResponsesEncryptedContentRectifierEnabled: test.encrypted,
				},
			}}
			resp, err := doRequestWithRepairs(c, server.Client(), req, info, nil)
			require.NoError(t, err)
			defer resp.Body.Close()
			responseBody, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.EqualValues(t, test.wantCalls, calls.Load())
			require.Equal(t, fmt.Sprint(test.wantCalls), resp.Header.Get("X-Attempt"))
			wantStatus := test.status
			if test.wantCalls > 1 {
				wantStatus = http.StatusOK
				if test.lastStatus != 0 {
					wantStatus = test.lastStatus
				}
			}
			require.Equal(t, wantStatus, resp.StatusCode)
			if wantStatus >= 400 {
				require.Equal(t, test.errorBody, string(responseBody))
			}
			for call := 0; call < test.wantCalls; call++ {
				captured := <-received
				require.NoError(t, captured.err)
				require.Equal(t, int64(len(captured.body)), captured.length)
				require.Equal(t, "Bearer original", captured.authorization)
				if call == 0 {
					require.Equal(t, test.body, string(captured.body))
				} else {
					require.JSONEq(t, test.wantBody, string(captured.body))
					if test.thinking {
						require.Contains(t, string(captured.body), "9007199254740993")
					}
				}
			}
		})
	}
}

func TestImageRepairPreservesSerializedToolOutput(t *testing.T) {
	body := []byte(`{"input":[{"type":"function_call_output","call_id":"call_1","output":"{\"result\":{\"content\":[{\"type\":\"image\",\"data\":\"abc\",\"mimeType\":\"image/png\"}]}}"}]}`)
	got, changed, err := requestrepair.RepairImages(body, 400, []byte(`image input not supported`))
	require.NoError(t, err)
	require.True(t, changed)
	require.Contains(t, string(got), `[Unsupported Image]`)
	require.NotContains(t, string(got), "abc")
	require.Contains(t, string(got), `\"input_text\"`)
	_, changed, err = requestrepair.RepairImages(got, 400, []byte(`image input not supported`))
	require.NoError(t, err)
	require.False(t, changed)
	unchanged := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"call_1","function":{"arguments":"{\"type\":\"image\",\"data\":\"keep\"}"}}]}]}`)
	got, changed, err = requestrepair.RepairImages(unchanged, 400, []byte(`image input not supported`))
	require.NoError(t, err)
	require.False(t, changed)
	require.True(t, bytes.Equal(unchanged, got))
}

func TestNewTaskAPIRequestInheritsClientCancellation(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	requestContext, cancel := context.WithCancel(context.Background())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestContext)

	upstream, err := newTaskAPIRequest(c, "https://provider.example/tasks", nil)
	require.NoError(t, err)
	cancel()

	require.ErrorIs(t, upstream.Context().Err(), context.Canceled)
}

func TestProcessHeaderOverride_ChannelTestSkipsPassthroughRules(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx, nil)
	require.NoError(t, err)
	require.Empty(t, headers)
}

func TestProcessHeaderOverride_ChannelTestSkipsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx, nil)
	require.NoError(t, err)
	_, ok := headers["x-upstream-trace"]
	require.False(t, ok)
}

func TestProcessHeaderOverride_NonTestKeepsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx, nil)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-upstream-trace"])
}

func TestProcessHeaderOverride_RuntimeOverrideIsFinalHeaderMap(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		IsChannelTest:             false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"x-static":  "runtime-value",
			"x-runtime": "runtime-only",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
				"X-Legacy": "legacy-only",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx, nil)
	require.NoError(t, err)
	require.Equal(t, "runtime-value", headers["x-static"])
	require.Equal(t, "runtime-only", headers["x-runtime"])
	_, exists := headers["x-legacy"]
	require.False(t, exists)
}

func TestProcessHeaderOverride_PassthroughSkipsAcceptEncoding(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")
	ctx.Request.Header.Set("Accept-Encoding", "gzip")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx, nil)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-trace-id"])

	_, hasAcceptEncoding := headers["accept-encoding"]
	require.False(t, hasAcceptEncoding)
}

func TestProcessHeaderOverride_PassHeadersTemplateSetsRuntimeHeaders(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set("Originator", "Codex CLI")
	ctx.Request.Header.Set("Session_id", "sess-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		RequestHeaders: map[string]string{
			"Originator": "Codex CLI",
			"Session_id": "sess-123",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]any{
				"operations": []any{
					map[string]any{
						"mode":  "pass_headers",
						"value": []any{"Originator", "Session_id", "X-Codex-Beta-Features"},
					},
				},
			},
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
			},
		},
	}

	_, err := relaycommon.ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)
	require.NoError(t, err)
	require.True(t, info.UseRuntimeHeadersOverride)
	require.Equal(t, "Codex CLI", info.RuntimeHeadersOverride["originator"])
	require.Equal(t, "sess-123", info.RuntimeHeadersOverride["session_id"])
	_, exists := info.RuntimeHeadersOverride["x-codex-beta-features"]
	require.False(t, exists)
	require.Equal(t, "legacy-value", info.RuntimeHeadersOverride["x-static"])

	headers, err := processHeaderOverride(info, ctx, nil)
	require.NoError(t, err)
	require.Equal(t, "Codex CLI", headers["originator"])
	require.Equal(t, "sess-123", headers["session_id"])
	_, exists = headers["x-codex-beta-features"]
	require.False(t, exists)

	upstreamReq := httptest.NewRequest(http.MethodPost, "https://example.com/v1/responses", nil)
	applyHeaderOverrideToRequest(upstreamReq, headers)
	require.Equal(t, "Codex CLI", upstreamReq.Header.Get("Originator"))
	require.Equal(t, "sess-123", upstreamReq.Header.Get("Session_id"))
	require.Empty(t, upstreamReq.Header.Get("X-Codex-Beta-Features"))
}

func TestToWebSocketURL(t *testing.T) {
	for input, want := range map[string]string{
		"https://api.openai.com/v1/responses":             "wss://api.openai.com/v1/responses",
		"http://127.0.0.1:3000/v1/responses":              "ws://127.0.0.1:3000/v1/responses",
		"wss://chatgpt.com/backend-api/codex/responses":   "wss://chatgpt.com/backend-api/codex/responses",
		"ws://127.0.0.1:3000/backend-api/codex/responses": "ws://127.0.0.1:3000/backend-api/codex/responses",
	} {
		assert.Equal(t, want, toWebSocketURL(input), input)
	}
}
