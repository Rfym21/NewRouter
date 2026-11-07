package anonymizer

import (
	"crypto/sha256"
	"errors"
	"strconv"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/singleflight"
)

// FingerprintedDetector identifies the model and configuration behind detector results.
// A changed model revision, language or threshold must produce a different fingerprint.
type FingerprintedDetector interface {
	Detector
	Fingerprint() [32]byte
}

// History keeps only bounded span metadata, scoped to the tenant, route, rules
// and detector configuration. Original text and reversible aliases stay request-local.
var historyDetectionCache = newDetectionCache(4096, 32<<20, 10*time.Minute)
var historyDetectionFlights singleflight.Group

// WithDetectionHistory keeps the first successful model spans stable on one route
// when stable surrogates are enabled. Empty routes disable history reuse.
func (s *Session) WithDetectionHistory(route string) *Session {
	if s != nil {
		scope := sha256.Sum256([]byte(s.engagement + "\x00" + route))
		if scope != s.historyScope {
			s.analysis = nil
		}
		s.historyScope = scope
		s.historyEnabled = route != ""
	}
	return s
}

func (s *Session) detectorMatches(text string) ([]Match, error) {
	detector, fingerprinted := s.detector.(FingerprintedDetector)
	if !s.historyEnabled || !s.vault.stable || !fingerprinted || len(text) > 1<<20 {
		return s.detector.Detect(s.ctx, text)
	}
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	fingerprint := detector.Fingerprint()
	var identity [64]byte
	copy(identity[:32], s.historyScope[:])
	copy(identity[32:], fingerprint[:])
	scope := sha256.Sum256(identity[:])
	now := time.Now()
	key := historyDetectionCache.key(scope, text, s.rules, now)
	if matches, ok := historyDetectionCache.get(key, text, now); ok {
		return matches, nil
	}
	flightKey := string(key.scope[:]) + string(key.rules[:]) + string(key.text[:]) + strconv.FormatInt(key.day, 10)
	result := historyDetectionFlights.DoChan(flightKey, func() (any, error) {
		if matches, ok := historyDetectionCache.get(key, text, time.Now()); ok {
			return detectionSpans(matches), nil
		}
		matches, err := detector.Detect(s.ctx, text)
		if err != nil {
			return nil, err
		}
		if err := validateDetectorMatches(text, matches); err != nil {
			return nil, err
		}
		if err := s.ctx.Err(); err != nil {
			return nil, err
		}
		historyDetectionCache.put(key, matches, time.Now())
		return detectionSpans(matches), nil
	})
	select {
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	case result := <-result:
		if result.Err != nil {
			return nil, result.Err
		}
		if err := s.ctx.Err(); err != nil {
			return nil, err
		}
		spans := result.Val.([]cachedDetection)
		matches := make([]Match, len(spans))
		for i, span := range spans {
			matches[i] = Match{Text: text[span.start:span.end], Start: span.start, End: span.end, EntityType: span.entityType, RuleName: span.ruleName}
		}
		return matches, nil
	}
}

func detectionSpans(matches []Match) []cachedDetection {
	spans := make([]cachedDetection, len(matches))
	for i, match := range matches {
		spans[i] = cachedDetection{start: match.Start, end: match.End, entityType: match.EntityType, ruleName: match.RuleName}
	}
	return spans
}

func validateDetectorMatches(text string, matches []Match) error {
	for _, match := range matches {
		if match.Start < 0 || match.End <= match.Start || match.End > len(text) ||
			text[match.Start:match.End] != match.Text || !utf8.ValidString(match.Text) {
			return errors.New("invalid anonymization detector result")
		}
		if !presidioEntityType.MatchString(match.EntityType) || match.RuleName != "" {
			return errors.New("invalid anonymization detector entity type")
		}
	}
	return nil
}

func detectorFingerprint(detector Detector) ([32]byte, bool) {
	if fingerprinted, ok := detector.(FingerprintedDetector); ok {
		return fingerprinted.Fingerprint(), true
	}
	return [32]byte{}, false
}
