package anonymizer

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"regexp/syntax"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	"github.com/stretchr/testify/require"
)

// Produce varied witnesses from the regex syntax, independent of gate logic.
func regexWitness(re *syntax.Regexp, rng *rand.Rand) string {
	switch re.Op {
	case syntax.OpLiteral:
		runes := append([]rune(nil), re.Rune...)
		for i, r := range runes {
			if re.Flags&syntax.FoldCase != 0 {
				for n := rng.Intn(4); n > 0; n-- {
					r = unicode.SimpleFold(r)
				}
				runes[i] = r
			}
		}
		return string(runes)
	case syntax.OpCharClass:
		i := 2 * rng.Intn(len(re.Rune)/2)
		return string(re.Rune[i] + rune(rng.Intn(int(re.Rune[i+1]-re.Rune[i]+1))))
	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return "x"
	case syntax.OpCapture:
		return regexWitness(re.Sub[0], rng)
	case syntax.OpConcat:
		var out strings.Builder
		for _, sub := range re.Sub {
			out.WriteString(regexWitness(sub, rng))
		}
		return out.String()
	case syntax.OpAlternate:
		return regexWitness(re.Sub[rng.Intn(len(re.Sub))], rng)
	case syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
		minimum, maximum := re.Min, re.Max
		switch re.Op {
		case syntax.OpStar:
			minimum, maximum = 0, 2
		case syntax.OpPlus:
			minimum, maximum = 1, 3
		case syntax.OpQuest:
			minimum, maximum = 0, 1
		}
		if maximum < 0 {
			maximum = minimum + 2
		}
		count := minimum + rng.Intn(min(3, maximum-minimum)+1)
		var out strings.Builder
		for i := 0; i < count; i++ {
			out.WriteString(regexWitness(re.Sub[0], rng))
		}
		return out.String()
	default:
		return ""
	}
}

func detectorCorpus(t testing.TB) []string {
	t.Helper()
	texts := []string{
		"", "plain text", "🙂姓名：张三 地址：上海市浦东新区世纪大道100号",
		"tokens: 13800138000 联系电话: 13800138000 身份证 11010519491231002X",
		"AWS_ſECRET_ACCESS_KEY=" + strings.Repeat("A", 40),
		"redis://user:password@private.test 4111 1111 1111 1111",
		"alice@example.test https://example.test/path 192.0.2.88/24",
		"\xff\xfe\x00alice@example.test\x80", strings.Repeat("alice@example.test ", 200),
		performanceText("plain", 10240), performanceText("unicode", 10240),
	}
	rng := rand.New(rand.NewSource(71))
	patterns := append(append([]patternSpec(nil), chinesePatterns...), detectorPatterns...)
	patterns = append(patterns, mustPattern("PAN", panPattern.String()))
	for i, spec := range patterns {
		re, err := syntax.Parse(spec.re.String(), syntax.Perl)
		require.NoError(t, err)
		found := 0
		for attempt := 0; attempt < 150 && found < 4; attempt++ {
			text := regexWitness(re, rng)
			if !spec.re.MatchString(text) {
				continue
			}
			found++
			texts = append(texts, text, "🙂 "+text+"\n")
			if i < len(chinesePatterns)+len(detectorPatterns) {
				require.True(t, spec.gate.possible(detectorPrefilter.inspect(text)), "gate rejected pattern %d witness %q", i, text)
			}
		}
		require.Positive(t, found, "no matching witness for pattern %d", i)
	}
	return texts
}

func TestDetectorOptimizationMatchesReference(t *testing.T) {
	for i, text := range detectorCorpus(t) {
		for _, reserved := range [][]span{nil, {{0, min(4, len(text))}}, {{2, min(8, len(text))}, {0, min(3, len(text))}}} {
			want := detectStructuredReference(text, reserved)
			got := detectStructured(text, reserved)
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("case %d reserved %v text %q\n got %#v\nwant %#v", i, reserved, text, got, want)
			}
		}
	}
}

func FuzzDetectorOptimization(f *testing.F) {
	for _, text := range []string{"alice@example.test", "🙂姓名：张三", "AWS_ſECRET_ACCESS_KEY=" + strings.Repeat("A", 40), "192.0.2.1 4111111111111111", "\xffplain\x00text"} {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 8192 {
			t.Skip()
		}
		if want, got := detectStructuredReference(text, nil), Detect(text); !reflect.DeepEqual(want, got) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})
}

