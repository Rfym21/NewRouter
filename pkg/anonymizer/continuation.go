package anonymizer

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"new-router/common"
)

const continuationKey = "new-router.anonymizer.continuation"
const (
	defaultMaxSavedMappingBytes = 4 << 20
	minSavedMappingBytes        = 64 << 10
	maxSavedMappingBytes        = 16 << 20
)

// ErrContinuationUnavailable indicates that saved history mappings cannot be loaded.
var ErrContinuationUnavailable = errors.New("anonymization context is unavailable or expired; start a new conversation")

var errContextCapacityExceeded = errors.New("anonymization context capacity exceeded")

type continuationState struct {
	scope      string
	seen       map[string]bool
	signatures map[string]string
}

// AttachContinuation partitions opaque upstream state by authenticated user and
// token. No user-provided identifier can select another partition.
func AttachContinuation(c *gin.Context, userID, tokenID int) {
	if c == nil {
		return
	}
	if userID <= 0 {
		c.Set(continuationKey, (*continuationState)(nil))
		return
	}
	scope := fmt.Sprintf("user:%d:token:%d", userID, tokenID)
	if state, _ := c.Get(continuationKey); state != nil {
		if current, ok := state.(*continuationState); ok && current != nil && current.scope == scope {
			return
		}
	}
	c.Set(continuationKey, &continuationState{scope: scope, seen: make(map[string]bool), signatures: make(map[string]string)})
}

func continuationFromContext(c *gin.Context) *continuationState {
	if c == nil {
		return nil
	}
	value, _ := c.Get(continuationKey)
	state, _ := value.(*continuationState)
	return state
}

func opaqueReference(kind, value string) string {
	digest := sha256.Sum256([]byte(value))
	return kind + ":" + hex.EncodeToString(digest[:])
}

// RestoreRequestContext imports mappings before transforming a replayed history.
// Unavailable mappings are skipped unless the session requires strict continuity.
// Opaque artifacts remain untouched and may contain aliases we can no longer restore.
func RestoreRequestContext(c *gin.Context, data []byte) error {
	var body map[string]any
	if err := common.UnmarshalWithNumber(data, &body); err != nil {
		return err
	}
	refs := []string{}
	if id, ok := body["previous_response_id"].(string); ok && id != "" {
		refs = append(refs, "response:"+id)
	}
	if id := conversationID(body["conversation"]); id != "" {
		refs = append(refs, "conversation:"+id)
	}
	refs = append(refs, opaqueReferences(body["input"])...)
	refs = append(refs, itemReferences(body["input"])...)
	refs = append(refs, opaqueReferences(body["messages"])...)
	refs = append(refs, opaqueReferences(body["contents"])...)
	unique := make([]string, 0, len(refs))
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if !seen[ref] {
			unique = append(unique, ref)
			seen[ref] = true
		}
	}
	refs = unique
	if len(refs) == 0 {
		return nil
	}
	if len(refs) > 256 {
		return errors.New("too many anonymization context references")
	}
	state, session := continuationFromContext(c), FromContext(c)
	if state == nil || session == nil {
		return ErrContinuationUnavailable
	}
	missing := 0
	defer func() {
		if missing > 0 {
			common.SysLog(fmt.Sprintf("anonymization continuation degraded: %d unavailable context references skipped", missing))
		}
	}()
	for _, ref := range refs {
		marker := "request:" + ref
		if state.seen[marker] {
			continue
		}
		mappings, err := defaultContextStore.load(requestContext(c), state.scope, ref)
		if err != nil {
			if contextErr := requestContext(c).Err(); contextErr != nil {
				return contextErr
			}
			if !session.strictContext && errors.Is(err, ErrContinuationUnavailable) {
				state.seen[marker] = true
				missing++
				continue
			}
			return ErrContinuationUnavailable
		}
		if err := session.Import(mappings); err != nil {
			return err
		}
		state.seen[marker] = true
	}
	return nil
}

func conversationID(value any) string {
	if id, ok := value.(string); ok {
		return id
	}
	if object, ok := value.(map[string]any); ok {
		id, _ := object["id"].(string)
		return id
	}
	return ""
}

func itemReferences(value any) []string {
	var refs []string
	switch items := value.(type) {
	case []any:
		for _, item := range items {
			refs = append(refs, itemReferences(item)...)
		}
	case map[string]any:
		if items["type"] == "item_reference" {
			if id, ok := items["id"].(string); ok && id != "" {
				refs = append(refs, "item:"+id)
			}
		}
	}
	return refs
}

func outputItemReferences(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	var refs []string
	for _, item := range items {
		if object, ok := item.(map[string]any); ok {
			if id, ok := object["id"].(string); ok && id != "" {
				refs = append(refs, "item:"+id)
			}
		}
	}
	return refs
}

