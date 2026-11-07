package anonymizer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalDetectorRequiresOperatorAssets(t *testing.T) {
	for _, dir := range []string{"", " ", filepath.Join(t.TempDir(), "missing-model")} {
		_, err := NewLocalDetector(dir)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), dir+string(os.PathSeparator))
	}
	var detector *LocalDetector
	_, err := detector.Detect(context.Background(), "private-sensitive-value")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "private-sensitive-value")
	assert.Equal(t, [32]byte{}, detector.Fingerprint())
	assert.Equal(t, "DATE_TIME", localEntityType("private_date"))
}

func TestLocalDetectorPinnedModelInference(t *testing.T) {
	dir := os.Getenv("ANONYMIZATION_TEST_LOCAL_MODEL_DIR")
	if dir == "" {
		t.Skip("ANONYMIZATION_TEST_LOCAL_MODEL_DIR is not set")
	}
	detector, err := NewLocalDetector(dir)
	require.NoError(t, err)
	require.NotEqual(t, [32]byte{}, detector.Fingerprint())
	const chinese = "联系人：张三，电话 13800138000，邮箱 zhangsan@example.com。"
	const english = "My name is Alice Johnson. My email address is alice.johnson@example.com."
	for _, text := range []string{chinese, english} {
		t.Run(text, func(t *testing.T) {
			matches, err := detector.Detect(context.Background(), text)
			require.NoError(t, err)
			found := make(map[string]string)
			for _, match := range matches {
				require.GreaterOrEqual(t, match.Start, 0)
				require.LessOrEqual(t, match.End, len(text))
				require.True(t, utf8.ValidString(text[match.Start:match.End]))
				assert.Equal(t, match.Text, text[match.Start:match.End])
				found[match.Text] = match.EntityType
			}
			if text == chinese {
				assert.Equal(t, "PERSON", found["张三"])
				assert.Equal(t, "PHONE_NUMBER", found["13800138000"])
				assert.Equal(t, "EMAIL_ADDRESS", found["zhangsan@example.com"])
			} else {
				assert.Equal(t, "PERSON", found["Alice Johnson"])
				assert.Equal(t, "EMAIL_ADDRESS", found["alice.johnson@example.com"])
			}
		})
	}
	t.Run("overlapping long history", func(t *testing.T) {
		text := strings.Repeat("ordinary words ", 250) + chinese + strings.Repeat(" ordinary words", 100)
		matches, err := detector.Detect(context.Background(), text)
		require.NoError(t, err)
		var person, email bool
		for _, match := range matches {
			person = person || match.EntityType == "PERSON" && match.Text == "张三"
			email = email || match.EntityType == "EMAIL_ADDRESS" && match.Text == "zhangsan@example.com"
		}
		assert.True(t, person)
		assert.True(t, email)
	})
	t.Run("concurrent reads", func(t *testing.T) {
		var wg sync.WaitGroup
		for range 2 {
			wg.Go(func() {
				matches, err := detector.Detect(context.Background(), chinese)
				assert.NoError(t, err)
				assert.NotEmpty(t, matches)
			})
		}
		wg.Wait()
	})
	t.Run("cancellation and invalid input", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := detector.Detect(ctx, chinese)
		require.ErrorIs(t, err, context.Canceled)
		_, err = detector.Detect(context.Background(), "invalid \xff")
		require.Error(t, err)
		_, err = detector.Detect(context.Background(), strings.Repeat("x", maxPresidioTextBytes+1))
		require.Error(t, err)
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		_, err = detector.Detect(ctx, strings.Repeat(chinese, 50))
		require.ErrorIs(t, err, context.DeadlineExceeded)
		_, err = detector.Detect(context.Background(), chinese)
		require.NoError(t, err, "cancelled inference must release its CPU capacity")
	})
}

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
