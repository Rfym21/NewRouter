package anonymizer

import (
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

// Vault keeps stable mappings per engagement.
type Vault struct {
	mu       sync.Mutex
	byKey    map[string]Mapping
	byEngage map[string][]Mapping
	used     map[string]struct{}
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
		return mapping
	}

	surrogate := GenerateSurrogate(original, entityType)
	for i := 0; i < 200; i++ {
		usedKey := engagement + "\x00" + surrogate
		if _, exists := v.used[usedKey]; !exists {
			v.used[usedKey] = struct{}{}
			break
		}
		surrogate = GenerateSurrogate(original, entityType)
	}

	mapping := Mapping{
		EntityType: entityType,
		Original:   original,
		Surrogate:  surrogate,
	}
	v.byKey[key] = mapping
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

// DefaultVault is the process-local vault used by relay request sessions.
var DefaultVault = NewVault()

// Session applies anonymization and deanonymization for one relay request.
type Session struct {
	engagement string
	vault      *Vault
}

// NewSession creates a request-scoped anonymization session.
func NewSession(engagement string, vault *Vault) *Session {
	engagement = strings.TrimSpace(engagement)
	if engagement == "" {
		engagement = "default"
	}
	if vault == nil {
		vault = DefaultVault
	}
	return &Session{engagement: engagement, vault: vault}
}

// Engagement returns the vault partition used by the session.
func (s *Session) Engagement() string {
	if s == nil {
		return ""
	}
	return s.engagement
}

// AnonymizeString replaces detected sensitive values with stable surrogates.
func (s *Session) AnonymizeString(text string) string {
	if s == nil || strings.TrimSpace(text) == "" {
		return text
	}

	matches := Detect(text)
	if len(matches) == 0 {
		return text
	}

	entities := make(map[string]string, len(matches))
	for _, match := range matches {
		if shouldKeepOriginal(match.Text, match.EntityType) || s.vault.hasSurrogate(s.engagement, match.Text) {
			continue
		}
		entities[match.Text] = match.EntityType
	}
	if len(entities) == 0 {
		return text
	}

	type entity struct {
		original   string
		entityType string
	}
	ordered := make([]entity, 0, len(entities))
	for original, entityType := range entities {
		ordered = append(ordered, entity{original: original, entityType: entityType})
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return len(ordered[i].original) > len(ordered[j].original)
	})

	result := text
	for _, item := range ordered {
		if !strings.Contains(result, item.original) {
			continue
		}
		mapping := s.vault.GetOrCreate(s.engagement, item.original, item.entityType)
		result = strings.ReplaceAll(result, item.original, mapping.Surrogate)
	}
	return result
}

// DeanonymizeString restores surrogates for this session engagement.
func (s *Session) DeanonymizeString(text string) string {
	if s == nil || text == "" {
		return text
	}
	result := text
	for _, mapping := range s.vault.Mappings(s.engagement) {
		if strings.Contains(result, mapping.Surrogate) {
			result = strings.ReplaceAll(result, mapping.Surrogate, mapping.Original)
		}
	}
	return result
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