func opaqueReferences(value any) []string {
	var refs []string
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			refs = append(refs, opaqueReferences(item)...)
		}
	case map[string]any:
		typeName, _ := v["type"].(string)
		if typeName == "reasoning" || typeName == "compaction" {
			if blob, ok := v["encrypted_content"].(string); ok && blob != "" {
				refs = append(refs, opaqueReference("encrypted", blob))
			}
		}
		if typeName == "thinking" || typeName == "compaction" {
			if signature, ok := v["signature"].(string); ok && signature != "" {
				refs = append(refs, opaqueReference("anthropic", signature))
			}
		}
		if typeName == "redacted_thinking" {
			if blob, ok := v["data"].(string); ok && blob != "" {
				refs = append(refs, opaqueReference("anthropic-redacted", blob))
			}
		}
		if signature, ok := v["thoughtSignature"].(string); ok && signature != "" {
			refs = append(refs, opaqueReference("gemini", signature))
		}
		if signature, ok := v["thought_signature"].(string); ok && signature != "" {
			refs = append(refs, opaqueReference("gemini", signature))
		}
		// Only protocol containers are traversed, never metadata or tool schemas.
		for _, key := range []string{"content", "parts"} {
			refs = append(refs, opaqueReferences(v[key])...)
		}
	}
	return refs
}

// ObserveResponse publishes the immutable request mappings before exposing a
// response ID or opaque state to the client. It never stores response bodies.
func ObserveResponse(c *gin.Context, data []byte) error {
	state, session := continuationFromContext(c), FromContext(c)
	if state == nil || session == nil {
		return nil
	}
	var body map[string]any
	if err := common.UnmarshalWithNumber(data, &body); err != nil {
		return nil
	}
	refs := []string{}
	response, nested := body["response"].(map[string]any)
	if !nested {
		response = body
	}
	object, _ := response["object"].(string)
	if nested || object == "response" || object == "response.compaction" {
		if id, ok := response["id"].(string); ok && id != "" {
			refs = append(refs, "response:"+id)
		}
		if id := conversationID(response["conversation"]); id != "" {
			refs = append(refs, "conversation:"+id)
		}
		refs = append(refs, opaqueReferences(response["output"])...)
		refs = append(refs, outputItemReferences(response["output"])...)
	}
	refs = append(refs, opaqueReferences(body["item"])...)
	if eventType, ok := body["type"].(string); ok && strings.HasPrefix(eventType, "response.") {
		if item, ok := body["item"].(map[string]any); ok {
			if id, ok := item["id"].(string); ok && id != "" {
				refs = append(refs, "item:"+id)
			}
		}
	}
	refs = append(refs, opaqueReferences(body["content"])...)
	if candidates, ok := body["candidates"].([]any); ok {
		for _, candidate := range candidates {
			if item, ok := candidate.(map[string]any); ok {
				refs = append(refs, opaqueReferences(item["content"])...)
			}
		}
	}
	typeName, _ := body["type"].(string)
	index := fmt.Sprint(body["index"])
	if typeName == "content_block_start" {
		refs = append(refs, opaqueReferences(body["content_block"])...)
	}
	if typeName == "content_block_delta" {
		if delta, ok := body["delta"].(map[string]any); ok && delta["type"] == "signature_delta" {
			fragment, _ := delta["signature"].(string)
			if len(state.signatures) >= 256 || len(state.signatures[index])+len(fragment) > 65536 {
				return errors.New("anonymization signature capacity exceeded")
			}
			state.signatures[index] += fragment
		}
	}
	if typeName == "content_block_stop" {
		if signature := state.signatures[index]; signature != "" {
			refs = append(refs, opaqueReference("anthropic", signature))
			delete(state.signatures, index)
		}
	}
	newRefs := make([]string, 0, len(refs))
	batchSeen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if !state.seen["response:"+ref] && !batchSeen[ref] {
			newRefs = append(newRefs, ref)
			batchSeen[ref] = true
		}
	}
	if len(newRefs) == 0 {
		return nil
	}
	if err := defaultContextStore.save(requestContext(c), state.scope, newRefs, session.Snapshot()); err != nil {
		if contextErr := requestContext(c).Err(); contextErr != nil {
			return contextErr
		}
		// Retention is an optimization for later turns. A storage or size limit
		// must not turn an otherwise valid response into a failed request.
		common.SysLog(fmt.Sprintf("anonymization continuation degraded: could not retain context: %v", err))
		for _, ref := range newRefs {
			state.seen["response:"+ref] = true
		}
		return nil
	}
	for _, ref := range newRefs {
		state.seen["response:"+ref] = true
	}
	return nil
}

func requestContext(c *gin.Context) context.Context {
	if c != nil && c.Request != nil {
		return c.Request.Context()
	}
	return context.Background()
}

