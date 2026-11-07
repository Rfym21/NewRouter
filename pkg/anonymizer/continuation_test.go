package anonymizer

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"new-router/common"
)

func isolatedContinuationStore(t *testing.T) {
	t.Helper()
	previous, previousRedis, previousClient := defaultContextStore, common.RedisEnabled, common.RDB
	defaultContextStore = newContextStore(16, time.Hour, time.Now)
	common.RedisEnabled, common.RDB = false, nil
	t.Cleanup(func() {
		defaultContextStore = previous
		common.RedisEnabled = previousRedis
		common.RDB = previousClient
	})
}

func continuationContext(user, token int) (*gin.Context, *Session) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	session := NewSession(fmt.Sprintf("user:%d:token:%d", user, token), nil).WithStrictContinuation(true)
	Attach(c, session)
	AttachContinuation(c, user, token)
	return c, session
}

func TestCompactionContinuationKeepsMappingsAndTenantIsolation(t *testing.T) {
	isolatedContinuationStore(t)
	c, session := continuationContext(1, 10)
	masked := session.AnonymizeString("alice@private.example")
	require.NotEqual(t, "alice@private.example", masked)
	response := []byte(`{"id":"cmp-1","object":"response.compaction","output":[{"id":"item-1","type":"compaction","encrypted_content":"opaque-history"}]}`)
	require.NoError(t, ObserveResponse(c, response))
	request := []byte(`{"input":[{"id":"item-1","type":"compaction","encrypted_content":"opaque-history"},{"role":"user","content":"alice@private.example"}]}`)
	c2, s2 := continuationContext(1, 10)
	require.NoError(t, RestoreRequestContext(c2, request))
	assert.Equal(t, masked, s2.AnonymizeString("alice@private.example"))
	assert.Equal(t, "alice@private.example", s2.DeanonymizeString(masked))
	for _, identity := range [][2]int{{2, 10}, {1, 11}, {0, 0}} {
		foreign, foreignSession := continuationContext(identity[0], identity[1])
		assert.ErrorIs(t, RestoreRequestContext(foreign, request), ErrContinuationUnavailable)
		assert.Equal(t, masked, foreignSession.DeanonymizeString(masked))
	}
	assert.ErrorIs(t, RestoreRequestContext(c2, []byte(`{"input":[{"id":"item-1","type":"compaction","encrypted_content":"different-ciphertext"}]}`)), ErrContinuationUnavailable)
	assert.ErrorIs(t, RestoreRequestContext(c2, []byte(`{"previous_response_id":"unknown"}`)), ErrContinuationUnavailable)
}

func TestResponsesAndSignedHistoriesReuseRequestSnapshots(t *testing.T) {
	isolatedContinuationStore(t)
	c, session := continuationContext(3, 5)
	masked := session.AnonymizeString("alice@private.example")
	for _, event := range []string{
		`{"type":"response.created","response":{"id":"resp-1","object":"response","conversation":{"id":"conv-1"}}}`,
		`{"type":"response.output_item.done","item":{"type":"reasoning","encrypted_content":"reasoning-blob"}}`,
		`{"type":"response.output_item.added","item":{"id":"item-reference-1","type":"message"}}`,
		`{"id":"resp-2","object":"response","output":[{"id":"output-reference-1","type":"function_call","arguments":"{}"}]}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"signature_delta","signature":"signature-"}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"signature_delta","signature":"tail"}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"candidates":[{"content":{"parts":[{"thoughtSignature":"gemini-signature"}]}}]}`,
	} {
		require.NoError(t, ObserveResponse(c, []byte(event)))
	}
	for _, request := range []string{
		`{"previous_response_id":"resp-1"}`,
		`{"conversation":"conv-1"}`,
		`{"input":[{"type":"reasoning","encrypted_content":"reasoning-blob"}]}`,
		`{"input":[{"type":"item_reference","id":"item-reference-1"}]}`,
		`{"input":[{"type":"item_reference","id":"output-reference-1"}]}`,
		`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"opaque","signature":"signature-tail"}]}]}`,
		`{"contents":[{"parts":[{"thoughtSignature":"gemini-signature"}]}]}`,
	} {
		ctx, next := continuationContext(3, 5)
		require.NoError(t, RestoreRequestContext(ctx, []byte(request)))
		assert.Equal(t, masked, next.AnonymizeString("alice@private.example"))
		next.AnonymizeString("new@private.example")
		assert.Len(t, session.Snapshot(), 1, "loaded requests do not mutate a previous request's vault")
	}
	ctx, _ := continuationContext(3, 5)
	assert.NoError(t, RestoreRequestContext(ctx, []byte(`{"metadata":{"type":"compaction","encrypted_content":"not-a-history"}}`)), "metadata is not interpreted as opaque protocol state")
	assert.ErrorIs(t, RestoreRequestContext(ctx, []byte(`{"input":[{"type":"item_reference","id":"unknown-item"}]}`)), ErrContinuationUnavailable)
	foreign, _ := continuationContext(3, 6)
	assert.ErrorIs(t, RestoreRequestContext(foreign, []byte(`{"input":[{"type":"item_reference","id":"item-reference-1"}]}`)), ErrContinuationUnavailable)
}

