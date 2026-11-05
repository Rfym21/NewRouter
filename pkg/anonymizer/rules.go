package anonymizer

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"regexp/syntax"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxCustomRules = 128
const MaxRulePatternBytes = 4096

// Rule is a literal or RE2 matcher. CaptureGroup selects the sensitive value
// inside a contextual regex; group zero replaces its whole match.
type Rule struct {
	Name         string `json:"name"`
	Mode         string `json:"mode"`
	Pattern      string `json:"pattern"`
	EntityType   string `json:"entity_type,omitempty"`
	CaptureGroup int    `json:"capture_group,omitempty"`
	Enabled      bool   `json:"enabled"`
}

type compiledRule struct {
	name         string
	literal      string
	pattern      *regexp.Regexp
	entityType   string
	captureGroup int
}

// RuleSet is immutable after compilation and can be shared by concurrent
// request snapshots. Source rules and their sensitive patterns are not exposed.
type RuleSet struct {
	rules       []compiledRule
	fingerprint [32]byte
}

var customRuleEntities = map[string]struct{}{
	"CUSTOM": {}, "IP_ADDRESS": {}, "CIDR": {}, "HOSTNAME": {}, "DOMAIN": {},
	"USERNAME": {}, "EMAIL_ADDRESS": {}, "EMAIL": {}, "URL": {}, "ORGANIZATION": {},
	"PERSON": {}, "CREDENTIAL": {}, "HASH": {}, "MAC_ADDRESS": {}, "PATH": {},
	"TOKEN": {}, "IDENTIFIER": {}, "PHONE_NUMBER": {}, "CN_ID_CARD": {},
	"ADDRESS": {}, "LOCATION": {},
}

// CompileRules validates disabled rules too, so later enabling a rule cannot
// introduce a malformed expression. Errors identify only the rule index, never
// include its pattern, captured secret or regular-expression parser diagnostics.
func CompileRules(rules []Rule) (*RuleSet, error) {
	if len(rules) > MaxCustomRules {
		return nil, fmt.Errorf("too many custom anonymization rules (maximum %d)", MaxCustomRules)
	}
	compiled := &RuleSet{rules: make([]compiledRule, 0, len(rules))}
	names := make(map[string]struct{}, len(rules))
	for index, rule := range rules {
		invalid := func(reason string) error {
			return fmt.Errorf("invalid custom anonymization rule %d: %s", index+1, reason)
		}
		name := strings.TrimSpace(rule.Name)
		if name == "" || utf8.RuneCountInString(name) > 80 || !utf8.ValidString(name) || strings.ContainsFunc(rule.Name, unicode.IsControl) {
			return nil, invalid("invalid name")
		}
		nameKey := strings.ToLower(name)
		if _, duplicate := names[nameKey]; duplicate {
			return nil, invalid("duplicate name")
		}
		names[nameKey] = struct{}{}
		if strings.TrimSpace(rule.Pattern) == "" || len(rule.Pattern) > MaxRulePatternBytes || !utf8.ValidString(rule.Pattern) {
			return nil, invalid("invalid pattern length")
		}
		entityType := strings.ToUpper(strings.TrimSpace(rule.EntityType))
		if entityType == "" {
			entityType = "CUSTOM"
		}
		if _, valid := customRuleEntities[entityType]; !valid {
			return nil, invalid("unsupported entity type")
		}
		entry := compiledRule{name: name, entityType: entityType, captureGroup: rule.CaptureGroup}
		switch rule.Mode {
		case "literal":
			if rule.CaptureGroup != 0 {
				return nil, invalid("literal rule cannot select a capture group")
			}
			entry.literal = rule.Pattern
		case "regex":
			pattern, err := regexp.Compile(rule.Pattern)
			parsed, parseErr := syntax.Parse(rule.Pattern, syntax.Perl)
			if err != nil || parseErr != nil || nullableRuleExpression(parsed) {
				return nil, invalid("invalid or empty-matching regular expression")
			}
			if rule.CaptureGroup < 0 || rule.CaptureGroup > pattern.NumSubexp() {
				return nil, invalid("capture group outside regular expression")
			}
			if rule.CaptureGroup > 0 && nullableCapture(parsed, rule.CaptureGroup) {
				return nil, invalid("capture group can match an empty value")
			}
			entry.pattern = pattern
		default:
			return nil, invalid("unsupported match mode")
		}
		if rule.Enabled {
			compiled.rules = append(compiled.rules, entry)
		}
	}
	encoded, err := json.Marshal(rules)
	if err != nil {
		return nil, fmt.Errorf("invalid custom anonymization rules")
	}
	compiled.fingerprint = sha256.Sum256(encoded)
	return compiled, nil
}