type savedContext struct {
	data []byte
	refs int
}
type savedReference struct {
	snapshot string
	expires  time.Time
	sequence uint64
}
type contextStore struct {
	mu         sync.Mutex
	refs       map[string]savedReference
	snapshots  map[string]*savedContext
	bytes      int
	sequence   uint64
	maxEntries int
	maxBytes   int
	ttl        time.Duration
	now        func() time.Time
}

func newContextStore(maxEntries int, ttl time.Duration, now func() time.Time) *contextStore {
	return &contextStore{
		refs:       make(map[string]savedReference),
		snapshots:  make(map[string]*savedContext),
		maxEntries: maxEntries,
		maxBytes:   defaultMaxSavedMappingBytes,
		ttl:        ttl,
		now:        now,
	}
}

// clampContextLimits enforces the same bounds as the environment defaults.
func clampContextLimits(maxEntries, ttlSeconds, maxBytes int) (int, time.Duration, int) {
	return min(max(maxEntries, 16), 65536),
		time.Duration(min(max(ttlSeconds, 60), 86400)) * time.Second,
		min(max(maxBytes, minSavedMappingBytes), maxSavedMappingBytes)
}

var defaultContextStore = func() *contextStore {
	entries, ttl, maxBytes := clampContextLimits(
		common.GetEnvOrDefault("ANONYMIZATION_MAX_CONTEXTS", 1024),
		common.GetEnvOrDefault("ANONYMIZATION_CONTEXT_TTL_SECONDS", 3600),
		common.GetEnvOrDefault("ANONYMIZATION_MAX_CONTEXT_KB", defaultMaxSavedMappingBytes>>10)<<10,
	)
	store := newContextStore(entries, ttl, time.Now)
	store.maxBytes = maxBytes
	return store
}()

// SetContextStoreLimits hot-applies runtime limits to the default continuation
// store. A shorter TTL only affects saves and renewals from this point on;
// entries already stored in Redis keep their remaining TTL.
func SetContextStoreLimits(maxEntries, ttlSeconds, maxBytes int) {
	entries, ttl, maxBytes := clampContextLimits(maxEntries, ttlSeconds, maxBytes)
	s := defaultContextStore
	s.mu.Lock()
	s.maxEntries = entries
	s.maxBytes = maxBytes
	s.ttl = ttl
	s.mu.Unlock()
}

// limits snapshots the runtime-mutable bounds under the store lock; the Redis
// paths run outside it because their pipelines hold their own timeouts.
func (s *contextStore) limits() (int, time.Duration, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxEntries, s.ttl, s.maxBytes
}

func continuationCacheKey(scope, ref string) string {
	digest := sha256.Sum256([]byte(scope + "\x00" + ref))
	return "anonymization:v1:" + hex.EncodeToString(digest[:])
}

func encodeMappings(mappings []Mapping, maxBytes int) ([]byte, error) {
	sort.Slice(mappings, func(i, j int) bool {
		if mappings[i].Surrogate == mappings[j].Surrogate {
			return mappings[i].Original < mappings[j].Original
		}
		return mappings[i].Surrogate < mappings[j].Surrogate
	})
	data, err := json.Marshal(mappings)
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, errContextCapacityExceeded
	}
	return data, nil
}

func (s *contextStore) save(ctx context.Context, scope string, refs []string, mappings []Mapping) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	maxEntries, _, maxBytes := s.limits()
	if len(refs) > maxEntries {
		return errors.New("too many anonymization context references")
	}
	data, err := encodeMappings(mappings, maxBytes)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(refs))
	for _, ref := range refs {
		keys = append(keys, continuationCacheKey(scope, ref))
	}
	if common.RedisEnabled && common.RDB != nil {
		return s.saveRedis(ctx, keys, data)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpired()
	digest := sha256.Sum256(append([]byte(scope+"\x00"), data...))
	id := hex.EncodeToString(digest[:])
	for _, key := range keys {
		if previous, ok := s.refs[key]; ok {
			if previous.snapshot == id {
				s.sequence++
				previous.sequence = s.sequence
				previous.expires = s.now().Add(s.ttl)
				s.refs[key] = previous
				continue
			}
			s.remove(key)
		}
		blob := s.snapshots[id]
		if blob == nil {
			blob = &savedContext{data: data}
			s.snapshots[id] = blob
			s.bytes += len(data)
		}
		blob.refs++
		s.sequence++
		s.refs[key] = savedReference{snapshot: id, expires: s.now().Add(s.ttl), sequence: s.sequence}
	}
	totalBytes := max(32<<20, s.maxBytes*2)
	for len(s.refs) > s.maxEntries || s.bytes > totalBytes {
		oldest := ""
		var sequence uint64
		for key, ref := range s.refs {
			if oldest == "" || ref.sequence < sequence {
				oldest, sequence = key, ref.sequence
			}
		}
		s.remove(oldest)
	}
	return nil
}