func TestContextStoreExpiryEvictionAndBounds(t *testing.T) {
	isolatedContinuationStore(t)
	now := time.Unix(1000, 0)
	store := newContextStore(2, time.Minute, func() time.Time { return now })
	ctx := context.Background()
	mappings := []Mapping{{Original: "private", Surrogate: "fake", EntityType: "CUSTOM"}}
	require.NoError(t, store.save(ctx, "scope", []string{"first", "second"}, mappings))
	assert.Len(t, store.snapshots, 1, "aliases share a bounded immutable snapshot")
	now = now.Add(50 * time.Second)
	_, err := store.load(ctx, "scope", "first")
	require.NoError(t, err)
	now = now.Add(20 * time.Second)
	_, err = store.load(ctx, "scope", "second")
	require.ErrorIs(t, err, ErrContinuationUnavailable)
	_, err = store.load(ctx, "scope", "first")
	require.NoError(t, err, "active contexts have sliding expiry")
	require.NoError(t, store.save(ctx, "scope", []string{"third", "fourth"}, mappings))
	_, err = store.load(ctx, "scope", "first")
	assert.ErrorIs(t, err, ErrContinuationUnavailable)
	assert.Len(t, store.refs, 2)
	assert.Error(t, store.save(ctx, "scope", []string{"a", "b", "c"}, mappings))
	assert.Error(t, store.save(ctx, "scope", []string{"big"}, []Mapping{{Original: strings.Repeat("x", maxSavedMappingBytes), Surrogate: "fake", EntityType: "CUSTOM"}}))
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	assert.Error(t, store.save(canceled, "scope", []string{"canceled"}, mappings))
}

func TestRedisContinuationEncryptionAndCrossReplicaRecovery(t *testing.T) {
	isolatedContinuationStore(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { client.Close() })
	common.RedisEnabled, common.RDB = true, client
	previousSecret := common.CryptoSecret
	common.CryptoSecret = "shared-test-secret"
	t.Cleanup(func() { common.CryptoSecret = previousSecret })
	first := newContextStore(2, time.Minute, time.Now)
	second := newContextStore(2, time.Minute, time.Now)
	mappings := []Mapping{{Original: "very-private-secret", Surrogate: "fake", EntityType: "CUSTOM"}}
	require.NoError(t, first.save(context.Background(), "tenant-A", []string{"response"}, mappings))
	key := continuationCacheKey("tenant-A", "response")
	encoded, err := server.Get(key)
	require.NoError(t, err)
	assert.NotContains(t, encoded, "very-private-secret")
	loaded, err := second.load(context.Background(), "tenant-A", "response")
	require.NoError(t, err)
	assert.Equal(t, mappings, loaded)
	_, err = second.load(context.Background(), "tenant-B", "response")
	assert.ErrorIs(t, err, ErrContinuationUnavailable)
	require.NoError(t, server.Set(continuationCacheKey("tenant-B", "response"), encoded))
	_, err = second.load(context.Background(), "tenant-B", "response")
	assert.ErrorIs(t, err, ErrContinuationUnavailable, "ciphertext is bound to the tenant-specific key")
	server.FastForward(2 * time.Minute)
	_, err = second.load(context.Background(), "tenant-A", "response")
	assert.ErrorIs(t, err, ErrContinuationUnavailable)
}

