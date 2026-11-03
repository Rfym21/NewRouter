package anonymizer

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/netip"
	"sort"
	"strings"
	"sync"
)

// Mapping stores a reversible original/surrogate pair.
type Mapping struct {
	EntityType string
	Original   string
	Surrogate  string
}

type rawTextKey struct {
	engagement string
	digest     [32]byte
}

type preparedMatch struct {
	Match
	original string
}

// Vault keeps stable mappings per engagement.
type Vault struct {
	mu            sync.Mutex
	byKey         map[string]Mapping
	byEngage      map[string][]Mapping
	used          map[string]struct{}
	reserved      map[string]struct{}
	reservedBytes int
	rawTexts      map[rawTextKey]struct{}
	bytes         int
	err           error
	// sorted caches the longest-surrogate-first Mappings result per engagement;
	// invalidated on GetOrCreate. Deanonymize calls Mappings once per restored
	// string, so caching avoids re-copying and re-sorting on every stream chunk.
	sorted map[string][]Mapping
}

// NewVault creates an empty in-memory anonymization vault.
func NewVault() *Vault {
	return &Vault{
		byKey:    make(map[string]Mapping),
		byEngage: make(map[string][]Mapping),
		used:     make(map[string]struct{}),
		reserved: make(map[string]struct{}),
		rawTexts: make(map[rawTextKey]struct{}),
		sorted:   make(map[string][]Mapping),
	}
}

// GetOrCreate returns the existing surrogate or creates one for the engagement.
func (v *Vault) GetOrCreate(engagement string, original string, entityType string) Mapping {
	if v == nil {
		v = DefaultVault
	}
	key := engagement + "\x00" + entityType + "\x00" + original

	v.mu.Lock()
	defer v.mu.Unlock()

	if mapping, ok := v.byKey[key]; ok {
		// A restored plaintext value can equal an earlier realistic surrogate.
		// Keep the earlier reverse mapping for opaque history, but use a fresh
		// forward alias once that surrogate is reserved as a real value.
		_, realValue := v.reserved[engagement+"\x00"+mapping.Surrogate]
		_, rawValue := v.rawTexts[rawTextKey{engagement, sha256.Sum256([]byte(mapping.Surrogate))}]
		if !realValue && !rawValue {
			return mapping
		}
	}
	if len(v.byEngage[engagement]) >= 8192 || v.bytes+len(original)+256 > 2<<20 {
		v.err = errors.New("anonymization mapping capacity exceeded")
		return Mapping{}
	}

	surrogate := GenerateSurrogate(original, entityType)
	available := false
	for i := 0; i < 200; i++ {
		usedKey := engagement + "\x00" + surrogate
		_, reserved := v.reserved[usedKey]
		_, rawValue := v.rawTexts[rawTextKey{engagement, sha256.Sum256([]byte(surrogate))}]
		if _, exists := v.used[usedKey]; !exists && !reserved && !rawValue && surrogate != original {
			v.used[usedKey] = struct{}{}
			available = true
			break
		}
		surrogate = GenerateSurrogate(original, entityType)
	}
	if !available {
		v.err = errors.New("anonymization replacement capacity exceeded")
		return Mapping{}
	}

	mapping := Mapping{
		EntityType: entityType,
		Original:   original,
		Surrogate:  surrogate,
	}
	v.byKey[key] = mapping
	v.bytes += len(original) + len(surrogate) + len(entityType)
	v.byEngage[engagement] = append(v.byEngage[engagement], mapping)
	delete(v.sorted, engagement)
	return mapping
}

// Mappings returns mappings for the engagement, longest surrogate first. The
// result is cached (invalidated on GetOrCreate); callers must treat it as
// read-only.
func (v *Vault) Mappings(engagement string) []Mapping {
	if v == nil {
		v = DefaultVault
	}
	v.mu.Lock()
	defer v.mu.Unlock()

	if cached, ok := v.sorted[engagement]; ok {
		return cached
	}
	mappings := append([]Mapping(nil), v.byEngage[engagement]...)
	sort.SliceStable(mappings, func(i, j int) bool {
		return len(mappings[i].Surrogate) > len(mappings[j].Surrogate)
	})
	if v.sorted == nil {
		v.sorted = make(map[string][]Mapping)
	}
	v.sorted[engagement] = mappings
	return mappings
}

func (v *Vault) hasSurrogate(engagement string, value string) bool {
	if v == nil {
		v = DefaultVault
	}
	v.mu.Lock()
	defer v.mu.Unlock()

	_, ok := v.used[engagement+"\x00"+value]
	return ok
}