func TestDetectionCacheIsolationVersionsAndExpiry(t *testing.T) {
	c := newDetectionCache(2, 4096, time.Minute)
	now := time.Unix(100000, 0)
	text := "alice@example.test"
	scope := sha256.Sum256([]byte("user:1:token:1"))
	k := c.key(scope, text, nil, now)
	want := DetectWithRules(text, nil)
	c.put(k, want, now)
	got, ok := c.get(k, text, now)
	require.True(t, ok)
	require.Equal(t, want, got)
	got[0].EntityType = "mutated"
	got, ok = c.get(k, text, now)
	require.True(t, ok)
	require.Equal(t, want, got, "returned slices must be independent")
	other := c.key(sha256.Sum256([]byte("user:1:token:2")), text, nil, now)
	_, ok = c.get(other, text, now)
	require.False(t, ok)
	rules, err := CompileRules([]Rule{{Name: "custom", Mode: "literal", Pattern: "alice", Enabled: true}})
	require.NoError(t, err)
	_, ok = c.get(c.key(scope, text, rules, now), text, now)
	require.False(t, ok)
	_, ok = c.get(c.key(scope, text, nil, now.Add(24*time.Hour)), text, now)
	require.False(t, ok, "date-sensitive validators must be refreshed")
	_, ok = c.get(k, text, now.Add(time.Minute))
	require.False(t, ok)
	require.Empty(t, c.entries)
	for i := 0; i < 3; i++ {
		c.put(c.key(scope, fmt.Sprint(i), nil, now), nil, now)
	}
	require.Len(t, c.entries, 2)
	_, ok = c.get(c.key(scope, "2", nil, now), "2", now)
	require.True(t, ok, "negative results must be cached")
	require.LessOrEqual(t, c.bytes, c.maxBytes)
	// Byte limits apply independently of entry limits.
	bounded := newDetectionCache(100, 512, time.Minute)
	for i := 0; i < 10; i++ {
		bounded.put(c.key(scope, fmt.Sprint(i), nil, now), nil, now)
	}
	require.LessOrEqual(t, bounded.bytes, 512)
	require.Less(t, len(bounded.entries), 10)
}

func TestDetectionCacheConcurrentAccess(t *testing.T) {
	c := newDetectionCache(32, 16<<10, time.Minute)
	var wg sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				text := fmt.Sprintf("worker %d case %d", worker, i%8)
				now := time.Now()
				key := c.key([32]byte{}, text, nil, now)
				c.put(key, nil, now)
				c.get(key, text, now)
			}
		}(worker)
	}
	wg.Wait()
	require.LessOrEqual(t, len(c.entries), c.maxEntries)
	require.LessOrEqual(t, c.bytes, c.maxBytes)
}

type changingDetector struct{ calls int }

func (d *changingDetector) Detect(_ context.Context, text string) ([]Match, error) {
	d.calls++
	return []Match{{Text: text[:5], Start: 0, End: 5, EntityType: "PERSON"}}, nil
}

type historyTestDetector struct {
	fingerprint [32]byte
	calls       atomic.Int32
	find        func(context.Context, string) ([]Match, error)
}

// Fingerprint identifies the model configuration used by history tests.
func (d *historyTestDetector) Fingerprint() [32]byte { return d.fingerprint }

// Detect counts inference calls independently of the shared span cache.
func (d *historyTestDetector) Detect(ctx context.Context, text string) ([]Match, error) {
	d.calls.Add(1)
	return d.find(ctx, text)
}

func TestDetectionHistoryIsStableAndScoped(t *testing.T) {
	previous := historyDetectionCache
	historyDetectionCache = newDetectionCache(64, 1<<20, time.Minute)
	t.Cleanup(func() { historyDetectionCache = previous })
	const text = "alice met boris"
	start := 0
	detector := &historyTestDetector{fingerprint: sha256.Sum256([]byte("model-v1"))}
	detector.find = func(_ context.Context, text string) ([]Match, error) {
		return []Match{{Text: text[start : start+5], Start: start, End: start + 5, EntityType: "PERSON"}}, nil
	}
	makeSession := func(tenant, route string, stable bool) *Session {
		return NewSession(tenant, nil).WithStableSurrogates(stable).
			WithDetector(context.Background(), detector).WithDetectionHistory(route)
	}
	first := makeSession("tenant-one", "channel-one:model-one", true)
	masked := first.AnonymizeString(text)
	require.NoError(t, first.Err())
	require.NotContains(t, masked, "alice")
	start = 10
	replay := makeSession("tenant-one", "channel-one:model-one", true)
	require.Equal(t, masked, replay.AnonymizeString(text))
	require.Equal(t, text, replay.DeanonymizeString(masked))
	require.EqualValues(t, 1, detector.calls.Load())
	for _, scope := range []struct{ tenant, route string }{
		{"tenant-two", "channel-one:model-one"},
		{"tenant-one", "channel-two:model-one"},
		{"tenant-one", "channel-one:model-two"},
	} {
		session := makeSession(scope.tenant, scope.route, true)
		output := session.AnonymizeString(text)
		require.NoError(t, session.Err())
		require.Contains(t, output, "alice")
		require.NotContains(t, output, "boris")
	}
	require.EqualValues(t, 4, detector.calls.Load())
	detector.fingerprint = sha256.Sum256([]byte("model-v2"))
	changedModel := makeSession("tenant-one", "channel-one:model-one", true)
	require.NotContains(t, changedModel.AnonymizeString(text), "boris")
	require.EqualValues(t, 5, detector.calls.Load())
	rules, err := CompileRules([]Rule{{Name: "verb", Mode: "literal", Pattern: "met", Enabled: true}})
	require.NoError(t, err)
	changedRules := makeSession("tenant-one", "channel-one:model-one", true).WithRules(rules)
	require.NotContains(t, changedRules.AnonymizeString(text), "met")
	require.EqualValues(t, 6, detector.calls.Load())
	for range 2 {
		random := makeSession("tenant-one", "channel-one:model-one", false)
		random.AnonymizeString(text)
		require.NoError(t, random.Err())
	}
	require.EqualValues(t, 8, detector.calls.Load(), "random aliases must keep fresh detector behavior")
	for _, element := range historyDetectionCache.entries {
		entry := element.Value.(*detectionCacheEntry)
		for _, span := range entry.matches {
			require.Equal(t, "PERSON", span.entityType)
			require.Empty(t, span.ruleName)
		}
	}
}