func TestImportedMappingsDoNotCascadeOrShareDefaultVault(t *testing.T) {
	s := NewSession("one", nil)
	require.NoError(t, s.Import([]Mapping{{Original: "fake-B", Surrogate: "fake-A", EntityType: "CUSTOM"}, {Original: "secret", Surrogate: "fake-B", EntityType: "CUSTOM"}}))
	assert.Equal(t, "fake-B secret", s.DeanonymizeString("fake-A fake-B"), "inserted originals are not recursively transformed")
	assert.Empty(t, NewSession("one", nil).Snapshot())
	assert.Error(t, s.Import([]Mapping{{Original: "another-secret", Surrogate: "fake-A", EntityType: "CUSTOM"}}))
}

func TestContinuationFallbackKeepsAvailableMappingsAfterMissingReferences(t *testing.T) {
	isolatedContinuationStore(t)
	c, original := continuationContext(4, 9)
	alias := original.AnonymizeString("alice@private.example")
	require.NoError(t, ObserveResponse(c, []byte(`{"id":"retained","object":"response","conversation":"available"}`)))
	request := []byte(`{"previous_response_id":"unknown","input":[{"type":"item_reference","id":"missing-item"},{"type":"reasoning","encrypted_content":"missing-blob"}],"conversation":"available"}`)
	next, session := continuationContext(4, 9)
	session.WithStrictContinuation(false)
	require.NoError(t, RestoreRequestContext(next, request))
	assert.Equal(t, "alice@private.example", session.DeanonymizeString(alias))
	assert.Len(t, session.Snapshot(), 1)
	require.NoError(t, RestoreRequestContext(next, request), "conversions can revisit skipped references")
	foreign, foreignSession := continuationContext(4, 10)
	foreignSession.WithStrictContinuation(false)
	require.NoError(t, RestoreRequestContext(foreign, request))
	assert.Empty(t, foreignSession.Snapshot(), "fallback preserves authenticated partitions")
}

func TestExpiredContinuationFallbackInMemoryAndRedis(t *testing.T) {
	for _, useRedis := range []bool{false, true} {
		t.Run(fmt.Sprintf("redis=%t", useRedis), func(t *testing.T) {
			isolatedContinuationStore(t)
			now := time.Unix(1000, 0)
			defaultContextStore = newContextStore(16, time.Minute, func() time.Time { return now })
			var server *miniredis.Miniredis
			if useRedis {
				server = miniredis.RunT(t)
				client := redis.NewClient(&redis.Options{Addr: server.Addr()})
				t.Cleanup(func() { assert.NoError(t, client.Close()) })
				common.RedisEnabled, common.RDB = true, client
			}
			c, session := continuationContext(6, 7)
			session.AnonymizeString("alice@private.example")
			require.NoError(t, ObserveResponse(c, []byte(`{"id":"expired","object":"response"}`)))
			now = now.Add(2 * time.Minute)
			if server != nil {
				server.FastForward(2 * time.Minute)
			}
			for _, strict := range []bool{false, true} {
				c, session := continuationContext(6, 7)
				session.WithStrictContinuation(strict)
				err := RestoreRequestContext(c, []byte(`{"previous_response_id":"expired"}`))
				if strict {
					require.ErrorIs(t, err, ErrContinuationUnavailable)
				} else {
					require.NoError(t, err)
					assert.Empty(t, session.Snapshot())
				}
			}
		})
	}
}

func TestContinuationFallbackPreservesConflictsAndCancellation(t *testing.T) {
	isolatedContinuationStore(t)
	first := []Mapping{{Original: "private-a", Surrogate: "same-alias", EntityType: "CUSTOM"}}
	second := []Mapping{{Original: "private-b", Surrogate: "same-alias", EntityType: "CUSTOM"}}
	require.NoError(t, defaultContextStore.save(context.Background(), "user:8:token:9", []string{"response:first"}, first))
	require.NoError(t, defaultContextStore.save(context.Background(), "user:8:token:9", []string{"conversation:second"}, second))
	c, session := continuationContext(8, 9)
	session.WithStrictContinuation(false)
	require.EqualError(t, RestoreRequestContext(c, []byte(`{"previous_response_id":"first","conversation":"second"}`)), "conflicting anonymization contexts")
	c, session = continuationContext(8, 9)
	session.WithStrictContinuation(false)
	ctx, cancel := context.WithCancel(c.Request.Context())
	cancel()
	c.Request = c.Request.WithContext(ctx)
	require.ErrorIs(t, RestoreRequestContext(c, []byte(`{"previous_response_id":"missing"}`)), context.Canceled)
}
