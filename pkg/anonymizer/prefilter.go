package anonymizer

import (
	"regexp/syntax"
	"strings"
	"unicode/utf8"
)

// Gates are necessary conditions only. Matching still uses the original regex
// against the original text, including its anchors, captures and UTF-8 offsets.
type patternGate struct {
	chars   [2]uint64
	runs    [3]int // decimal, hexadecimal, ASCII word
	minLen  int
	allIn   [3]bool
	literal string
	word    int
	bit     uint64
}

type textFeatures struct {
	chars [2]uint64
	runs  [3]int
	words [2]uint64
	size  int
}

func canonicalASCII(r rune) (byte, bool) {
	// These are the only non-ASCII runes in ASCII Unicode simple-fold cycles.
	// RE2 (?i) can match them in an otherwise ASCII literal.
	if r == '\u212a' {
		r = 'k'
	}
	if r == '\u017f' {
		r = 's'
	}
	if r < 0 || r >= 128 {
		return 0, false
	}
	if r >= 'A' && r <= 'Z' {
		r += 'a' - 'A'
	}
	return byte(r), true
}

func runClasses(c byte) [3]bool {
	digit := c >= '0' && c <= '9'
	hex := digit || c >= 'a' && c <= 'f'
	return [3]bool{digit, hex, digit || c >= 'a' && c <= 'z' || c == '_'}
}

func compilePatternGate(expr string) patternGate {
	re, err := syntax.Parse(expr, syntax.Perl)
	if err != nil {
		return patternGate{}
	}
	return gateExpression(re)
}

func gateExpression(re *syntax.Regexp) patternGate {
	g := patternGate{allIn: [3]bool{true, true, true}}
	switch re.Op {
	case syntax.OpEmptyMatch, syntax.OpBeginLine, syntax.OpEndLine, syntax.OpBeginText, syntax.OpEndText, syntax.OpWordBoundary, syntax.OpNoWordBoundary:
	case syntax.OpCapture:
		return gateExpression(re.Sub[0])
	case syntax.OpLiteral:
		g.minLen = len(re.Rune) // conservative byte lower bound, including invalid UTF-8
		var token strings.Builder
		var streak [3]int
		for _, r := range re.Rune {
			c, ok := canonicalASCII(r)
			if !ok {
				if token.Len() > len(g.literal) {
					g.literal = token.String()
				}
				token = strings.Builder{}
				g.allIn = [3]bool{}
				streak = [3]int{}
				continue
			}
			g.chars[c/64] |= uint64(1) << (c % 64)
			token.WriteByte(c)
			for j, in := range runClasses(c) {
				g.allIn[j] = g.allIn[j] && in
				if in {
					streak[j]++
				} else {
					streak[j] = 0
				}
				g.runs[j] = max(g.runs[j], streak[j])
			}
		}
		if token.Len() > len(g.literal) {
			g.literal = token.String()
		}
	case syntax.OpCharClass:
		g.minLen = 1
		for i := 0; i < len(re.Rune); i += 2 {
			if re.Rune[i+1] >= 128 {
				g.allIn = [3]bool{}
				break
			}
			for r := re.Rune[i]; r <= re.Rune[i+1]; r++ {
				c, _ := canonicalASCII(r)
				for j, in := range runClasses(c) {
					g.allIn[j] = g.allIn[j] && in
				}
			}
		}
		for j, in := range g.allIn {
			if in {
				g.runs[j] = 1
			}
		}
	case syntax.OpConcat:
		var streak [3]int
		for _, sub := range re.Sub {
			s := gateExpression(sub)
			g.minLen += s.minLen
			for j := range g.chars {
				g.chars[j] |= s.chars[j]
			}
			if len(s.literal) > len(g.literal) {
				g.literal = s.literal
			}
			for j := range g.runs {
				g.allIn[j] = g.allIn[j] && s.allIn[j]
				g.runs[j] = max(g.runs[j], s.runs[j])
				if s.allIn[j] {
					streak[j] += s.minLen
				} else {
					streak[j] = 0
				}
				g.runs[j] = max(g.runs[j], streak[j])
			}
		}
	case syntax.OpAlternate:
		g = gateExpression(re.Sub[0])
		for _, sub := range re.Sub[1:] {
			s := gateExpression(sub)
			g.minLen = min(g.minLen, s.minLen)
			for j := range g.chars {
				g.chars[j] &= s.chars[j]
			}
			if g.literal != s.literal {
				g.literal = ""
			}
			for j := range g.runs {
				g.runs[j] = min(g.runs[j], s.runs[j])
				g.allIn[j] = g.allIn[j] && s.allIn[j]
			}
		}
	case syntax.OpStar, syntax.OpQuest, syntax.OpPlus, syntax.OpRepeat:
		g = gateExpression(re.Sub[0])
		minimum := re.Min
		if re.Op == syntax.OpPlus {
			minimum = 1
		}
		if re.Op == syntax.OpStar || re.Op == syntax.OpQuest {
			minimum = 0
		}
		g.minLen *= minimum
		if minimum == 0 {
			g.chars = [2]uint64{}
			g.runs = [3]int{}
			g.literal = ""
		}
		for j, in := range g.allIn {
			if in {
				g.runs[j] = g.minLen
			}
		}
	default:
		g.allIn = [3]bool{}
	}
	return g
}