func nullableCapture(expression *syntax.Regexp, group int) bool {
	if expression.Op == syntax.OpCapture && expression.Cap == group {
		return nullableRuleExpression(expression.Sub[0])
	}
	for _, sub := range expression.Sub {
		if nullableCapture(sub, group) {
			return true
		}
	}
	return false
}

func nullableRuleExpression(expression *syntax.Regexp) bool {
	if expression == nil {
		return false
	}
	switch expression.Op {
	case syntax.OpEmptyMatch, syntax.OpBeginLine, syntax.OpEndLine, syntax.OpBeginText, syntax.OpEndText, syntax.OpWordBoundary, syntax.OpNoWordBoundary, syntax.OpStar, syntax.OpQuest:
		return true
	case syntax.OpLiteral:
		return len(expression.Rune) == 0
	case syntax.OpCapture, syntax.OpPlus:
		return nullableRuleExpression(expression.Sub[0])
	case syntax.OpRepeat:
		return expression.Min == 0 || nullableRuleExpression(expression.Sub[0])
	case syntax.OpConcat:
		for _, sub := range expression.Sub {
			if !nullableRuleExpression(sub) {
				return false
			}
		}
		return true
	case syntax.OpAlternate:
		for _, sub := range expression.Sub {
			if nullableRuleExpression(sub) {
				return true
			}
		}
	}
	return false
}

// Detect resolves overlaps by configured rule order. Match offsets are bytes,
// matching the built-in detector and the spans used by relay request sessions.
func (r *RuleSet) Detect(text string) []Match {
	if r == nil || text == "" {
		return nil
	}
	var matches []Match
	var covered []span
	for _, rule := range r.rules {
		var candidates []span
		if rule.pattern != nil {
			for _, indices := range rule.pattern.FindAllStringSubmatchIndex(text, -1) {
				start, end := indices[rule.captureGroup*2], indices[rule.captureGroup*2+1]
				if start >= 0 && end > start {
					candidates = append(candidates, span{start: start, end: end})
				}
			}
		} else {
			for offset := 0; offset < len(text); {
				position := strings.Index(text[offset:], rule.literal)
				if position < 0 {
					break
				}
				start := offset + position
				end := start + len(rule.literal)
				candidates = append(candidates, span{start: start, end: end})
				offset = end
			}
		}
		var accepted []span
		for _, candidate := range candidates {
			position := sort.Search(len(covered), func(i int) bool { return covered[i].end > candidate.start })
			if position < len(covered) && covered[position].start < candidate.end {
				continue
			}
			matches = append(matches, Match{Text: text[candidate.start:candidate.end], EntityType: rule.entityType, Start: candidate.start, End: candidate.end, RuleName: rule.name})
			accepted = append(accepted, candidate)
		}
		covered = append(covered, accepted...)
		sort.Slice(covered, func(i, j int) bool { return covered[i].start < covered[j].start })
	}
	return matches
}

// DetectWithRules gives explicit custom rules priority over built-in heuristics.
// The built-in pass starts with custom spans reserved, rather than discarding
// broad built-in matches afterward and losing narrower matches inside them.
func DetectWithRules(text string, rules *RuleSet) []Match {
	custom := rules.Detect(text)
	covered := make([]span, 0, len(custom))
	for _, match := range custom {
		covered = append(covered, span{start: match.Start, end: match.End})
	}
	return append(custom, detectStructured(text, covered)...)
}
