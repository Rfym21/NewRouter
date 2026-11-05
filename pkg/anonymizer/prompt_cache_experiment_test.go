package anonymizer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type promptCacheUsage struct {
	Input  int64 `json:"input_tokens"`
	Read   int64 `json:"cache_read_input_tokens"`
	Write  int64 `json:"cache_creation_input_tokens"`
	Output int64 `json:"output_tokens"`
}

type promptCacheMeasurement struct {
	Mode          string           `json:"mode"`
	Round         int              `json:"round"`
	LocalMS       float64          `json:"local_anonymization_ms"`
	TTFTMS        float64          `json:"upstream_first_text_ms"`
	TotalMS       float64          `json:"upstream_total_ms"`
	Usage         promptCacheUsage `json:"usage"`
	EstimatedCost *float64         `json:"estimated_cost,omitempty"`
}

type promptCachePrices struct {
	Input  *float64 `json:"input"`
	Read   *float64 `json:"read"`
	Write  *float64 `json:"write"`
	Output *float64 `json:"output"`
}

func (p promptCachePrices) cost(u promptCacheUsage) (float64, error) {
	for _, value := range []*float64{p.Input, p.Read, p.Write, p.Output} {
		if value == nil || *value < 0 || math.IsNaN(*value) || math.IsInf(*value, 0) {
			return 0, errors.New("all four nonnegative per-million token prices are required")
		}
	}
	return (float64(u.Input)*(*p.Input) + float64(u.Read)*(*p.Read) + float64(u.Write)*(*p.Write) + float64(u.Output)*(*p.Output)) / 1e6, nil
}

// Synthetic input only. The breakpoint precedes a growing messages history;
// identical system text is a necessary, not sufficient, condition for a hit.
func promptCacheExperimentRequest(model, campaign, mode string, round int) []byte {
	prefix := "Experiment " + campaign + " mode " + mode + ". Contact alice@example.test at 192.0.2.88.\n"
	prefix += performanceText("plain", 64<<10)
	messages := []any{map[string]any{"role": "user", "content": "Reply with OK only."}}
	if round > 0 {
		messages = append(messages, map[string]any{"role": "assistant", "content": "OK"}, map[string]any{"role": "user", "content": "Again reply with OK only."})
	}
	data, _ := json.Marshal(map[string]any{
		"model": model, "max_tokens": 8, "stream": true,
		"system":   []any{map[string]any{"type": "text", "text": prefix, "cache_control": map[string]any{"type": "ephemeral"}}},
		"messages": messages,
	})
	return data
}

func experimentSystem(data []byte) (string, error) {
	var body struct {
		System []struct {
			Text string `json:"text"`
		} `json:"system"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return "", err
	}
	if len(body.System) != 1 {
		return "", errors.New("unexpected experiment system")
	}
	return body.System[0].Text, nil
}

func runPromptCacheExperiment(ctx context.Context, client *http.Client, endpoint, key, model, campaign string) ([]promptCacheMeasurement, error) {
	var results []promptCacheMeasurement
	for _, stable := range []bool{false, true} {
		mode := "random"
		if stable {
			mode = "stable"
		}
		for round := 0; round < 2; round++ {
			raw := promptCacheExperimentRequest(model, campaign, mode, round)
			start := time.Now()
			masked, err := AnonymizeClaudeRequest(raw, NewSession("prompt-cache-experiment", nil).WithStableSurrogates(stable))
			if err != nil {
				return nil, err
			}
			result := promptCacheMeasurement{Mode: mode, Round: round + 1, LocalMS: float64(time.Since(start)) / float64(time.Millisecond)}
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(masked))
			if err != nil {
				return nil, errors.New("invalid experiment endpoint")
			}
			request.Header.Set("x-api-key", key)
			request.Header.Set("anthropic-version", "2023-06-01")
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Accept", "text/event-stream")
			start = time.Now()
			response, err := client.Do(request)
			if err != nil {
				return nil, errors.New("experiment HTTP request failed")
			}
			if response.StatusCode != http.StatusOK {
				response.Body.Close()
				return nil, fmt.Errorf("experiment HTTP status %d", response.StatusCode)
			}
			err = readPromptCacheStream(response.Body, start, &result)
			response.Body.Close()
			if err != nil {
				return nil, err
			}
			result.TotalMS = float64(time.Since(start)) / float64(time.Millisecond)
			results = append(results, result)
		}
	}
	return results, nil
}

func readPromptCacheStream(body io.Reader, start time.Time, result *promptCacheMeasurement) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	started, stopped, text := false, false, false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event struct {
			Type    string `json:"type"`
			Message struct {
				Usage promptCacheUsage `json:"usage"`
			} `json:"message"`
			Usage map[string]int64 `json:"usage"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
			return errors.New("invalid experiment SSE data")
		}
		switch event.Type {
		case "error":
			return errors.New("upstream experiment stream error")
		case "message_start":
			result.Usage = event.Message.Usage
			started = true
		case "message_delta":
			// Delta usage is cumulative, not additive; absent keys retain start usage.
			if n, ok := event.Usage["input_tokens"]; ok {
				result.Usage.Input = n
			}
			if n, ok := event.Usage["output_tokens"]; ok {
				result.Usage.Output = n
			}
			if n, ok := event.Usage["cache_read_input_tokens"]; ok {
				result.Usage.Read = n
			}
			if n, ok := event.Usage["cache_creation_input_tokens"]; ok {
				result.Usage.Write = n
			}
		case "content_block_delta":
			if !text && event.Delta.Type == "text_delta" && event.Delta.Text != "" {
				result.TTFTMS = float64(time.Since(start)) / float64(time.Millisecond)
				text = true
			}
		case "message_stop":
			stopped = true
		}
	}
	if scanner.Err() != nil {
		return errors.New("experiment stream read failed")
	}
	if !started || !stopped || !text {
		return errors.New("incomplete experiment stream or missing text")
	}
	return nil
}