func (s *contextStore) load(ctx context.Context, scope, ref string) ([]Mapping, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := continuationCacheKey(scope, ref)
	var data []byte
	_, ttl, maxBytes := s.limits()
	if common.RedisEnabled && common.RDB != nil {
		bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		encoded, err := common.RDB.Get(bounded, key).Bytes()
		if err != nil {
			return nil, ErrContinuationUnavailable
		}
		data, err = openContext(key, encoded)
		if err != nil {
			return nil, ErrContinuationUnavailable
		}
		_, err = common.RDB.TxPipelined(bounded, func(pipe redis.Pipeliner) error {
			pipe.Expire(bounded, key, ttl)
			pipe.ZAdd(bounded, "anonymization:v1:expiry", &redis.Z{Score: float64(s.now().UnixMilli()), Member: key})
			return nil
		})
		if err != nil {
			return nil, ErrContinuationUnavailable
		}
	} else {
		s.mu.Lock()
		s.purgeExpired()
		entry, ok := s.refs[key]
		if !ok {
			s.mu.Unlock()
			return nil, ErrContinuationUnavailable
		}
		s.sequence++
		entry.sequence = s.sequence
		entry.expires = s.now().Add(s.ttl)
		s.refs[key] = entry
		data = append([]byte(nil), s.snapshots[entry.snapshot].data...)
		s.mu.Unlock()
	}
	if len(data) > maxBytes {
		return nil, ErrContinuationUnavailable
	}
	var mappings []Mapping
	if err := json.Unmarshal(data, &mappings); err != nil {
		return nil, ErrContinuationUnavailable
	}
	return mappings, nil
}

func (s *contextStore) remove(key string) {
	ref, ok := s.refs[key]
	if !ok {
		return
	}
	delete(s.refs, key)
	if blob := s.snapshots[ref.snapshot]; blob != nil {
		blob.refs--
		if blob.refs == 0 {
			s.bytes -= len(blob.data)
			delete(s.snapshots, ref.snapshot)
		}
	}
}
func (s *contextStore) purgeExpired() {
	for key, ref := range s.refs {
		if !ref.expires.After(s.now()) {
			s.remove(key)
		}
	}
}

func contextCipher() (cipher.AEAD, error) {
	key := sha256.Sum256([]byte("NewRouter anonymization contexts v1\x00" + common.CryptoSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func sealContext(key string, data []byte) ([]byte, error) {
	aead, err := contextCipher()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, data, []byte(key)), nil
}
func openContext(key string, data []byte) ([]byte, error) {
	aead, err := contextCipher()
	if err != nil {
		return nil, err
	}
	if len(data) < aead.NonceSize() {
		return nil, ErrContinuationUnavailable
	}
	return aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], []byte(key))
}

func (s *contextStore) saveRedis(ctx context.Context, keys []string, data []byte) error {
	maxEntries, ttl, _ := s.limits()
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	const index = "anonymization:v1:expiry"
	_, err := common.RDB.TxPipelined(bounded, func(pipe redis.Pipeliner) error {
		for _, key := range keys {
			encoded, err := sealContext(key, data)
			if err != nil {
				return err
			}
			pipe.Set(bounded, key, encoded, ttl)
			pipe.ZAdd(bounded, index, &redis.Z{Score: float64(s.now().UnixMilli()), Member: key})
		}
		pipe.ZRemRangeByScore(bounded, index, "-inf", strconv.FormatInt(s.now().Add(-ttl).UnixMilli(), 10))
		return nil
	})
	if err != nil {
		return err
	}
	count, err := common.RDB.ZCard(bounded, index).Result()
	if err != nil {
		return err
	}
	if excess := count - int64(maxEntries); excess > 0 {
		stale, err := common.RDB.ZRange(bounded, index, 0, excess-1).Result()
		if err != nil {
			return err
		}
		if len(stale) > 0 {
			_, err = common.RDB.TxPipelined(bounded, func(pipe redis.Pipeliner) error {
				pipe.Del(bounded, stale...)
				values := make([]any, len(stale))
				for i, key := range stale {
					values[i] = key
				}
				pipe.ZRem(bounded, index, values...)
				return nil
			})
		}
	}
	return err
}

// ClearContextForDisabledChannel also prevents a retry through a channel with
// anonymization disabled from restoring a previous attempt's surrogate set.
func ClearContextForDisabledChannel(c *gin.Context) {
	if c != nil {
		c.Set(contextKey, (*Session)(nil))
		c.Set(continuationKey, (*continuationState)(nil))
	}
}
