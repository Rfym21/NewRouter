package anonymizer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Detector adds a local or explicitly configured analyzer to the built-in
// structured-data rules. No network analyzer is configured by default.
type Detector interface {
	Detect(context.Context, string) ([]Match, error)
}

const (
	maxPresidioTextBytes     = 1 << 20
	maxPresidioResponseBytes = 1 << 20
	maxPresidioMatches       = 4096
	presidioRequestTimeout   = 5 * time.Second
)

var presidioEntityType = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,63}$`)
var presidioLanguage = regexp.MustCompile(`^[a-zA-Z][a-zA-Z-]{1,19}$`)

type PresidioDetector struct {
	endpoint string
	language string
	client   *http.Client
}

// NewPresidioDetector accepts an analyzer base URL or its explicit /analyze
// endpoint. Clients can supply a private-network transport; each request still
// has an upper time and response-size bound.
func NewPresidioDetector(endpoint, language string, client *http.Client) (*PresidioDetector, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed == nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("invalid Presidio analyzer endpoint")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(parsed.Path, "/analyze") {
		parsed.Path += "/analyze"
	}
	language = strings.TrimSpace(language)
	if language == "" {
		language = "en"
	}
	if !presidioLanguage.MatchString(language) {
		return nil, errors.New("invalid Presidio analyzer language")
	}
	if client == nil {
		client = &http.Client{Timeout: presidioRequestTimeout}
	}
	// Analyzer redirects must not replay private request bodies to another URL.
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &PresidioDetector{endpoint: parsed.String(), language: language, client: &copyClient}, nil
}

type presidioSpan struct {
	EntityType string  `json:"entity_type"`
	Start      int     `json:"start"`
	End        int     `json:"end"`
	Score      float64 `json:"score"`
}

func (p *PresidioDetector) Detect(ctx context.Context, text string) ([]Match, error) {
	if p == nil || p.client == nil {
		return nil, errors.New("Presidio analyzer unavailable")
	}
	if len(text) > maxPresidioTextBytes || !utf8.ValidString(text) {
		return nil, errors.New("invalid Presidio analyzer text size or encoding")
	}
	if text == "" {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, presidioRequestTimeout)
	defer cancel()
	payload, _ := json.Marshal(struct {
		Text     string `json:"text"`
		Language string `json:"language"`
	}{Text: text, Language: p.language})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, errors.New("failed to prepare Presidio analyzer request")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(request)
	if err != nil {
		// Transport errors may include endpoints or server diagnostics. Never
		// forward them into logs or client errors along with the private input.
		return nil, errors.New("Presidio analyzer request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Presidio analyzer returned status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPresidioResponseBytes+1))
	if err != nil || len(body) > maxPresidioResponseBytes {
		return nil, errors.New("invalid Presidio analyzer response size")
	}
	var spans []presidioSpan
	if err := json.Unmarshal(body, &spans); err != nil || bytes.Equal(bytes.TrimSpace(body), []byte("null")) || len(spans) > maxPresidioMatches {
		return nil, errors.New("invalid Presidio analyzer response")
	}
	// Presidio offsets count Unicode code points (Python str), not UTF-8 bytes.
	runes := []rune(text)
	byteOffsets := make([]int, 0, len(runes)+1)
	for offset := range text {
		byteOffsets = append(byteOffsets, offset)
	}
	byteOffsets = append(byteOffsets, len(text))
	type spanKey struct {
		EntityType string
		Start, End int
	}
	seen := make(map[spanKey]struct{}, len(spans))
	matches := make([]Match, 0, len(spans))
	for _, span := range spans {
		if span.Start < 0 || span.End <= span.Start || span.End > len(runes) || !presidioEntityType.MatchString(span.EntityType) || math.IsNaN(span.Score) || math.IsInf(span.Score, 0) || span.Score < 0 || span.Score > 1 {
			return nil, errors.New("Presidio analyzer returned invalid entity spans")
		}
		key := spanKey{span.EntityType, span.Start, span.End}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		matches = append(matches, Match{Text: string(runes[span.Start:span.End]), EntityType: span.EntityType, Start: byteOffsets[span.Start], End: byteOffsets[span.End]})
	}
	return matches, nil
}
