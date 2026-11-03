package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"new-router/common"
)

func performAnonymizationPreview(t *testing.T, role int, body string, unknownLength bool) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	router.POST("/api/option/anonymization/preview", func(c *gin.Context) {
		c.Set("role", role)
		PreviewAnonymization(c)
	})
	request := httptest.NewRequest(http.MethodPost, "/api/option/anonymization/preview", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if unknownLength {
		request.ContentLength = -1
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestAnonymizationPreviewUsesUnsavedRulesAndFreshMappings(t *testing.T) {
	t.Setenv("ANONYMIZATION_PRESIDIO_URL", "")
	const body = `{"rules":[{"name":"internal host","mode":"literal","pattern":"github.com","enabled":true}],"text":"github.com alice@tenant.test"}`
	response := performAnonymizationPreview(t, common.RoleRootUser, body, false)
	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Anonymized string `json:"anonymized"`
			Matches    []struct {
				Text       string `json:"text"`
				RuleName   string `json:"rule_name"`
				EntityType string `json:"entity_type"`
				Start, End int
			} `json:"matches"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	assert.True(t, result.Success)
	require.Len(t, result.Data.Matches, 2)
	assert.Equal(t, "github.com", result.Data.Matches[0].Text)
	assert.Equal(t, "internal host", result.Data.Matches[0].RuleName)
	assert.Equal(t, "CUSTOM", result.Data.Matches[0].EntityType)
	assert.NotContains(t, result.Data.Anonymized, "github.com")
	assert.NotContains(t, result.Data.Anonymized, "alice@tenant.test")
	second := performAnonymizationPreview(t, common.RoleRootUser, body, false)
	require.Equal(t, http.StatusOK, second.Code)
	assert.NotEqual(t, response.Body.String(), second.Body.String(), "preview does not reuse persisted aliases")
}

func TestAnonymizationPreviewAllowsEmptyRulesAndSkipsDisabledRules(t *testing.T) {
	t.Setenv("ANONYMIZATION_PRESIDIO_URL", "")
	for _, rules := range []string{`[]`, `[{"name":"off","mode":"literal","pattern":"internal-code","enabled":false}]`} {
		response := performAnonymizationPreview(t, common.RoleRootUser, `{"rules":`+rules+`,"text":"internal-code"}`, false)
		require.Equal(t, http.StatusOK, response.Code)
		assert.Contains(t, response.Body.String(), `"matches":[]`)
		assert.Contains(t, response.Body.String(), `"anonymized":"internal-code"`)
	}
}

func TestAnonymizationPreviewBoundsMatchRowsWithoutTruncatingTheAnonymizedText(t *testing.T) {
	t.Setenv("ANONYMIZATION_PRESIDIO_URL", "")
	text := strings.Repeat("a", anonymizationPreviewMatchLimit+8)
	response := performAnonymizationPreview(t, common.RoleRootUser, `{"rules":[{"name":"characters","mode":"literal","pattern":"a","enabled":true}],"text":"`+text+`"}`, false)
	require.Equal(t, http.StatusOK, response.Code)
	var result struct {
		Data struct {
			Matches    []json.RawMessage `json:"matches"`
			MatchCount int               `json:"match_count"`
			Truncated  bool              `json:"truncated"`
			Anonymized string            `json:"anonymized"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	assert.Len(t, result.Data.Matches, anonymizationPreviewMatchLimit)
	assert.Equal(t, len(text), result.Data.MatchCount)
	assert.True(t, result.Data.Truncated)
	assert.Equal(t, len(text), strings.Count(result.Data.Anonymized, "[CUSTOM_"))
}

func TestAnonymizationPreviewRejectsInvalidInputWithoutEchoingText(t *testing.T) {
	t.Setenv("ANONYMIZATION_PRESIDIO_URL", "")
	const private = "private-sensitive-value"
	for _, body := range []string{
		`{"rules":[],"text":"private-sensitive-value","unknown":true}`,
		`{"rules":[{"name":"rule","mode":"literal","pattern":"secret","enabled":true,"unknown":true}],"text":"private-sensitive-value"}`,
		`{"rules":[],"text":"private-sensitive-value"} {"extra":true}`,
		`{"rules":null,"text":"private-sensitive-value"}`,
		`{"rules":[],"text":"   "}`,
		`{"rules":[],"text":123}`,
		`{"rules":[],"text":"` + strings.Repeat("x", anonymizationPreviewTextLimit+1) + `"}`,
		`{"rules":[{"name":"invalid","mode":"regex","pattern":"[private-sensitive-value","enabled":true}],"text":"private-sensitive-value"}`,
	} {
		response := performAnonymizationPreview(t, common.RoleRootUser, body, false)
		assert.Equal(t, http.StatusBadRequest, response.Code)
		assert.NotContains(t, response.Body.String(), private)
	}
	for _, unknownLength := range []bool{false, true} {
		response := performAnonymizationPreview(t, common.RoleRootUser, strings.Repeat("x", anonymizationPreviewBodyLimit+1), unknownLength)
		assert.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
	}
}

func TestAnonymizationPreviewRejectsNonRootBeforeAnalyzing(t *testing.T) {
	t.Setenv("ANONYMIZATION_PRESIDIO_URL", "invalid")
	for _, role := range []int{0, common.RoleCommonUser, common.RoleAdminUser} {
		response := performAnonymizationPreview(t, role, `{"rules":[],"text":"private-sensitive-value"}`, false)
		assert.Equal(t, http.StatusForbidden, response.Code)
		assert.NotContains(t, response.Body.String(), "private-sensitive-value")
	}
}

func TestAnonymizationPreviewMatchesConfiguredAnalyzerOnceAndFailsClosed(t *testing.T) {
	requests := 0
	analyzer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		fmt.Fprint(w, `[{"entity_type":"PERSON","start":5,"end":22,"score":0.95}]`)
	}))
	defer analyzer.Close()
	t.Setenv("ANONYMIZATION_PRESIDIO_URL", analyzer.URL)
	response := performAnonymizationPreview(t, common.RoleRootUser, `{"rules":[],"text":"call Jane Confidential today"}`, false)
	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, 1, requests)
	assert.Contains(t, response.Body.String(), `"text":"Jane Confidential"`)
	assert.Contains(t, response.Body.String(), `"entity_type":"PERSON"`)
	assert.NotContains(t, response.Body.String(), `"anonymized":"call Jane Confidential today"`)
	t.Setenv("ANONYMIZATION_PRESIDIO_URL", "invalid")
	response = performAnonymizationPreview(t, common.RoleRootUser, `{"rules":[],"text":"private-sensitive-value"}`, false)
	assert.Equal(t, http.StatusServiceUnavailable, response.Code)
	assert.NotContains(t, response.Body.String(), "private-sensitive-value")
}