// DefaultVault is retained for legacy callers. New sessions use isolated vaults.
var DefaultVault = NewVault()

// Session applies anonymization and deanonymization for one relay request.
type Session struct {
	engagement       string
	vault            *Vault
	ctx              context.Context
	detector         Detector
	rules            *RuleSet
	err              error
	analysis         map[string][]Match
	preflighting     bool
	rawInput         bool
	requestPrepared  bool
	preflightMatches map[string][]preparedMatch
}

// beginRequestPreflight makes the first complete helper input authoritative raw
// text. Later helper inputs are derived protocol conversions and can retain
// already-generated aliases. The temporary cache lives for one transform only.
func (s *Session) beginRequestPreflight() {
	s.preflighting = true
	s.rawInput = !s.requestPrepared
	s.preflightMatches = make(map[string][]preparedMatch)
}

func (s *Session) endRequestTransform(success bool) {
	s.preflighting = false
	s.rawInput = false
	s.preflightMatches = nil
	if success {
		s.requestPrepared = true
	}
}

// NewSession creates a request-scoped anonymization session.
func NewSession(engagement string, vault *Vault) *Session {
	engagement = strings.TrimSpace(engagement)
	if engagement == "" {
		engagement = "default"
	}
	if vault == nil {
		vault = NewVault()
	}
	return &Session{engagement: engagement, vault: vault, ctx: context.Background()}
}

// WithRules snapshots the validated rules for this request.
func (s *Session) WithRules(rules *RuleSet) *Session {
	if s != nil {
		s.rules = rules
	}
	return s
}

func (s *Session) WithDetector(ctx context.Context, detector Detector) *Session {
	if s != nil {
		if ctx != nil {
			s.ctx = ctx
		}
		s.detector = detector
	}
	return s
}

// Err must be checked before an anonymized request is sent upstream.
func (s *Session) Err() error {
	if s == nil {
		return nil
	}
	if s.err != nil {
		return s.err
	}
	s.vault.mu.Lock()
	defer s.vault.mu.Unlock()
	return s.vault.err
}

func (s *Session) Snapshot() []Mapping {
	if s == nil {
		return nil
	}
	return append([]Mapping(nil), s.vault.Mappings(s.engagement)...)
}

// Import merges immutable saved mappings into a request-local vault. Several
// surrogates for one original are allowed when independent history branches join.
func (s *Session) Import(mappings []Mapping) error {
	if s == nil {
		return nil
	}
	s.vault.mu.Lock()
	defer s.vault.mu.Unlock()
	for _, m := range mappings {
		if m.Original == "" || m.Surrogate == "" || m.EntityType == "" {
			return errors.New("invalid saved anonymization mapping")
		}
		usedKey := s.engagement + "\x00" + m.Surrogate
		if _, exists := s.vault.used[usedKey]; exists {
			for _, current := range s.vault.byEngage[s.engagement] {
				if current.Surrogate == m.Surrogate && current.Original != m.Original {
					return errors.New("conflicting anonymization contexts")
				}
			}
			continue
		}
		if len(s.vault.byEngage[s.engagement]) >= 8192 || s.vault.bytes+len(m.Original)+len(m.Surrogate)+len(m.EntityType) > 2<<20 {
			return errors.New("anonymization mapping capacity exceeded")
		}
		originalKey := s.engagement + "\x00" + m.Original
		if _, reserved := s.vault.reserved[originalKey]; !reserved {
			if len(s.vault.reserved) >= 8192 || s.vault.reservedBytes+len(m.Original) > 2<<20 {
				return errors.New("anonymization mapping capacity exceeded")
			}
			s.vault.reservedBytes += len(m.Original)
		}
		key := s.engagement + "\x00" + m.EntityType + "\x00" + m.Original
		if _, exists := s.vault.byKey[key]; !exists {
			s.vault.byKey[key] = m
		}
		s.vault.used[usedKey] = struct{}{}
		s.vault.reserved[originalKey] = struct{}{}
		s.vault.byEngage[s.engagement] = append(s.vault.byEngage[s.engagement], m)
		s.vault.bytes += len(m.Original) + len(m.Surrogate) + len(m.EntityType)
	}
	delete(s.vault.sorted, s.engagement)
	return nil
}

// Engagement returns the vault partition used by the session.
func (s *Session) Engagement() string {
	if s == nil {
		return ""
	}
	return s.engagement
}

