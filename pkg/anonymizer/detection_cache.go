package anonymizer

import (
	"container/list"
	"crypto/sha256"
	"sync"
	"time"
	"unsafe"
)

// This cache contains only context-independent local detection results. It
// never holds input text, pseudonyms, vaults or external analyzer responses.
// Rule changes select a new fingerprint; binaries start with an empty cache.
type detectionCacheKey struct {
	scope [32]byte
	rules [32]byte
	text  [32]byte
	day   int64 // CN identity birth-date validation changes at UTC midnight
}

type cachedDetection struct {
	start, end int
	entityType string
	ruleName   string
}

type detectionCacheEntry struct {
	key     detectionCacheKey
	matches []cachedDetection
	expires time.Time
	bytes   int
}

type detectionCache struct {
	mu                          sync.Mutex
	entries                     map[detectionCacheKey]*list.Element
	lru                         list.List
	bytes, maxBytes, maxEntries int
	ttl                         time.Duration
}

var localDetectionCache = newDetectionCache(4096, 32<<20, 10*time.Minute)

func newDetectionCache(entries, bytes int, ttl time.Duration) *detectionCache {
	return &detectionCache{entries: make(map[detectionCacheKey]*list.Element), maxEntries: entries, maxBytes: bytes, ttl: ttl}
}

func (c *detectionCache) key(scope [32]byte, text string, rules *RuleSet, now time.Time) detectionCacheKey {
	k := detectionCacheKey{scope: scope, text: sha256.Sum256([]byte(text)), day: now.Unix() / 86400}
	if rules != nil {
		k.rules = rules.fingerprint
	}
	return k
}

func (c *detectionCache) get(key detectionCacheKey, text string, now time.Time) ([]Match, bool) {
	c.mu.Lock()
	e := c.entries[key]
	if e == nil {
		c.mu.Unlock()
		return nil, false
	}
	entry := e.Value.(*detectionCacheEntry)
	if !now.Before(entry.expires) {
		c.remove(e)
		c.mu.Unlock()
		return nil, false
	}
	c.lru.MoveToFront(e)
	stored := entry.matches // immutable even after eviction; hydrate outside lock
	c.mu.Unlock()
	matches := make([]Match, len(stored))
	for i, m := range stored {
		matches[i] = Match{Text: text[m.start:m.end], Start: m.start, End: m.end, EntityType: m.entityType, RuleName: m.ruleName}
	}
	return matches, true
}

func (c *detectionCache) put(key detectionCacheKey, matches []Match, now time.Time) {
	// Account for the entry, list node, map overhead and retained strings. Bound
	// a single positive result too, so dense inputs cannot evict the whole cache.
	bytes := int(unsafe.Sizeof(detectionCacheEntry{})) + 256 + len(matches)*int(unsafe.Sizeof(cachedDetection{}))
	for _, m := range matches {
		bytes += len(m.EntityType) + len(m.RuleName)
	}
	if bytes > 128<<10 || bytes > c.maxBytes || c.maxEntries <= 0 {
		return
	}
	stored := make([]cachedDetection, len(matches))
	for i, m := range matches {
		stored[i] = cachedDetection{m.Start, m.End, m.EntityType, m.RuleName}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.entries[key]; e != nil {
		c.remove(e)
	}
	for len(c.entries) >= c.maxEntries || c.bytes+bytes > c.maxBytes {
		c.remove(c.lru.Back())
	}
	e := &detectionCacheEntry{key: key, matches: stored, expires: now.Add(c.ttl), bytes: bytes}
	c.entries[key] = c.lru.PushFront(e)
	c.bytes += bytes
}

func (c *detectionCache) remove(e *list.Element) {
	entry := e.Value.(*detectionCacheEntry)
	delete(c.entries, entry.key)
	c.bytes -= entry.bytes
	c.lru.Remove(e)
}

func (s *Session) localMatches(text string) []Match {
	if text == "" || len(text) > 1<<20 {
		return DetectWithRules(text, s.rules)
	}
	now := time.Now()
	key := localDetectionCache.key(s.detectionScope, text, s.rules, now)
	if matches, ok := localDetectionCache.get(key, text, now); ok {
		return matches
	}
	matches := DetectWithRules(text, s.rules)
	localDetectionCache.put(key, matches, now)
	return matches
}
