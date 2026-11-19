package clientua

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Category is the broad kind of a request client. It is stored with the log
// entry and used by the console to pick a fallback icon.
type Category string

const (
	CategoryAIAgent    Category = "ai_agent"
	CategoryAISDK      Category = "ai_sdk"
	CategoryAIApp      Category = "ai_app"
	CategoryHTTPClient Category = "http_client"
	CategoryGateway    Category = "gateway"
	CategoryCustom     Category = "custom"
	CategoryOther      Category = "other"
)

// CustomRule groups User-Agent keywords into one client name. Custom rules
// are checked before the built-in library, so an administrator can name a
// wrapper or a private fork without changing the built-in table.
type CustomRule struct {
	Name     string   `json:"name"`
	Keywords []string `json:"keywords"`
	// Icon is an optional icon token such as "lobe:ClaudeCode",
	// "si:SiGo" or "lu:Terminal". The console falls back to a generic
	// terminal icon when it is empty.
	Icon string `json:"icon,omitempty"`
}

// Result is what a User-Agent resolves to.
type Result struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Icon     string `json:"icon,omitempty"`
	Custom   bool   `json:"custom,omitempty"`
}

type builtinRule struct {
	key      string
	name     string
	category Category
	icon     string
	keywords []string
}

const (
	// MaxCustomRules bounds the administrator-managed rule list.
	MaxCustomRules = 100
	// MaxKeywordsPerRule bounds one rule.
	MaxKeywordsPerRule = 50
	// MaxCustomRuleNameRunes bounds a display name.
	MaxCustomRuleNameRunes = 64
	// MaxCustomKeywordRunes bounds one keyword.
	MaxCustomKeywordRunes = 128
	// MaxCustomIconRunes bounds an icon token.
	MaxCustomIconRunes = 128

	maxCustomRulesJSONBytes = 64 << 10
)

// Classify returns the client a User-Agent belongs to. Custom rules win over
// the built-in library, in their listed order. A User-Agent with no match is
// named Other, and an empty one has no name.
func Classify(userAgent string, custom []CustomRule) Result {
	ua := strings.ToLower(strings.TrimSpace(userAgent))
	if ua == "" {
		return Result{Key: "unknown", Category: string(CategoryOther), Icon: "lu:CircleHelp"}
	}

	for _, rule := range custom {
		name := strings.TrimSpace(rule.Name)
		if name == "" {
			continue
		}
		for _, keyword := range rule.Keywords {
			keyword = strings.ToLower(strings.TrimSpace(keyword))
			if keyword == "" || !strings.Contains(ua, keyword) {
				continue
			}
			icon := strings.TrimSpace(rule.Icon)
			if icon == "" {
				icon = "lu:Terminal"
			}
			return Result{
				Key:      "custom",
				Name:     name,
				Category: string(CategoryCustom),
				Icon:     icon,
				Custom:   true,
			}
		}
	}

	for _, rule := range builtinRules {
		for _, keyword := range rule.keywords {
			if strings.Contains(ua, keyword) {
				return Result{
					Key:      rule.key,
					Name:     rule.name,
					Category: string(rule.category),
					Icon:     rule.icon,
				}
			}
		}
	}

	return Result{Key: "other", Name: "Other", Category: string(CategoryOther), Icon: "lu:CircleHelp"}
}

// ParseCustomRules validates and normalizes the JSON stored at
// log_setting.client_rules. An empty or null value is a valid empty list.
func ParseCustomRules(raw string) ([]CustomRule, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil, nil
	}
	if len(raw) > maxCustomRulesJSONBytes {
		return nil, fmt.Errorf("custom client rules must not exceed %d bytes", maxCustomRulesJSONBytes)
	}

	var rules []CustomRule
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rules); err != nil {
		return nil, fmt.Errorf("custom client rules must be a JSON array of {name, keywords}: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("custom client rules must contain exactly one JSON array")
	}
	if len(rules) > MaxCustomRules {
		return nil, fmt.Errorf("custom client rules must list at most %d rules", MaxCustomRules)
	}

	seenNames := make(map[string]struct{}, len(rules))
	normalized := make([]CustomRule, 0, len(rules))
	for index := range rules {
		name := strings.TrimSpace(rules[index].Name)
		if name == "" || utf8.RuneCountInString(name) > MaxCustomRuleNameRunes {
			return nil, fmt.Errorf("custom client rule %d needs a name of 1 to %d characters", index+1, MaxCustomRuleNameRunes)
		}
		if strings.ContainsAny(name, "\r\n\t") {
			return nil, fmt.Errorf("custom client rule %d name must be a single line", index+1)
		}
		nameKey := strings.ToLower(name)
		if _, exists := seenNames[nameKey]; exists {
			return nil, fmt.Errorf("custom client rule name %q is used twice", name)
		}
		seenNames[nameKey] = struct{}{}

		if len(rules[index].Keywords) == 0 || len(rules[index].Keywords) > MaxKeywordsPerRule {
			return nil, fmt.Errorf("custom client rule %q needs 1 to %d keywords", name, MaxKeywordsPerRule)
		}
		keywords := make([]string, 0, len(rules[index].Keywords))
		seenKeywords := make(map[string]struct{}, len(rules[index].Keywords))
		for _, keyword := range rules[index].Keywords {
			keyword = strings.TrimSpace(keyword)
			if keyword == "" || utf8.RuneCountInString(keyword) > MaxCustomKeywordRunes {
				return nil, fmt.Errorf("custom client rule %q has a keyword of 1 to %d characters", name, MaxCustomKeywordRunes)
			}
			if strings.ContainsAny(keyword, "\r\n\t") {
				return nil, fmt.Errorf("custom client rule %q has a keyword that is not a single line", name)
			}
			keywordKey := strings.ToLower(keyword)
			if _, exists := seenKeywords[keywordKey]; exists {
				continue
			}
			seenKeywords[keywordKey] = struct{}{}
			keywords = append(keywords, keyword)
		}
		if len(keywords) == 0 {
			return nil, fmt.Errorf("custom client rule %q needs at least one keyword", name)
		}

		icon := strings.TrimSpace(rules[index].Icon)
		if icon != "" {
			if utf8.RuneCountInString(icon) > MaxCustomIconRunes || strings.ContainsAny(icon, "\r\n\t") {
				return nil, fmt.Errorf("custom client rule %q has an invalid icon", name)
			}
		}

		normalized = append(normalized, CustomRule{Name: name, Keywords: keywords, Icon: icon})
	}
	return normalized, nil
}

// ValidateCustomRules checks the JSON stored at log_setting.client_rules.
func ValidateCustomRules(raw string) error {
	_, err := ParseCustomRules(raw)
	return err
}