func TestStablePromptCachePrefixesAcrossHistory(t *testing.T) {
	for _, stable := range []bool{false, true} {
		mode := "random"
		if stable {
			mode = "stable"
		}
		var previous string
		for round := 0; round < 3; round++ {
			raw := promptCacheExperimentRequest("test-model", "local-prefix", mode, round)
			s := NewSession("prefix-history", nil).WithStableSurrogates(stable)
			masked, err := AnonymizeClaudeRequest(raw, s)
			require.NoError(t, err)
			prefix, err := experimentSystem(masked)
			require.NoError(t, err)
			require.NotContains(t, prefix, "alice@example.test")
			require.NotContains(t, prefix, "192.0.2.88")
			if round > 0 {
				if stable {
					require.Equal(t, previous, prefix)
				} else {
					require.NotEqual(t, previous, prefix)
				}
			}
			previous = prefix
			restored, err := DeanonymizeTextFields(masked, s)
			require.NoError(t, err)
			require.JSONEq(t, string(raw), string(restored))
			if stable {
				other, err := AnonymizeClaudeRequest(raw, NewSession("other-token", nil).WithStableSurrogates(true))
				require.NoError(t, err)
				otherPrefix, err := experimentSystem(other)
				require.NoError(t, err)
				require.NotEqual(t, prefix, otherPrefix)
			}
		}
	}
}

func TestPromptCacheExperimentHarness(t *testing.T) {
	// This fixture validates the experiment's wiring and SSE measurement only;
	// its synthetic cache counts are not evidence of real provider cache hits.
	seen := make(map[string]bool)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("x-api-key") != "test-key" {
			t.Error("missing experiment authentication")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		prefix, err := experimentSystem(data)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		read, write := 0, 16000
		if seen[prefix] {
			read, write = 16000, 0
		}
		seen[prefix] = true
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":8,\"cache_read_input_tokens\":%d,\"cache_creation_input_tokens\":%d}}}\n\n", read, write)
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"OK\"}}\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":2}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()
	results, err := runPromptCacheExperiment(context.Background(), server.Client(), server.URL, "test-key", "test-model", "fixture")
	require.NoError(t, err)
	require.Equal(t, 4, calls)
	require.Len(t, results, 4)
	require.Zero(t, results[1].Usage.Read)
	require.EqualValues(t, 16000, results[3].Usage.Read)
	for _, r := range results {
		require.EqualValues(t, 2, r.Usage.Output)
		require.GreaterOrEqual(t, r.TTFTMS, float64(0))
		require.GreaterOrEqual(t, r.TotalMS, r.TTFTMS)
	}
	for _, data := range []string{"", "data: invalid\n", "data: {\"type\":\"error\"}\n", "data: {\"type\":\"message_stop\"}\n"} {
		require.Error(t, readPromptCacheStream(strings.NewReader(data), time.Now(), &promptCacheMeasurement{}))
	}
	var prices promptCachePrices
	require.NoError(t, json.Unmarshal([]byte(`{"input":3,"read":0.3,"write":3.75,"output":15}`), &prices))
	cost, err := prices.cost(promptCacheUsage{Input: 100, Read: 200, Write: 300, Output: 20})
	require.NoError(t, err)
	require.InDelta(t, 0.001785, cost, 1e-12)
	_, err = (promptCachePrices{}).cost(promptCacheUsage{})
	require.Error(t, err)
}

func TestLivePromptCacheExperiment(t *testing.T) {
	if os.Getenv("ANONYMIZATION_LIVE_EXPERIMENT") != "1" {
		t.Skip("opt-in paid upstream experiment; see docs/anonymization-performance.md")
	}
	endpoint, key, model := os.Getenv("ANONYMIZATION_EXPERIMENT_URL"), os.Getenv("ANONYMIZATION_EXPERIMENT_API_KEY"), os.Getenv("ANONYMIZATION_EXPERIMENT_MODEL")
	u, err := url.Parse(endpoint)
	require.NoError(t, err)
	require.True(t, u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "", "experiment requires an HTTPS Messages endpoint without URL credentials")
	require.NotEmpty(t, key, "ANONYMIZATION_EXPERIMENT_API_KEY is required")
	require.NotEmpty(t, model, "ANONYMIZATION_EXPERIMENT_MODEL is required")
	var prices *promptCachePrices
	if rawPrices := os.Getenv("ANONYMIZATION_EXPERIMENT_PRICES"); rawPrices != "" {
		prices = &promptCachePrices{}
		require.NoError(t, json.Unmarshal([]byte(rawPrices), prices))
		_, err := prices.cost(promptCacheUsage{})
		require.NoError(t, err)
	}
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	results, err := runPromptCacheExperiment(context.Background(), client, endpoint, key, model, fmt.Sprintf("run-%d", time.Now().UnixNano()))
	require.NoError(t, err)
	for _, result := range results {
		if prices != nil {
			cost, err := prices.cost(result.Usage)
			require.NoError(t, err)
			result.EstimatedCost = &cost
		}
		data, err := json.Marshal(result)
		require.NoError(t, err)
		t.Log(string(data))
	}
}
