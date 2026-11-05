// Frozen pre-optimization detector used as a differential oracle.
package anonymizer

import "strings"

func detectStructuredReference(text string, existing []span) []Match {
	if text == "" {
		return nil
	}
	matches := make([]Match, 0)
	covered := append([]span(nil), existing...)

	overlaps := func(start int, end int) bool {
		for _, current := range covered {
			if start < current.end && end > current.start {
				return true
			}
		}
		return false
	}

	// Check explicit identifiers before generic numeric/hash rules. Labels and
	// validation prevent token counters and arbitrary digit strings becoming PII.
	for _, spec := range chinesePatterns {
		for _, idx := range spec.re.FindAllStringSubmatchIndex(text, -1) {
			start, end := idx[0], idx[1]
			if len(idx) >= 4 && idx[2] >= 0 {
				start, end = idx[2], idx[3]
			}
			if overlaps(start, end) || end <= start {
				continue
			}
			value := strings.TrimSpace(text[start:end])
			if value == "" {
				continue
			}
			start += strings.Index(text[start:end], value)
			end = start + len(value)
			if spec.entityType == "CN_ID_CARD" && !validChineseID(value) {
				continue
			}
			if spec.entityType == "PHONE_NUMBER" && hasUsageNumberContext(text[:start]) {
				continue
			}
			matches = append(matches, Match{Text: value, EntityType: spec.entityType, Start: start, End: end})
			covered = append(covered, span{start: start, end: end})
		}
	}

	for _, spec := range detectorPatterns {
		for _, idx := range spec.re.FindAllStringSubmatchIndex(text, -1) {
			if len(idx) < 2 || idx[0] < 0 || idx[1] <= idx[0] {
				continue
			}
			start, end := idx[0], idx[1]
			for i := 2; i+1 < len(idx); i += 2 {
				if idx[i] >= 0 && idx[i+1] > idx[i] {
					start, end = idx[i], idx[i+1]
					break
				}
			}
			if overlaps(start, end) {
				continue
			}
			value := strings.TrimSpace(text[start:end])
			if value == "" {
				continue
			}
			start += strings.Index(text[start:end], value)
			end = start + len(value)
			matches = append(matches, Match{Text: value, EntityType: spec.entityType, Start: start, End: end})
			covered = append(covered, span{start: start, end: end})
		}
	}

	for _, idx := range panPattern.FindAllStringIndex(text, -1) {
		if len(idx) != 2 || overlaps(idx[0], idx[1]) {
			continue
		}
		value := text[idx[0]:idx[1]]
		digits := digitsOnly(value)
		if len(digits) >= 13 && luhnValid(digits) {
			matches = append(matches, Match{Text: strings.TrimSpace(value), EntityType: "CREDENTIAL", Start: idx[0], End: idx[1]})
			covered = append(covered, span{start: idx[0], end: idx[1]})
		}
	}

	return matches
}
