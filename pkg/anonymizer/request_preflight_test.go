package anonymizer

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"new-router/common"
)

type requestPreflightDetector struct {
	t       *testing.T
	session *Session
	calls   map[string]int
}

func (d *requestPreflightDetector) Detect(_ context.Context, text string) ([]Match, error) {
	d.t.Helper()
	assert.True(d.t, d.session.preflighting, "detection must happen in the complete request preflight")
	assert.Empty(d.t, d.session.Snapshot(), "no alias may be generated before every raw text slot is detected")
	d.calls[text]++
	return nil, nil
}

func TestRequestPreflightReservesEveryOldFakeIPPoolBeforeGeneratingAliases(t *testing.T) {
	// Cover every address that the previous three /24 surrogate pools could
	// produce. A per-field transform would either leak a later real address or
	// exhaust those pools once the complete set of originals is reserved.
	originals := make(map[string]struct{}, 3*254)
	messages := make([]any, 0, 3*254)
	for _, prefix := range []string{"10.200.99.", "10.201.88.", "172.28.199."} {
		for host := 1; host <= 254; host++ {
			address := fmt.Sprintf("%s%d", prefix, host)
			originals[address] = struct{}{}
			messages = append(messages, map[string]any{"role": "user", "content": address})
		}
	}
	raw, err := common.Marshal(map[string]any{"messages": messages})
	require.NoError(t, err)
	session := NewSession("preflight", nil)
	detector := &requestPreflightDetector{t: t, session: session, calls: make(map[string]int)}
	session.WithDetector(context.Background(), detector)
	masked, err := AnonymizeChatRequest(raw, session)
	require.NoError(t, err)
	require.Len(t, detector.calls, len(originals))
	for original := range originals {
		assert.Equal(t, 1, detector.calls[original], "both passes must reuse one detector result for %s", original)
	}
	require.Len(t, session.Snapshot(), len(originals))
	for _, mapping := range session.Snapshot() {
		_, realAddress := originals[mapping.Surrogate]
		assert.False(t, realAddress, "alias must not equal any raw address in another field")
		assert.NotEqual(t, mapping.Original, mapping.Surrogate)
	}
	var body map[string]any
	require.NoError(t, common.UnmarshalWithNumber(masked, &body))
	for _, item := range body["messages"].([]any) {
		_, realAddress := originals[item.(map[string]any)["content"].(string)]
		assert.False(t, realAddress, "every actual request text value must be masked")
	}
	restored, err := DeanonymizeTextFields(masked, session)
	require.NoError(t, err)
	assert.JSONEq(t, string(raw), string(restored))
}

func TestImportedIPAliasIsRealInputAndRotatesConflictingForwardMapping(t *testing.T) {
	const original = "192.0.2.88"
	const oldAlias = "10.200.99.1"
	session := NewSession("history", nil)
	require.NoError(t, session.Import([]Mapping{{EntityType: "IP_ADDRESS", Original: original, Surrogate: oldAlias}}))
	raw := []byte(`{"messages":[{"role":"user","content":"192.0.2.88"},{"role":"user","content":"10.200.99.1"}]}`)
	masked, err := AnonymizeChatRequest(raw, session)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.UnmarshalWithNumber(masked, &body))
	messages := body["messages"].([]any)
	rotated := messages[0].(map[string]any)["content"].(string)
	newAlias := messages[1].(map[string]any)["content"].(string)
	assert.NotEqual(t, original, rotated)
	assert.NotEqual(t, oldAlias, rotated, "old alias is now a reserved real address")
	assert.NotEqual(t, oldAlias, newAlias, "a literal imported alias in raw input must be masked")
	assert.NotEqual(t, rotated, newAlias)
	assert.Equal(t, original+" / "+oldAlias, session.DeanonymizeString(oldAlias+" / "+newAlias), "history and fresh aliases restore in a single pass")
	require.Len(t, session.Snapshot(), 3, "retain the old reverse mapping while rotating the original's forward alias")
	restored, err := DeanonymizeTextFields(masked, session)
	require.NoError(t, err)
	assert.JSONEq(t, string(raw), string(restored))
	derived, err := AnonymizeChatRequest(masked, session)
	require.NoError(t, err)
	assert.JSONEq(t, string(masked), string(derived), "already transformed protocol input stays idempotent")
	replayed, err := AnonymizeChatRequest(raw, session)
	require.NoError(t, err)
	assert.JSONEq(t, string(masked), string(replayed), "retrying original input uses the same safe forward aliases")
	require.Len(t, session.Snapshot(), 3)
}

