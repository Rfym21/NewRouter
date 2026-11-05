package anonymizer

import "sort"

// spanIndex stores a sorted union. Buffered additions are non-overlapping and
// ordered within a regex FindAll pass, so they can be merged once per rule.
type spanIndex struct {
	spans []span
	added []span
}

func newSpanIndex(existing []span) spanIndex {
	s := spanIndex{added: append([]span(nil), existing...)}
	sort.Slice(s.added, func(i, j int) bool { return s.added[i].start < s.added[j].start })
	s.merge()
	return s
}

func (s *spanIndex) overlaps(start, end int) bool {
	i := sort.Search(len(s.spans), func(i int) bool { return s.spans[i].end > start })
	if i < len(s.spans) && s.spans[i].start < end {
		return true
	}
	// Regex candidates arrive in order; accepted candidates in this pass can
	// only overlap the last addition (including narrowed capture groups).
	return len(s.added) > 0 && s.added[len(s.added)-1].end > start && s.added[len(s.added)-1].start < end
}

func (s *spanIndex) add(start, end int) { s.added = append(s.added, span{start, end}) }

func (s *spanIndex) merge() {
	if len(s.added) == 0 {
		return
	}
	merged := make([]span, 0, len(s.spans)+len(s.added))
	i, j := 0, 0
	for i < len(s.spans) || j < len(s.added) {
		var next span
		if j == len(s.added) || i < len(s.spans) && s.spans[i].start <= s.added[j].start {
			next = s.spans[i]
			i++
		} else {
			next = s.added[j]
			j++
		}
		if next.end <= next.start {
			continue
		}
		if len(merged) > 0 && merged[len(merged)-1].end >= next.start {
			merged[len(merged)-1].end = max(merged[len(merged)-1].end, next.end)
		} else {
			merged = append(merged, next)
		}
	}
	s.spans, s.added = merged, s.added[:0]
}