func TestDetectionHistoryRejectsFailuresAndDeduplicatesConcurrentInference(t *testing.T) {
	previous := historyDetectionCache
	historyDetectionCache = newDetectionCache(64, 1<<20, time.Minute)
	t.Cleanup(func() { historyDetectionCache = previous })
	detector := &historyTestDetector{fingerprint: sha256.Sum256([]byte("model"))}
	detector.find = func(context.Context, string) ([]Match, error) { return nil, errors.New("unavailable") }
	makeSession := func() *Session {
		return NewSession("history-concurrency", nil).WithStableSurrogates(true).
			WithDetector(context.Background(), detector).WithDetectionHistory("route")
	}
	failed := makeSession()
	failed.AnonymizeString("alice")
	require.Error(t, failed.Err())
	require.Empty(t, historyDetectionCache.entries)
	detector.find = func(context.Context, string) ([]Match, error) {
		return []Match{{Text: "wrong", Start: 0, End: 5, EntityType: "PERSON"}}, nil
	}
	invalid := makeSession()
	invalid.AnonymizeString("alice")
	require.Error(t, invalid.Err())
	require.Empty(t, historyDetectionCache.entries)
	started := make(chan struct{})
	finish := make(chan struct{})
	detector.find = func(ctx context.Context, text string) ([]Match, error) {
		close(started)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-finish:
			return []Match{{Text: text, Start: 0, End: len(text), EntityType: "PERSON"}}, nil
		}
	}
	outputs := make(chan string, 8)
	errors := make(chan error, 8)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			session := makeSession()
			outputs <- session.AnonymizeString("alice")
			errors <- session.Err()
		})
	}
	<-started
	close(finish)
	workers.Wait()
	var expected string
	for range 8 {
		require.NoError(t, <-errors)
		output := <-outputs
		if expected == "" {
			expected = output
		}
		require.Equal(t, expected, output)
	}
	require.EqualValues(t, 3, detector.calls.Load())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	canceled := makeSession().WithDetector(ctx, detector)
	canceled.AnonymizeString("alice")
	require.Error(t, canceled.Err(), "even a cached result must honor cancellation")
}

func TestHistoryDetectionCachePreservesRequestSemantics(t *testing.T) {
	previous := localDetectionCache
	localDetectionCache = newDetectionCache(64, 1<<20, time.Minute)
	t.Cleanup(func() { localDetectionCache = previous })
	text := "alice@example.test and ordinary document"
	first := NewSession("cache-semantics", nil)
	a := first.AnonymizeString(text)
	second := NewSession("cache-semantics", nil)
	b := second.AnonymizeString(text)
	require.NotEqual(t, a, b, "cached detection must still create fresh random aliases")
	require.Equal(t, text, second.DeanonymizeString(b))
	require.EqualValues(t, 1, second.ReplacementCount())
	require.Len(t, localDetectionCache.entries, 1)
	rules, err := CompileRules([]Rule{{Name: "word", Mode: "literal", Pattern: "ordinary", Enabled: true}})
	require.NoError(t, err)
	third := NewSession("cache-semantics", nil).WithRules(rules)
	require.NotContains(t, third.AnonymizeString(text), "ordinary")
	require.Len(t, localDetectionCache.entries, 2)
	detector := &changingDetector{}
	for i := 0; i < 2; i++ {
		s := NewSession("cache-external", nil).WithDetector(context.Background(), detector)
		s.AnonymizeString("alice works here")
	}
	require.Equal(t, 2, detector.calls, "cross-request local cache must not reuse external NER responses")
	// Even a cached negative result must recognize a literal imported alias in
	// authoritative raw input. That recognition depends on this request's vault.
	const alias = "ordinary-token-value"
	warm := NewSession("cache-import", nil)
	require.Equal(t, alias, warm.AnonymizeString(alias))
	replay := NewSession("cache-import", nil)
	require.NoError(t, replay.Import([]Mapping{{Original: "previous-private-value", Surrogate: alias, EntityType: "TOKEN"}}))
	raw := []byte(`{"messages":[{"role":"user","content":"ordinary-token-value"}]}`)
	masked, err := AnonymizeClaudeRequest(raw, replay)
	require.NoError(t, err)
	require.NotContains(t, string(masked), alias)
	restored, err := DeanonymizeTextFields(masked, replay)
	require.NoError(t, err)
	require.JSONEq(t, string(raw), string(restored))
	require.EqualValues(t, 1, replay.ReplacementCount())
}