func (g patternGate) possible(f textFeatures) bool {
	if f.size < g.minLen {
		return false
	}
	for i := range g.chars {
		if f.chars[i]&g.chars[i] != g.chars[i] {
			return false
		}
	}
	for i := range g.runs {
		if f.runs[i] < g.runs[i] {
			return false
		}
	}
	return g.bit == 0 || f.words[g.word]&g.bit != 0
}

type prefilterNode struct {
	next  [128]int32
	fail  int32
	words [2]uint64
}

// One DFA pass locates every required literal, rather than scanning the text
// once with Contains for each pattern. Extra patterns beyond the bitset limit
// simply retain character/run gates and always fall back to their regex.
type patternPrefilter struct{ nodes []prefilterNode }

var detectorPrefilter = newPatternPrefilter()
var panGate = compilePatternGate(panPattern.String())

func newPatternPrefilter() patternPrefilter {
	p := patternPrefilter{nodes: make([]prefilterNode, 1)}
	tokens := make(map[string]int)
	for _, patterns := range [][]patternSpec{chinesePatterns, detectorPatterns} {
		for i := range patterns {
			g := &patterns[i].gate
			if len(g.literal) < 2 {
				continue
			}
			id, ok := tokens[g.literal]
			if !ok {
				id = len(tokens)
				if id >= 128 {
					continue
				}
				tokens[g.literal] = id
				state := int32(0)
				for j := 0; j < len(g.literal); j++ {
					c := g.literal[j]
					if p.nodes[state].next[c] == 0 {
						p.nodes[state].next[c] = int32(len(p.nodes))
						p.nodes = append(p.nodes, prefilterNode{})
					}
					state = p.nodes[state].next[c]
				}
				p.nodes[state].words[id/64] |= uint64(1) << (id % 64)
			}
			g.word, g.bit = id/64, uint64(1)<<(id%64)
		}
	}
	queue := make([]int32, 0, len(p.nodes))
	for _, n := range p.nodes[0].next {
		if n != 0 {
			queue = append(queue, n)
		}
	}
	for head := 0; head < len(queue); head++ {
		state := queue[head]
		failure := p.nodes[state].fail
		for j := range p.nodes[state].words {
			p.nodes[state].words[j] |= p.nodes[failure].words[j]
		}
		for c := range p.nodes[state].next {
			n := p.nodes[state].next[c]
			if n == 0 {
				p.nodes[state].next[c] = p.nodes[failure].next[c]
			} else {
				p.nodes[n].fail = p.nodes[failure].next[c]
				queue = append(queue, n)
			}
		}
	}
	return p
}

func (p *patternPrefilter) inspect(text string) textFeatures {
	f := textFeatures{size: len(text)}
	var runs [3]int
	state := int32(0)
	for i := 0; i < len(text); {
		r := rune(text[i])
		size := 1
		if r >= 128 {
			r, size = utf8.DecodeRuneInString(text[i:])
		}
		i += size
		c, ok := canonicalASCII(r)
		if !ok {
			state = 0
			runs = [3]int{}
			continue
		}
		f.chars[c/64] |= uint64(1) << (c % 64)
		for j, in := range runClasses(c) {
			if in {
				runs[j]++
			} else {
				runs[j] = 0
			}
			f.runs[j] = max(f.runs[j], runs[j])
		}
		state = p.nodes[state].next[c]
		for j := range f.words {
			f.words[j] |= p.nodes[state].words[j]
		}
	}
	return f
}
