package setting

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
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

// The optional analyzer is explicitly configured by the server operator; it is
// never enabled just because the anonymization switch is on.
func AnonymizationDetector() (anonymizer.Detector, error) {
	endpoint := strings.TrimSpace(os.Getenv("ANONYMIZATION_PRESIDIO_URL"))
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
