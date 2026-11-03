package anonymizer

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomRulesRejectContextualEmptyMatches(t *testing.T) {
	for _, pattern := range []string{`\b`, `^\b`, `(?:a|\b)`, `id:(a*)`} {
		group := 0
		if pattern == `id:(a*)` {
			group = 1
		}
		_, err := CompileRules([]Rule{{Name: "rule", Mode: "regex", Pattern: pattern, CaptureGroup: group, Enabled: true}})
		assert.Error(t, err, pattern)
	}
	_, err := CompileRules([]Rule{{Name: "valid", Mode: "regex", Pattern: `\bsecret\b`, Enabled: true}})
	assert.NoError(t, err)
}

func TestPresidioDoesNotRedirectPrivateBodies(t *testing.T) {
	forwarded := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded++; w.Write([]byte(`[]`)) }))
	defer target.Close()
	analyzer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer analyzer.Close()
	detector, err := NewPresidioDetector(analyzer.URL, "en", analyzer.Client())
	require.NoError(t, err)
	s := NewSession("test", nil).WithDetector(nil, detector)
	s.AnonymizeString("private customer data")
	assert.Error(t, s.Err())
	assert.Zero(t, forwarded)
}

func TestUnsignedAndSignedCompactionTextContracts(t *testing.T) {
	s := NewSession("test", nil)
	unsigned := []byte(`{"messages":[{"role":"assistant","content":[{"type":"compaction","content":"alice@private.example","tool_changes":[{"type":"tool_removal","name":"alice@private.example"}]}]}]}`)
	masked, err := AnonymizeClaudeRequest(unsigned, s)
	require.NoError(t, err)
	assert.NotContains(t, string(masked), `"content":"alice@private.example"`)
	assert.Contains(t, string(masked), `"name":"alice@private.example"`, "tool changes remain structural metadata")
	signed := []byte(`{"content":[{"type":"compaction","content":"alice@private.example","signature":"signature","encrypted_content":"opaque"}]}`)
	restored, err := DeanonymizeTextFields(signed, s)
	require.NoError(t, err)
	assert.JSONEq(t, string(signed), string(restored))
}
