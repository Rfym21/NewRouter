package anonymizer

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomRulesRespectOrderedCaptureGroupsAndBuiltinPriority(t *testing.T) {
	rules, err := CompileRules([]Rule{
		{Name: "敏感代号", Mode: "regex", Pattern: `代号[:：]\s*(项目[甲乙丙])`, CaptureGroup: 1, Enabled: true},
		{Name: "内部站点", Mode: "literal", Pattern: "github.com", EntityType: "DOMAIN", Enabled: true},
		{Name: "较宽规则", Mode: "regex", Pattern: `项目[甲乙丙]`, EntityType: "PERSON", Enabled: true},
		{Name: "关闭规则", Mode: "literal", Pattern: "public-value", Enabled: false},
	})
	require.NoError(t, err)
	const text = "代号： 项目甲, github.com, alice@tenant.test, public-value"
	matches := DetectWithRules(text, rules)
	require.Len(t, matches, 3)
	assert.Equal(t, "项目甲", matches[0].Text)
	assert.Equal(t, "CUSTOM", matches[0].EntityType)
	assert.Equal(t, "敏感代号", matches[0].RuleName)
	assert.Equal(t, "github.com", matches[1].Text)
	assert.Equal(t, "DOMAIN", matches[1].EntityType)
	assert.Empty(t, matches[2].RuleName)
	assert.Equal(t, "EMAIL_ADDRESS", matches[2].EntityType)
	for _, match := range matches {
		assert.Equal(t, match.Text, text[match.Start:match.End], "UTF-8 byte spans select the actual captured value")
	}
	session := NewSession("rules", NewVault()).WithRules(rules)
	masked := session.AnonymizeString(text)
	assert.NotContains(t, masked, "项目甲")
	assert.NotContains(t, masked, "github.com", "explicit rules override built-in preserved hosts")
	assert.Contains(t, masked, "public-value")
	assert.Equal(t, text, session.DeanonymizeString(masked))
}

func TestCustomRuleSetSnapshotsDoNotMutateWithSourceRules(t *testing.T) {
	source := []Rule{{Name: "code", Mode: "literal", Pattern: "secret-value", Enabled: true}}
	rules, err := CompileRules(source)
	require.NoError(t, err)
	source[0].Pattern = "different"
	source[0].Enabled = false
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			for range 30 {
				matches := rules.Detect("secret-value different")
				assert.Len(t, matches, 1)
				assert.Equal(t, "secret-value", matches[0].Text)
			}
		})
	}
	group.Wait()
}

func TestCustomRuleValidationDoesNotExposePatternsAndChecksDisabledRules(t *testing.T) {
	valid := Rule{Name: "rule", Mode: "literal", Pattern: "private-secret-value", Enabled: true}
	tests := []struct {
		name string
		rule Rule
	}{
		{"empty name", Rule{Mode: "literal", Pattern: valid.Pattern}},
		{"control name", Rule{Name: "rule\n", Mode: "literal", Pattern: valid.Pattern}},
		{"unknown mode", Rule{Name: "rule", Mode: "glob", Pattern: valid.Pattern}},
		{"empty pattern", Rule{Name: "rule", Mode: "literal"}},
		{"long pattern", Rule{Name: "rule", Mode: "literal", Pattern: strings.Repeat("x", MaxRulePatternBytes+1)}},
		{"invalid regex", Rule{Name: "rule", Mode: "regex", Pattern: "[private-secret-value"}},
		{"lookaround", Rule{Name: "rule", Mode: "regex", Pattern: `(?<=private-secret-value)abc`}},
		{"matches empty", Rule{Name: "rule", Mode: "regex", Pattern: `private-secret-value|.*`}},
		{"negative group", Rule{Name: "rule", Mode: "regex", Pattern: `private-secret-value`, CaptureGroup: -1}},
		{"missing group", Rule{Name: "rule", Mode: "regex", Pattern: `private-secret-value`, CaptureGroup: 1}},
		{"literal group", Rule{Name: "rule", Mode: "literal", Pattern: valid.Pattern, CaptureGroup: 1}},
		{"unknown entity", Rule{Name: "rule", Mode: "literal", Pattern: valid.Pattern, EntityType: "NOT_A_SUPPORTED_ENTITY"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := CompileRules([]Rule{test.rule})
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "private-secret-value")
		})
	}
	_, err := CompileRules([]Rule{valid, {Name: "RULE", Mode: "literal", Pattern: "another", Enabled: false}})
	require.Error(t, err)
	tooMany := make([]Rule, MaxCustomRules+1)
	_, err = CompileRules(tooMany)
	require.Error(t, err)
}

func TestCustomCaptureDoesNotHideAdjacentBuiltinSensitiveValues(t *testing.T) {
	rules, err := CompileRules([]Rule{{Name: "organization", Mode: "literal", Pattern: "private.internal", Enabled: true}})
	require.NoError(t, err)
	matches := DetectWithRules("https://private.internal/path PASSWORD=super-secret", rules)
	require.Len(t, matches, 2)
	assert.Equal(t, "private.internal", matches[0].Text)
	assert.Equal(t, "super-secret", matches[1].Text)
}
