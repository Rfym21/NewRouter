package anonymizer

import "strings"

// StreamRestorer performs cross-chunk deanonymization for a single streaming
// response. A surrogate can be split across SSE chunks (models emit it a token
// at a time), so a stateless per-chunk restore never sees the whole surrogate
// and leaves it un-restored. StreamRestorer buffers a short tail per logical
// text stream until enough bytes arrive to decide whether they complete a
// surrogate, then restores and emits only the safe prefix.
//
// It is NOT safe for concurrent use: each streaming response must drive it from
// a single goroutine. The relay stream handlers already run their per-chunk
// callback serially (see helper.StreamScannerHandler), so this holds.
type StreamRestorer struct {
	session        *Session
	surrogates     []string // snapshot of this engagement's surrogates
	replacer       *strings.Replacer
	trie           []surrogateNode
	starts         [4]uint64
	singleOriginal string
	maxLen         int // longest surrogate length
	carry          map[string]string
	jsonStreams    map[string]*jsonStreamState
}

// NewStreamRestorer snapshots the session's current surrogate set for streaming
// restoration. It returns nil when s is nil, so callers can gate the streaming
// path on a non-nil result while Push/Flush/PendingKeys remain nil-safe.
//
// The snapshot is stable: surrogate mappings are only ever created while the
// request is being anonymized, never during response streaming.
func (s *Session) NewStreamRestorer() *StreamRestorer {
	if s == nil {
		return nil
	}
	mappings := s.vault.Mappings(s.engagement)
	surrogates := make([]string, 0, len(mappings))
	maxLen := 0
	pairs := make([]string, 0, len(mappings)*2)
	for _, m := range mappings {
		if m.Surrogate == "" {
			continue
		}
		surrogates = append(surrogates, m.Surrogate)
		pairs = append(pairs, m.Surrogate, m.Original)
		if len(m.Surrogate) > maxLen {
			maxLen = len(m.Surrogate)
		}
	}
	r := &StreamRestorer{
		session:    s,
		surrogates: surrogates,
		maxLen:     maxLen,
		carry:      make(map[string]string),
	}
	if len(pairs) > 0 {
		r.replacer = strings.NewReplacer(pairs...)
	}
	if len(pairs) == 2 {
		r.singleOriginal = pairs[1]
	}
	r.buildTrie()
	return r
}

// Push appends increment to the buffer for the given logical-stream key and
// returns the portion that is safe to emit now, fully deanonymized. Trailing
// bytes that could still be the start of a surrogate are held until the next
// Push or Flush. A nil receiver returns increment unchanged (no buffering), so
// callers that disable the feature see byte-identical passthrough.
func (r *StreamRestorer) Push(key string, increment string) string {
	if r == nil {
		return increment
	}
	buf := r.carry[key] + increment
	hold := r.holdLen(buf)
	if hold == 0 {
		delete(r.carry, key)
	} else {
		r.carry[key] = buf[len(buf)-hold:]
	}
	head := buf[:len(buf)-hold]
	if head == "" {
		return ""
	}
	return r.restore(head)
}

// Flush returns the deanonymized residual held for key and clears it. A tail
// that never completed a surrogate is emitted as-is; it is a surrogate fragment,
// never the real sensitive value, so this leaks nothing. A nil receiver returns "".
func (r *StreamRestorer) Flush(key string) string {
	if r == nil {
		return ""
	}
	tail := r.carry[key]
	if tail == "" {
		return ""
	}
	delete(r.carry, key)
	return r.restore(tail)
}

func (r *StreamRestorer) restore(text string) string {
	if r.replacer == nil {
		return text
	}
	if len(r.surrogates) == 1 {
		return strings.ReplaceAll(text, r.surrogates[0], r.singleOriginal)
	}
	// The generic Replacer allocates even when no key matches. Reuse the trie
	// to recognize that common case without scanning every mapping.
	if !r.containsSurrogate(text) {
		return text
	}
	return r.replacer.Replace(text)
}

func (r *StreamRestorer) containsSurrogate(text string) bool {
	for start := 0; start < len(text); start++ {
		c := text[start]
		if r.starts[c/64]&(uint64(1)<<(c%64)) == 0 {
			continue
		}
		if r.matchSurrogate(text[start:], 0) {
			return true
		}
	}
	return false
}

type surrogateNode struct {
	edges    []surrogateEdge
	terminal bool
}

type surrogateEdge struct {
	label string
	child int
}

func (r *StreamRestorer) edge(state int, first byte) (surrogateEdge, int, bool) {
	for i, edge := range r.trie[state].edges {
		if edge.label[0] == first {
			return edge, i, true
		}
	}
	return surrogateEdge{}, 0, false
}