// AnonymizeString replaces detected sensitive values with stable surrogates.
func (s *Session) rawText(text string) bool {
	if s.rawInput {
		return true
	}
	s.vault.mu.Lock()
	defer s.vault.mu.Unlock()
	_, raw := s.vault.rawTexts[rawTextKey{s.engagement, sha256.Sum256([]byte(text))}]
	return raw
}

func (s *Session) reserveRawText(text string) bool {
	key := rawTextKey{s.engagement, sha256.Sum256([]byte(text))}
	s.vault.mu.Lock()
	defer s.vault.mu.Unlock()
	if _, existing := s.vault.rawTexts[key]; existing {
		return true
	}
	if len(s.vault.rawTexts) >= 16384 {
		s.err = errors.New("anonymization text slot capacity exceeded")
		return false
	}
	s.vault.rawTexts[key] = struct{}{}
	return true
}

func (s *Session) detectedMatches(text string, raw bool) []preparedMatch {
	matches := DetectWithRules(text, s.rules)
	if s.detector != nil {
		extra, cached := s.analysis[text]
		if !cached {
			var err error
			extra, err = s.detector.Detect(s.ctx, text)
			if err != nil {
				s.err = errors.New("anonymization detector unavailable")
				return nil
			}
			if len(text) <= 65536 {
				if s.analysis == nil {
					s.analysis = make(map[string][]Match)
				}
				if len(s.analysis) < 128 {
					s.analysis[text] = extra
				}
			}
		}
		for _, m := range extra {
			if m.Start < 0 || m.End <= m.Start || m.End > len(text) || text[m.Start:m.End] != m.Text {
				s.err = errors.New("invalid anonymization detector result")
				return nil
			}
			overlaps := false
			for _, current := range matches {
				if m.Start < current.End && m.End > current.Start {
					overlaps = true
					break
				}
			}
			if !overlaps {
				matches = append(matches, m)
			}
		}
	}
	// Raw input is authoritative even if it literally contains an earlier alias.
	// Give explicit custom rules precedence; then recognize complete old aliases
	// before heuristics which might recognize only an IP inside a fake URL.
	var forced []Match
	if raw {
		var selected []Match
		for _, match := range matches {
			if match.RuleName != "" {
				selected = append(selected, match)
			}
		}
		overlaps := func(match Match) bool {
			for _, existing := range selected {
				if match.Start < existing.End && match.End > existing.Start {
					return true
				}
			}
			return false
		}
		for _, mapping := range s.vault.Mappings(s.engagement) {
			if mapping.Surrogate == "" {
				continue
			}
			for offset := 0; offset < len(text); {
				position := strings.Index(text[offset:], mapping.Surrogate)
				if position < 0 {
					break
				}
				start := offset + position
				end := start + len(mapping.Surrogate)
				candidate := Match{Text: mapping.Surrogate, EntityType: mapping.EntityType, Start: start, End: end}
				if !overlaps(candidate) {
					selected = append(selected, candidate)
					forced = append(forced, candidate)
				}
				offset = end
			}
		}
		for _, match := range matches {
			if match.RuleName == "" && !overlaps(match) {
				selected = append(selected, match)
			}
		}
		matches = selected
	}
	filtered := make([]preparedMatch, 0, len(matches))
	for _, match := range matches {
		isAlias := false
		for _, candidate := range forced {
			if candidate.Start == match.Start && candidate.End == match.End {
				isAlias = true
				break
			}
		}
		if (!isAlias && match.RuleName == "" && shouldKeepOriginal(match.Text, match.EntityType)) || (!raw && s.isSurrogateMatch(text, match)) {
			continue
		}
		original := match.Text
		if !raw {
			// Derived converters may join several already-masked text slots. A
			// broad rule can cover that joined text; flatten its old aliases once
			// rather than creating a mapping whose original contains other aliases.
			original = s.DeanonymizeString(original)
		}
		filtered = append(filtered, preparedMatch{Match: match, original: original})
	}
	return filtered
}

