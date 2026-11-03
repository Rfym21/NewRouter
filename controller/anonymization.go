package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"new-router/common"
	"new-router/pkg/anonymizer"
	"new-router/setting"
)

const anonymizationPreviewBodyLimit = 256 << 10
const anonymizationPreviewTextLimit = 16 << 10
const anonymizationPreviewMatchLimit = 512

type cachedPreviewDetector struct {
	matches []anonymizer.Match
}

func (p cachedPreviewDetector) Detect(context.Context, string) ([]anonymizer.Match, error) {
	return p.matches, nil
}

// PreviewAnonymization uses a fresh vault and never saves test text, rules or
// mappings. The response is available only to the authenticated root actor.
func PreviewAnonymization(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.GetInt("role") != common.RoleRootUser {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "Unauthorized, insufficient privileges"})
		return
	}
	invalid := func(status int) {
		c.JSON(status, gin.H{"success": false, "code": "ANONYMIZATION_PREVIEW_INVALID", "message": "Invalid anonymization preview input"})
	}
	if c.Request.ContentLength > anonymizationPreviewBodyLimit {
		invalid(http.StatusRequestEntityTooLarge)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, anonymizationPreviewBodyLimit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			invalid(http.StatusRequestEntityTooLarge)
		} else {
			invalid(http.StatusBadRequest)
		}
		return
	}
	var request struct {
		Rules []anonymizer.Rule `json:"rules"`
		Text  string            `json:"text"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if !utf8.Valid(body) || decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || request.Rules == nil || len(request.Text) > anonymizationPreviewTextLimit || strings.TrimSpace(request.Text) == "" {
		invalid(http.StatusBadRequest)
		return
	}
	rules, err := anonymizer.CompileRules(request.Rules)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "ANONYMIZATION_RULES_INVALID", "message": "Invalid anonymization rules"})
		return
	}
	detector, err := setting.AnonymizationDetector()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "code": "ANONYMIZATION_ANALYZER_UNAVAILABLE", "message": "Anonymization analyzer unavailable"})
		return
	}
	matches := anonymizer.DetectWithRules(request.Text, rules)
	session := anonymizer.NewSession("preview", anonymizer.NewVault()).WithRules(rules)
	if detector != nil {
		extra, analyzeErr := detector.Detect(c.Request.Context(), request.Text)
		if analyzeErr != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "code": "ANONYMIZATION_ANALYZER_UNAVAILABLE", "message": "Anonymization analyzer unavailable"})
			return
		}
		for _, match := range extra {
			overlaps := false
			for _, existing := range matches {
				if match.Start < existing.End && match.End > existing.Start {
					overlaps = true
					break
				}
			}
			if !overlaps {
				matches = append(matches, match)
			}
		}
		session.WithDetector(c.Request.Context(), cachedPreviewDetector{matches: extra})
	}
	anonymized := session.AnonymizeString(request.Text)
	if err := session.Err(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "code": "ANONYMIZATION_ANALYZER_UNAVAILABLE", "message": "Anonymization analyzer unavailable"})
		return
	}
	// The detector also identifies reserved public terms. Report only values
	// which the runtime policy actually replaces, matching the preview output.
	used := make(map[string]struct{})
	for _, mapping := range session.Snapshot() {
		used[mapping.EntityType+"\x00"+mapping.Original] = struct{}{}
	}
	accepted := make([]anonymizer.Match, 0, len(matches))
	for _, match := range matches {
		if _, replaced := used[match.EntityType+"\x00"+match.Text]; replaced {
			accepted = append(accepted, match)
		}
	}
	matchCount := len(accepted)
	truncated := matchCount > anonymizationPreviewMatchLimit
	if truncated {
		accepted = accepted[:anonymizationPreviewMatchLimit]
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"matches": accepted, "anonymized": anonymized, "match_count": matchCount, "truncated": truncated}})
}