func (r *StreamRestorer) buildTrie() {
	r.trie = make([]surrogateNode, 1)
	for _, surrogate := range r.surrogates {
		r.starts[surrogate[0]/64] |= uint64(1) << (surrogate[0] % 64)
		state, remaining := 0, surrogate
		for len(remaining) > 0 {
			edge, index, ok := r.edge(state, remaining[0])
			if !ok {
				child := len(r.trie)
				r.trie = append(r.trie, surrogateNode{terminal: true})
				r.trie[state].edges = append(r.trie[state].edges, surrogateEdge{remaining, child})
				break
			}
			n := 0
			for n < len(remaining) && n < len(edge.label) && remaining[n] == edge.label[n] {
				n++
			}
			remaining = remaining[n:]
			if n == len(edge.label) {
				state = edge.child
				if remaining == "" {
					r.trie[state].terminal = true
				}
				continue
			}
			// Compressed edges keep the node count proportional to mappings,
			// including imported aliases and very long HASH values.
			branch := len(r.trie)
			r.trie = append(r.trie, surrogateNode{edges: []surrogateEdge{{edge.label[n:], edge.child}}, terminal: remaining == ""})
			r.trie[state].edges[index] = surrogateEdge{edge.label[:n], branch}
			if remaining != "" {
				child := len(r.trie)
				r.trie = append(r.trie, surrogateNode{terminal: true})
				r.trie[branch].edges = append(r.trie[branch].edges, surrogateEdge{remaining, child})
			}
			break
		}
	}
}

// matchSurrogate recognizes a complete alias ending at or beyond minEnd.
func (r *StreamRestorer) matchSurrogate(text string, minEnd int) bool {
	state, offset := 0, 0
	for offset < len(text) {
		edge, _, ok := r.edge(state, text[offset])
		if !ok || !strings.HasPrefix(text[offset:], edge.label) {
			return false
		}
		offset += len(edge.label)
		state = edge.child
		if r.trie[state].terminal && offset >= minEnd {
			return true
		}
	}
	return false
}

func (r *StreamRestorer) strictSurrogatePrefix(text string) bool {
	state := 0
	for len(text) > 0 {
		edge, _, ok := r.edge(state, text[0])
		if !ok {
			return false
		}
		if len(text) <= len(edge.label) {
			return strings.HasPrefix(edge.label, text) && (len(text) < len(edge.label) || len(r.trie[edge.child].edges) > 0)
		}
		if !strings.HasPrefix(text, edge.label) {
			return false
		}
		text, state = text[len(edge.label):], edge.child
	}
	return false
}

// PendingKeys returns the keys that still hold a non-empty tail, so a handler
// can flush residuals at end-of-stream. A nil receiver returns nil.
func (r *StreamRestorer) PendingKeys() []string {
	if r == nil || len(r.carry) == 0 {
		return nil
	}
	keys := make([]string, 0, len(r.carry))
	for k, v := range r.carry {
		if v != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

// holdLen returns the length of the longest suffix of buf that is a strict
// prefix of some surrogate — i.e. bytes that could still grow into a full
// surrogate as more chunks arrive and therefore must not be emitted yet. The
// check runs on the raw (still-anonymized) buffer so a shorter surrogate that
// is a prefix of a longer one (e.g. "10.0.0.4" vs "10.0.0.42") is not restored
// prematurely. Surrogates are ASCII, so the returned boundary is rune-safe.
func (r *StreamRestorer) holdLen(buf string) int {
	if len(r.surrogates) == 0 || len(buf) == 0 {
		return 0
	}
	if len(r.surrogates) == 1 {
		sg := r.surrogates[0]
		for pos := max(0, len(buf)-len(sg)+1); pos < len(buf); pos++ {
			if !strings.HasPrefix(sg, buf[pos:]) {
				continue
			}
			start, end := max(0, pos-len(sg)+1), min(len(buf), pos+len(sg)-1)
			if end-start >= len(sg) && strings.Contains(buf[start:end], sg) {
				continue
			}
			return len(buf) - pos
		}
		return 0
	}
	// A held suffix is a strict prefix of a surrogate, so its length is at most
	// maxLen-1; scanning positions from the earliest gives the longest suffix first.
	start := 0
	if limit := r.maxLen - 1; limit >= 0 && len(buf) > limit {
		start = len(buf) - limit
	}
	for pos := start; pos < len(buf); pos++ {
		if r.starts[buf[pos]/64]&(uint64(1)<<(buf[pos]%64)) == 0 {
			continue
		}
		if r.strictSurrogatePrefix(buf[pos:]) {
			// A full surrogate can itself end in one of these prefixes.
			// Holding only that suffix would split the complete match, so
			// neither this chunk nor Flush could restore it afterward.
			if r.splitsCompleteSurrogate(buf, pos) {
				continue
			}
			return len(buf) - pos
		}
	}
	return 0
}

func (r *StreamRestorer) splitsCompleteSurrogate(buf string, boundary int) bool {
	for start := max(0, boundary-r.maxLen+1); start < boundary; start++ {
		if r.matchSurrogate(buf[start:], boundary-start+1) {
			return true
		}
	}
	return false
}