// isSurrogateMatch also recognizes a detector match inside a known URL/token
// alias. Checking only exact entity strings would re-anonymize fake URL hosts
// during a second protocol conversion and destroy their reverse mapping.
func (s *Session) isSurrogateMatch(text string, match Match) bool {
	if s.vault.hasSurrogate(s.engagement, match.Text) {
		return true
	}
	for _, mapping := range s.vault.Mappings(s.engagement) {
		if len(mapping.Surrogate) <= match.End-match.Start {
			continue
		}
		start := max(0, match.End-len(mapping.Surrogate))
		end := min(len(text), match.Start+len(mapping.Surrogate))
		if end < start {
			continue
		}
		for position := start; position < end; {
			relative := strings.Index(text[position:end], mapping.Surrogate)
			if relative < 0 {
				break
			}
			position += relative
			if position <= match.Start && position+len(mapping.Surrogate) >= match.End {
				return true
			}
			position++
		}
	}
	return false
}

func (s *Session) reserveOriginals(matches []preparedMatch) bool {
	s.vault.mu.Lock()
	defer s.vault.mu.Unlock()
	for _, match := range matches {
		if _, exists := s.vault.reserved[s.engagement+"\x00"+match.original]; exists {
			continue
		}
		if len(s.vault.reserved) >= 8192 || s.vault.reservedBytes+len(match.original) > 2<<20 {
			s.err = errors.New("anonymization mapping capacity exceeded")
			return false
		}
		s.vault.reserved[s.engagement+"\x00"+match.original] = struct{}{}
		s.vault.reservedBytes += len(match.original)
		// A non-canonical address/network is still the same sensitive value.
		// Prevent a canonical fake from representing that real network.
		canonical := match.original
		if match.EntityType == "CIDR" {
			if prefix, err := netip.ParsePrefix(match.original); err == nil {
				canonical = prefix.Masked().String()
			}
		} else if match.EntityType == "IP_ADDRESS" {
			if address, err := netip.ParseAddr(match.original); err == nil {
				canonical = address.String()
			}
		}
		if canonical != match.original {
			key := rawTextKey{s.engagement, sha256.Sum256([]byte(canonical))}
			if _, exists := s.vault.rawTexts[key]; !exists && len(s.vault.rawTexts) >= 16384 {
				s.err = errors.New("anonymization text slot capacity exceeded")
				return false
			}
			s.vault.rawTexts[key] = struct{}{}
		}
	}
	return true
}

// AnonymizeString applies one-pass span replacements. Complete request helpers
// first invoke it in preflight mode for every text slot before generating any
// alias, which prevents cross-field realistic-surrogate collisions.
func (s *Session) AnonymizeString(text string) string {
	if s == nil || strings.TrimSpace(text) == "" || s.Err() != nil {
		return text
	}
	raw := s.rawText(text)
	if raw && s.preflighting && !s.reserveRawText(text) {
		return text
	}
	if !raw && s.vault.hasSurrogate(s.engagement, text) {
		return text
	}
	filtered, cached := s.preflightMatches[text]
	if !cached {
		filtered = s.detectedMatches(text, raw)
		if s.preflightMatches != nil {
			s.preflightMatches[text] = filtered
		}
	}
	if s.Err() != nil || len(filtered) == 0 || !s.reserveOriginals(filtered) {
		return text
	}
	if s.preflighting {
		return text
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Start == filtered[j].Start {
			return filtered[i].End > filtered[j].End
		}
		return filtered[i].Start < filtered[j].Start
	})
	var result strings.Builder
	cursor := 0
	for _, item := range filtered {
		if item.Start < cursor || item.End > len(text) || item.End <= item.Start {
			continue
		}
		mapping := s.vault.GetOrCreate(s.engagement, item.original, item.EntityType)
		if mapping.Surrogate == "" {
			return text
		}
		result.WriteString(text[cursor:item.Start])
		result.WriteString(mapping.Surrogate)
		cursor = item.End
	}
	result.WriteString(text[cursor:])
	return result.String()
}

// DeanonymizeString restores surrogates for this session engagement.
func (s *Session) DeanonymizeString(text string) string {
	if s == nil || text == "" {
		return text
	}
	pairs := make([]string, 0)
	for _, mapping := range s.vault.Mappings(s.engagement) {
		if strings.Contains(text, mapping.Surrogate) {
			pairs = append(pairs, mapping.Surrogate, mapping.Original)
		}
	}
	if len(pairs) == 0 {
		return text
	}
	return strings.NewReplacer(pairs...).Replace(text)
}

// DeanonymizeValue recursively restores strings inside JSON-like values.
func (s *Session) DeanonymizeValue(value any) any {
	switch v := value.(type) {
	case string:
		return s.DeanonymizeString(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = s.DeanonymizeValue(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = s.DeanonymizeValue(item)
		}
		return out
	default:
		return value
	}
}