func referenceHoldLen(surrogates []string, buf string) int {
	maxLen := 0
	for _, sg := range surrogates {
		maxLen = max(maxLen, len(sg))
	}
	for pos := max(0, len(buf)-maxLen+1); pos < len(buf); pos++ {
		for _, sg := range surrogates {
			if len(sg) <= len(buf)-pos || !strings.HasPrefix(sg, buf[pos:]) {
				continue
			}
			splits := false
			for _, full := range surrogates {
				start, end := max(0, pos-len(full)+1), min(len(buf), pos+len(full)-1)
				if end-start >= len(full) && strings.Contains(buf[start:end], full) {
					splits = true
					break
				}
			}
			if !splits {
				return len(buf) - pos
			}
		}
	}
	return 0
}

func TestStreamTrieAndReplacerMatchReference(t *testing.T) {
	rng := rand.New(rand.NewSource(53))
	for trial := 0; trial < 150; trial++ {
		v := NewVault()
		seen := make(map[string]bool)
		for i := 0; i < 12; i++ {
			var word strings.Builder
			for n := rng.Intn(7) + 1; n > 0; n-- {
				word.WriteByte("abct-"[rng.Intn(5)])
			}
			if seen[word.String()] {
				continue
			}
			seen[word.String()] = true
			v.byEngage["trie"] = append(v.byEngage["trie"], Mapping{Original: fmt.Sprintf("private-%d", i), Surrogate: word.String()})
		}
		s := NewSession("trie", v)
		r := s.NewStreamRestorer()
		require.LessOrEqual(t, len(r.trie), 2*len(r.surrogates)+1, "compressed trie size must depend on mapping count")
		for i := 0; i < 30; i++ {
			buf := r.surrogates[rng.Intn(len(r.surrogates))] + " " + r.surrogates[rng.Intn(len(r.surrogates))]
			buf = buf[:rng.Intn(len(buf)+1)]
			require.Equal(t, referenceHoldLen(r.surrogates, buf), r.holdLen(buf), "trial %d buf %q", trial, buf)
			require.Equal(t, s.DeanonymizeString(buf), r.restore(buf))
		}
	}
}

func TestStreamTrieCompressesLongAliases(t *testing.T) {
	prefix := strings.Repeat("a", 64<<10)
	v := NewVault()
	v.byEngage["long"] = []Mapping{{Original: "first", Surrogate: prefix + "x"}, {Original: "second", Surrogate: prefix + "y"}}
	r := NewSession("long", v).NewStreamRestorer()
	require.LessOrEqual(t, len(r.trie), 5)
	require.Equal(t, len(prefix), r.holdLen(prefix))
	require.Equal(t, "first", r.restore(prefix+"x"))
}

func FuzzStreamOptimization(f *testing.F) {
	f.Add("abc", "abcd", "abcd abc ab")
	f.Add("token-t", "test", "token-ttest to")
	f.Fuzz(func(t *testing.T, a, b, text string) {
		if a == "" || b == "" || a == b || len(a) > 64 || len(b) > 64 || len(text) > 512 {
			t.Skip()
		}
		v := NewVault()
		v.byEngage["fuzz"] = []Mapping{{Original: "original-a", Surrogate: a}, {Original: "original-b", Surrogate: b}}
		s := NewSession("fuzz", v)
		r := s.NewStreamRestorer()
		if got, want := r.holdLen(text), referenceHoldLen(r.surrogates, text); got != want {
			t.Fatalf("hold %d, want %d", got, want)
		}
		if got, want := r.restore(text), s.DeanonymizeString(text); got != want {
			t.Fatalf("restore %q, want %q", got, want)
		}
	})
}
