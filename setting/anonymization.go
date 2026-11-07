package setting

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"new-router/pkg/anonymizer"
)

const AnonymizationRulesOptionKey = "anonymization_setting.custom_rules"

func ParseAnonymizationRules(value string) (*anonymizer.RuleSet, error) {
	if len(value) > 128<<10 {
		return nil, errors.New("anonymization rules are too large")
	}
	var rules []anonymizer.Rule
	decoder := json.NewDecoder(bytes.NewBufferString(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rules); err != nil {
		return nil, errors.New("invalid anonymization rules JSON")
	}
	if rules == nil {
		return nil, errors.New("anonymization rules must be an array")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("invalid anonymization rules JSON")
	}
	return anonymizer.CompileRules(rules)
}

var anonymizationAnalyzerClient = &http.Client{Timeout: 3 * time.Second}

var localAnonymizationAnalyzer struct {
	sync.Mutex
	directory string
	detector  *anonymizer.LocalDetector
}

// The optional analyzer is explicitly configured by the server operator; it is
// never enabled just because the anonymization switch is on.
func AnonymizationDetector() (anonymizer.Detector, error) {
	endpoint := strings.TrimSpace(os.Getenv("ANONYMIZATION_PRESIDIO_URL"))
	modelDir := strings.TrimSpace(os.Getenv("ANONYMIZATION_LOCAL_NER_MODEL_DIR"))
	if modelDir != "" {
		if endpoint != "" {
			return nil, errors.New("configure one anonymization analyzer")
		}
		directory, err := filepath.Abs(modelDir)
		if err != nil {
			return nil, errors.New("invalid anonymization model directory")
		}
		localAnonymizationAnalyzer.Lock()
		defer localAnonymizationAnalyzer.Unlock()
		if localAnonymizationAnalyzer.detector != nil && localAnonymizationAnalyzer.directory == directory {
			return localAnonymizationAnalyzer.detector, nil
		}
		detector, err := anonymizer.NewLocalDetector(directory)
		if err != nil {
			return nil, errors.New("local anonymization model unavailable")
		}
		localAnonymizationAnalyzer.directory = directory
		localAnonymizationAnalyzer.detector = detector
		return detector, nil
	}
	if endpoint == "" {
		return nil, nil
	}
	language := strings.TrimSpace(os.Getenv("ANONYMIZATION_PRESIDIO_LANGUAGE"))
	if language == "" {
		language = "en"
	}
	return anonymizer.NewPresidioDetector(endpoint, language, anonymizationAnalyzerClient)
}

// AnonymizationEnabled controls whether request/response anonymization is
// enabled by default. Channel settings can override this default per channel.
var AnonymizationEnabled = false

// AnonymizationStableSurrogates controls whether surrogates are derived
// deterministically per user/token engagement instead of drawn fresh per
// request. Stable surrogates keep request prefixes byte-identical across
// turns, so upstream prompt caches stay effective for full-history clients;
// the trade-off is that one original always maps to one pseudonym inside an
// engagement while values never link across users or tokens.
var AnonymizationStableSurrogates = false