func TestImportedCustomAliasLiteralIsMaskedAndRawRetryRemainsStable(t *testing.T) {
	const original = "private business value"
	const oldAlias = "[CUSTOM_SAVED_ALIAS]"
	session := NewSession("custom-history", nil)
	require.NoError(t, session.Import([]Mapping{{EntityType: "CUSTOM", Original: original, Surrogate: oldAlias}}))
	raw := []byte(`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Literal [CUSTOM_SAVED_ALIAS] in new user text"}]}]}`)
	masked, err := AnonymizeResponsesRequest(raw, session)
	require.NoError(t, err)
	assert.NotContains(t, string(masked), oldAlias)
	require.Len(t, session.Snapshot(), 2)
	var newAlias string
	for _, mapping := range session.Snapshot() {
		if mapping.Original == oldAlias {
			newAlias = mapping.Surrogate
		}
	}
	require.NotEmpty(t, newAlias)
	assert.Equal(t, original+" / "+oldAlias, session.DeanonymizeString(oldAlias+" / "+newAlias))
	restored, err := DeanonymizeTextFields(masked, session)
	require.NoError(t, err)
	assert.JSONEq(t, string(raw), string(restored))
	derived, err := AnonymizeTextFields(masked, session)
	require.NoError(t, err)
	assert.JSONEq(t, string(masked), string(derived))
	replayed, err := AnonymizeResponsesRequest(raw, session)
	require.NoError(t, err)
	assert.JSONEq(t, string(masked), string(replayed))
}

func TestURLAliasAndItsIPRemainIdempotentAcrossRequestHelpers(t *testing.T) {
	raw := []byte(`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Fetch https://private.customer.example/secret"}]}]}`)
	session := NewSession("url-conversion", nil)
	masked, err := AnonymizeResponsesRequest(raw, session)
	require.NoError(t, err)
	require.Len(t, session.Snapshot(), 1)
	assert.Equal(t, "URL", session.Snapshot()[0].EntityType)
	assert.True(t, strings.HasPrefix(session.Snapshot()[0].Surrogate, "https://host-"))
	derived, err := AnonymizeTextFields(masked, session)
	require.NoError(t, err)
	assert.JSONEq(t, string(masked), string(derived), "the IP inside an existing URL alias must not create another alias")
	derived, err = AnonymizeResponsesRequest(derived, session)
	require.NoError(t, err)
	assert.JSONEq(t, string(masked), string(derived))
	require.Len(t, session.Snapshot(), 1)
	restored, err := DeanonymizeTextFields(derived, session)
	require.NoError(t, err)
	assert.JSONEq(t, string(raw), string(restored))
}

func TestBroadRuleOnJoinedDerivedTextStoresSemanticOriginals(t *testing.T) {
	rules, err := CompileRules([]Rule{{Name: "whole text", Mode: "regex", Pattern: `(?s)^.+$`, Enabled: true}})
	require.NoError(t, err)
	session := NewSession("joined-conversion", nil).WithRules(rules)
	masked, err := AnonymizeEmbeddingRequest([]byte(`{"input":["private alpha","private beta"]}`), session)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.UnmarshalWithNumber(masked, &body))
	input := body["input"].([]any)
	joined := input[0].(string) + " / " + input[1].(string)
	derived, err := common.Marshal(map[string]any{"prompt": joined})
	require.NoError(t, err)
	converted, err := AnonymizeCompletionRequest(derived, session)
	require.NoError(t, err)
	require.NoError(t, common.UnmarshalWithNumber(converted, &body))
	newAlias := body["prompt"].(string)
	assert.NotEqual(t, joined, newAlias)
	assert.Equal(t, "private alpha / private beta", session.DeanonymizeString(newAlias))
	require.Len(t, session.Snapshot(), 3)
	for _, mapping := range session.Snapshot() {
		assert.NotContains(t, mapping.Original, "[CUSTOM_", "mapping originals must contain restored business values")
	}
	response, err := common.Marshal(map[string]any{"output_text": newAlias})
	require.NoError(t, err)
	restored, err := DeanonymizeTextFields(response, session)
	require.NoError(t, err)
	assert.JSONEq(t, `{"output_text":"private alpha / private beta"}`, string(restored))
}

func TestNoncanonicalCIDRReservesItsCanonicalFormBeforeAliasRotation(t *testing.T) {
	const original = "172.16.0.0/24"
	const oldAlias = "10.200.99.0/24"
	const rawNetwork = "10.200.99.128/24"
	session := NewSession("cidr-history", nil)
	require.NoError(t, session.Import([]Mapping{{EntityType: "CIDR", Original: original, Surrogate: oldAlias}}))
	raw := []byte(`{"input":["172.16.0.0/24","10.200.99.128/24"]}`)
	masked, err := AnonymizeEmbeddingRequest(raw, session)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.UnmarshalWithNumber(masked, &body))
	input := body["input"].([]any)
	assert.NotEqual(t, oldAlias, input[0], "a noncanonical real network also reserves the equivalent canonical alias")
	assert.NotEqual(t, rawNetwork, input[1])
	for _, alias := range input {
		prefix, parseErr := netip.ParsePrefix(alias.(string))
		require.NoError(t, parseErr)
		assert.Equal(t, prefix.Masked(), prefix)
		assert.NotEqual(t, oldAlias, prefix.String())
	}
	restored, err := DeanonymizeTextFields(masked, session)
	require.NoError(t, err)
	assert.JSONEq(t, string(raw), string(restored), "restoration preserves the original spelling of the real CIDR")
}
