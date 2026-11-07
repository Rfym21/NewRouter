package anonymizer

import (
	"net/netip"
	"net/url"
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
	assert.Regexp(t, regexp.MustCompile(`^public-ip-[a-z]{12}$`),
		GenerateStableSurrogate(salt, "8.8.8.8", "IP_ADDRESS", 0))
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`),
		GenerateStableSurrogate(salt, "123e4567-e89b-12d3-a456-426614174000", "IDENTIFIER", 0))
	assert.Len(t, GenerateStableSurrogate(salt, strings.Repeat("a", 40), "HASH", 0), 40)
}

func TestAddressSurrogatesRetainNetworkSemantics(t *testing.T) {
	for _, test := range []struct {
		original string
		prefix   string
		kind     string
	}{
		{original: "8.8.8.8", prefix: "public-ip-"},
		{original: "2606:4700:4700::1111", prefix: "public-ipv6-"},
		{original: "::ffff:8.8.8.8", prefix: "public-ipv4-mapped-"},
		{original: "0.0.0.0", prefix: "unspecified-ip-"},
		{original: "::", prefix: "unspecified-ipv6-"},
		{original: "::1", prefix: "loopback-ipv6-"},
		{original: "224.0.0.1", prefix: "multicast-ip-"},
		{original: "192.0.2.9", prefix: "documentation-ip-"},
		{original: "2001:db8::1234", prefix: "documentation-ipv6-"},
		{original: "100.64.1.2", prefix: "shared-ip-"},
		{original: "198.18.1.2", prefix: "benchmark-ip-"},
		{original: "240.1.2.3", prefix: "reserved-ip-"},
		{original: "10.2.3.4", kind: "private"},
		{original: "172.17.3.4", kind: "private"},
		{original: "192.168.3.4", kind: "private"},
		{original: "fd12:3456:789a::1", kind: "private"},
		{original: "::ffff:192.168.3.4", kind: "private"},
		{original: "127.0.0.1", kind: "loopback"},
		{original: "169.254.3.4", kind: "link-local"},
		{original: "fe80::1234", kind: "link-local"},
	} {
		t.Run(test.original, func(t *testing.T) {
			engagement := "address-semantics"
			vault := NewVault()
			vault.SetStableSurrogates(true)
			mapping := vault.GetOrCreate(engagement, test.original, "IP_ADDRESS")
			require.NotEmpty(t, mapping.Surrogate)
			require.NotEqual(t, test.original, mapping.Surrogate)
			if test.prefix != "" {
				assert.True(t, strings.HasPrefix(mapping.Surrogate, test.prefix), mapping.Surrogate)
				_, err := netip.ParseAddr(mapping.Surrogate)
				assert.Error(t, err, "a named stand-in cannot select a real public address")
			} else {
				address, err := netip.ParseAddr(mapping.Surrogate)
				require.NoError(t, err)
				original := netip.MustParseAddr(test.original)
				assert.Equal(t, original.Is4(), address.Is4())
				assert.Equal(t, original.Is4In6(), address.Is4In6())
				assert.Equal(t, test.kind, addressKind(address.Unmap()))
			}
			assert.Equal(t, test.original, NewSession(engagement, vault).DeanonymizeString(mapping.Surrogate))
			assert.Equal(t, mapping.Surrogate, GenerateStableSurrogate(stableSurrogateSalt(engagement), test.original, "IP_ADDRESS", 0))
			assert.NotEqual(t, mapping.Surrogate, GenerateStableSurrogate(stableSurrogateSalt("foreign-scope"), test.original, "IP_ADDRESS", 0))
		})
	}
}

func TestNetworkAndURLSurrogatesRetainUsefulStructure(t *testing.T) {
	salt := stableSurrogateSalt("network-structure")
	for _, original := range []string{"10.2.3.0/24", "172.16.2.0/25", "fd12:3456::/48"} {
		masked := GenerateStableSurrogate(salt, original, "CIDR", 0)
		prefix, err := netip.ParsePrefix(masked)
		require.NoError(t, err)
		assert.True(t, prefix.Addr().IsPrivate())
		assert.Equal(t, netip.MustParsePrefix(original).Bits(), prefix.Bits())
		assert.Equal(t, prefix.Masked(), prefix)
		assert.NotEqual(t, original, masked)
	}
	for _, test := range []struct {
		original string
		prefix   string
		suffix   string
	}{
		{original: "8.8.8.0/24", prefix: "public-ipv4-network-", suffix: "/24"},
		{original: "2606:4700::/32", prefix: "public-ipv6-network-", suffix: "/32"},
		{original: "10.0.0.0/8", prefix: "private-ipv4-network-", suffix: "/8"},
		{original: "192.0.2.0/24", prefix: "documentation-ipv4-network-", suffix: "/24"},
		{original: "2001:db8::/32", prefix: "documentation-ipv6-network-", suffix: "/32"},
	} {
		masked := GenerateStableSurrogate(salt, test.original, "CIDR", 0)
		assert.True(t, strings.HasPrefix(masked, test.prefix), masked)
		assert.True(t, strings.HasSuffix(masked, test.suffix), masked)
	}
	for _, original := range []string{
		"https://user:secret@8.8.8.8:8443/private?token=secret#private",
		"https://private.example:443/private",
		"http://192.168.2.3:8080/private",
		"https://[fd12:3456::1234]:8443/private",
		"http://localhost:9090/private",
	} {
		parsed, err := url.Parse(original)
		require.NoError(t, err)
		masked, err := url.Parse(GenerateStableSurrogate(salt, original, "URL", 0))
		require.NoError(t, err)
		assert.Equal(t, parsed.Scheme, masked.Scheme)
		assert.Equal(t, parsed.Port(), masked.Port())
		assert.Nil(t, masked.User)
		assert.Empty(t, masked.RawQuery)
		assert.Empty(t, masked.Fragment)
		assert.Equal(t, "/path", masked.Path)
		if address, err := netip.ParseAddr(parsed.Hostname()); err == nil && address.IsPrivate() {
			replacement, err := netip.ParseAddr(masked.Hostname())
			require.NoError(t, err)
			assert.True(t, replacement.IsPrivate())
			assert.Equal(t, address.Is6(), replacement.Is6())
		}
	}
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
	legacyIP := Mapping{EntityType: "IP_ADDRESS", Original: "93.184.216.43", Surrogate: "10.200.90.8"}
	require.NoError(t, session.Import([]Mapping{legacyIP}))
	assert.Equal(t, legacyIP.Surrogate, session.AnonymizeString(legacyIP.Original))
	assert.Equal(t, legacyIP.Original, session.DeanonymizeString(legacyIP.Surrogate),
		"saved IP mappings retain their original aliases after the shape change")
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
