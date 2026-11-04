package anonymizer

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateStableSurrogateDeterminism(t *testing.T) {
	salt := stableSurrogateSalt("user:1:token:1")
	first := GenerateStableSurrogate(salt, "alice@example.test", "EMAIL", 0)
	require.NotEmpty(t, first)
	assert.Equal(t, first, GenerateStableSurrogate(salt, "alice@example.test", "EMAIL", 0))
	// The attempt index feeds the derivation, so collision retries replay the
	// same candidate sequence instead of diverging.
	assert.NotEqual(t, first, GenerateStableSurrogate(salt, "alice@example.test", "EMAIL", 1))
	// A different engagement salt must not reproduce another scope's pseudonym.
	assert.NotEqual(t, first, GenerateStableSurrogate(stableSurrogateSalt("user:2:token:1"), "alice@example.test", "EMAIL", 0))
}

func TestGenerateStableSurrogateKeepsFormats(t *testing.T) {
	salt := stableSurrogateSalt("user:1:token:1")
	assert.Regexp(t, regexp.MustCompile(`^[a-z]{8}@example\.pentest$`),
		GenerateStableSurrogate(salt, "alice@example.test", "EMAIL", 0))
	assert.Regexp(t, regexp.MustCompile(`^10\.\d{1,3}\.\d{1,3}\.\d{1,3}$`),
		GenerateStableSurrogate(salt, "8.8.8.8", "IP_ADDRESS", 0))
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`),
		GenerateStableSurrogate(salt, "123e4567-e89b-12d3-a456-426614174000", "IDENTIFIER", 0))
	assert.Len(t, GenerateStableSurrogate(salt, strings.Repeat("a", 40), "HASH", 0), 40)
}

func TestVaultStableSurrogatesRepeatAcrossRequests(t *testing.T) {
	first := NewVault()
	first.SetStableSurrogates(true)
	mapping := first.GetOrCreate("user:1:token:1", "alice@example.test", "EMAIL")
	require.NotEmpty(t, mapping.Surrogate)

	// An independent vault models the next request: the same engagement and
	// original must map to the same surrogate so full-history clients keep
	// byte-identical prefixes.
	second := NewVault()
	second.SetStableSurrogates(true)
	assert.Equal(t, mapping.Surrogate, second.GetOrCreate("user:1:token:1", "alice@example.test", "EMAIL").Surrogate)

	foreign := NewVault()
	foreign.SetStableSurrogates(true)
	assert.NotEqual(t, mapping.Surrogate, foreign.GetOrCreate("user:2:token:1", "alice@example.test", "EMAIL").Surrogate)

	randomA := NewVault().GetOrCreate("user:1:token:1", "alice@example.test", "EMAIL")
	randomB := NewVault().GetOrCreate("user:1:token:1", "alice@example.test", "EMAIL")
	assert.NotEqual(t, randomA.Surrogate, randomB.Surrogate, "default mode draws fresh randomness per vault")
}

func TestSessionStableSurrogatesKeepPrefixesIdentical(t *testing.T) {
	text := "contact alice@example.test before friday"
	first := NewSession("user:1:token:1", nil).WithStableSurrogates(true)
	masked := first.AnonymizeString(text)
	require.NotEqual(t, text, masked)

	second := NewSession("user:1:token:1", nil).WithStableSurrogates(true)
	assert.Equal(t, masked, second.AnonymizeString(text))
	assert.Equal(t, text, second.DeanonymizeString(masked),
		"a later request restores its own stable aliases without a saved context")
}

func TestStableSurrogatesYieldToImportedMappings(t *testing.T) {
	session := NewSession("user:1:token:1", nil).WithStableSurrogates(true)
	legacy := Mapping{EntityType: "EMAIL_ADDRESS", Original: "alice@example.test", Surrogate: "legacy@example.pentest"}
	require.NoError(t, session.Import([]Mapping{legacy}))
	assert.Equal(t, "legacy@example.pentest", session.AnonymizeString("alice@example.test"),
		"restored continuation mappings keep precedence over freshly derived ones")
}

func TestSetContextStoreLimitsClampsAndApplies(t *testing.T) {
	isolatedContinuationStore(t)
	now := time.Unix(1000, 0)
	defaultContextStore = newContextStore(64, time.Hour, func() time.Time { return now })

	SetContextStoreLimits(5, 10)
	maxEntries, ttl := defaultContextStore.limits()
	assert.Equal(t, 16, maxEntries)
	assert.Equal(t, 60*time.Second, ttl)

	SetContextStoreLimits(1_000_000, 1_000_000)
	maxEntries, ttl = defaultContextStore.limits()
	assert.Equal(t, 65536, maxEntries)
	assert.Equal(t, 24*3600*time.Second, ttl)

	SetContextStoreLimits(128, 60)
	c, session := continuationContext(7, 8)
	masked := session.AnonymizeString("alice@private.example")
	require.NoError(t, ObserveResponse(c, []byte(`{"id":"resp-1","object":"response"}`)))
	now = now.Add(61 * time.Second)
	next, _ := continuationContext(7, 8)
	assert.ErrorIs(t, RestoreRequestContext(next, []byte(`{"previous_response_id":"resp-1"}`)), ErrContinuationUnavailable,
		"a clamped-down TTL expires saved contexts")
	assert.Equal(t, "alice@private.example", session.DeanonymizeString(masked))
}
