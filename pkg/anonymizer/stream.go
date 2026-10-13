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
	session    *Session
	surrogates []string // snapshot of this engagement's surrogates
	maxLen     int      // longest surrogate length
	carry      map[string]string
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
	for _, m := range mappings {
		if m.Surrogate == "" {
			continue
		}
		surrogates = append(surrogates, m.Surrogate)
		if len(m.Surrogate) > maxLen {
			maxLen = len(m.Surrogate)
		}
	}
	return &StreamRestorer{
		session:    s,
		surrogates: surrogates,
		maxLen:     maxLen,
		carry:      make(map[string]string),
	}
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
	return r.session.DeanonymizeString(head)
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
	return r.session.DeanonymizeString(tail)
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
	// A held suffix is a strict prefix of a surrogate, so its length is at most
	// maxLen-1; scanning positions from the earliest gives the longest suffix first.
	start := 0
	if limit := r.maxLen - 1; limit >= 0 && len(buf) > limit {
		start = len(buf) - limit
	}
	for pos := start; pos < len(buf); pos++ {
		suffix := buf[pos:]
		for _, sg := range r.surrogates {
			if len(sg) > len(suffix) && strings.HasPrefix(sg, suffix) {
				// A full surrogate can itself end in one of these prefixes.
				// Holding only that suffix would split the complete match, so
				// neither this chunk nor Flush could restore it afterward.
				if r.splitsCompleteSurrogate(buf, pos) {
					continue
				}
				return len(buf) - pos
			}
		}
	}
	return 0
}

func (r *StreamRestorer) splitsCompleteSurrogate(buf string, boundary int) bool {
	for _, surrogate := range r.surrogates {
		// Every full match contained in this window crosses the boundary.
		// Bound the search to the surrounding surrogate length so large
		// response chunks do not require rescanning all preceding content.
		start := max(0, boundary-len(surrogate)+1)
		end := min(len(buf), boundary+len(surrogate)-1)
		if end-start >= len(surrogate) && strings.Contains(buf[start:end], surrogate) {
			return true
		}
	}
	return false
}
