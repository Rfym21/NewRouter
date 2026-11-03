package anonymizer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPresidioAdapterValidatesUnicodeSpansAndDeduplicatesEntities(t *testing.T) {
	const text = "🙂张三 alice@tenant.test"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/analyze", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var request map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		assert.Equal(t, text, request["text"])
		assert.Equal(t, "zh", request["language"])
		fmt.Fprint(w, `[{"entity_type":"PERSON","start":1,"end":3,"score":0.95},{"entity_type":"PERSON","start":1,"end":3,"score":0.8}]`)
	}))
	defer server.Close()
	detector, err := NewPresidioDetector(server.URL, "zh", server.Client())
	require.NoError(t, err)
	matches, err := detector.Detect(context.Background(), text)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, "张三", matches[0].Text)
	assert.Equal(t, "PERSON", matches[0].EntityType)
	assert.Equal(t, strings.Index(text, "张三"), matches[0].Start)
	assert.Equal(t, matches[0].Text, text[matches[0].Start:matches[0].End])
}

func TestPresidioAdapterRejectsUntrustedResponsesWithoutEchoingPrivateText(t *testing.T) {
	const private = "private-sensitive-value"
	for _, body := range []string{
		`[{"entity_type":"PERSON","start":-1,"end":3,"score":0.9}]`,
		`[{"entity_type":"PERSON","start":1,"end":9999,"score":0.9}]`,
		`[{"entity_type":"PERSON","start":1,"end":1,"score":0.9}]`,
		`[{"entity_type":"PERSON","start":1,"end":3,"score":1.9}]`,
		`[{"entity_type":"private-sensitive-value","start":1,"end":3,"score":0.9}]`,
		`null`,
		`{"error":"private-sensitive-value"}`,
		strings.Repeat("private-sensitive-value", maxPresidioResponseBytes/len(private)+1),
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		detector, err := NewPresidioDetector(server.URL+"/analyze", "", server.Client())
		require.NoError(t, err)
		_, err = detector.Detect(context.Background(), private)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), private)
		server.Close()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, private)
	}))
	defer server.Close()
	detector, err := NewPresidioDetector(server.URL, "", server.Client())
	require.NoError(t, err)
	_, err = detector.Detect(context.Background(), private)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), private)
	_, err = detector.Detect(context.Background(), strings.Repeat("x", maxPresidioTextBytes+1))
	require.Error(t, err)
}

func TestPresidioAdapterConfigurationAndCancellation(t *testing.T) {
	for _, endpoint := range []string{"", "localhost:3000", "file:///private", "ftp://example.com", "http://user:password@example.com", "http://example.com?private=secret", "http://example.com#secret"} {
		_, err := NewPresidioDetector(endpoint, "en", nil)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "password")
	}
	_, err := NewPresidioDetector("http://localhost:3000", "invalid language", nil)
	require.Error(t, err)
	detector, err := NewPresidioDetector("http://127.0.0.1:1", "en", nil)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = detector.Detect(ctx, "private-sensitive-value")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "private-sensitive-value")
	assert.NotContains(t, err.Error(), "127.0.0.1")
}
