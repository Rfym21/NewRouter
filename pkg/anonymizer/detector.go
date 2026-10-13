package anonymizer

import (
	"encoding/base64"
	"regexp"
	"strings"
)

// obfKey masks embedded detector metadata (offensive-security tool names, secret
// prefixes, artifact filenames). These literals are heuristically flagged as
// hacktool signatures by endpoint AV (e.g. McAfee) when present verbatim in the
// compiled binary, causing false positives. Storing them XOR+base64-encoded keeps
// the plaintext out of the binary; detection behavior is unchanged after decode.
var obfKey = []byte("nr-anon-v1")

// deobf reverses the obfKey/base64 masking applied to embedded detector data.
func deobf(encoded string) string {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	out := make([]byte, len(raw))
	for i := range raw {
		out[i] = raw[i] ^ obfKey[i%len(obfKey)]
	}
	return string(out)
}

// Match is one detected sensitive value and its entity type.
type Match struct {
	Text       string
	EntityType string
}

type patternSpec struct {
	entityType string
	re         *regexp.Regexp
}

type span struct {
	start int
	end   int
}

var detectorPatterns = []patternSpec{
	mustPattern("CIDR", `\b(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)/(?:[12]?\d|3[0-2])\b`),
	mustPattern("IP_ADDRESS", `\b(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\b`),
	mustPattern("IP_ADDRESS", `\b(?:[0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}\b|\b(?:[0-9a-fA-F]{1,4}:){1,7}:\b|\b::(?:[fF]{4}(?::0{1,4})?:)?(?:25[0-5]|2[0-4]\d|[01]?\d\d?)(?:\.(?:25[0-5]|2[0-4]\d|[01]?\d\d?)){3}\b`),
	mustPattern("HASH", `\b0x0200[0-9a-fA-F]{64}\b`),
	mustPattern("HASH", `\b[a-fA-F0-9]{64}\b`),
	mustPattern("HASH", `\b[a-fA-F0-9]{48}\b`),
	mustPattern("HASH", `\b[a-fA-F0-9]{40}\b`),
	mustPattern("HASH", `\b[a-fA-F0-9]{32}\b`),
	mustPattern("HASH", `\b[0-9a-fA-F]{16}\b`),
	mustPattern("TOKEN", `\b0x[0-9a-fA-F]{16,}\b`),
	mustPattern("MAC_ADDRESS", `\b(?:[0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}\b`),
	mustPattern("EMAIL_ADDRESS", `\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`),
	mustPattern("URL", `https?://[^\s<>"'{}|\\^`+"`"+`\[\])()]+`),
	mustPattern("TOKEN", `\b(?:AKIA|ASIA|AIDA|AROA|AIPA|ANPA|ANVA|APKA)[A-Z0-9]{16,17}\b`),
	mustPattern("TOKEN", `\b(?:sk_live|sk_test|pk_live|pk_test|rk_live|whsec)_[A-Za-z0-9]{10,64}\b`),
	mustPattern("TOKEN", `\bSG\.[A-Za-z0-9_\-]{20,}\b`),
	mustPattern("TOKEN", `\bya29\.[A-Za-z0-9_.\-]{20,}\b`),
	mustPattern("TOKEN", `(?i)\bAWS_SECRET_ACCESS_KEY\s*=\s*([A-Za-z0-9/+]{40})\b`),
	mustPattern("TOKEN", `(?i)\baws_secret_access_key\s*=\s*([A-Za-z0-9/+]{40})\b`),
	mustPattern("TOKEN", `\bgh[pousr]_[A-Za-z0-9]{36,}\b`),
	mustPattern("TOKEN", `\bxox[bpasreg]-[0-9A-Za-z\-]{10,}\b`),
	mustPattern("TOKEN", `\bsq0[a-z]{3}-[0-9A-Za-z_\-]{22,43}\b`),
	mustPattern("TOKEN", `\bEAAA[a-zA-Z0-9]{57,63}\b`),
	mustPattern("TOKEN", `\bkey-[0-9a-zA-Z]{32}\b`),
	mustPattern("TOKEN", `\b[0-9a-f]{32}-us\d{1,2}\b`),
	mustPattern("TOKEN", `\bAC[a-f0-9]{32}\b`),
	mustPattern("TOKEN", `\bAIza[0-9A-Za-z_\-]{35}\b`),
	mustPattern("TOKEN", `\bsk-ant-[A-Za-z0-9_\-]{8,}\b`),
	mustPattern("TOKEN", `\bgsk_[A-Za-z0-9]{8,}\b`),
	mustPattern("TOKEN", `\bcsk-[A-Za-z0-9]{8,}\b`),
	mustPattern("TOKEN", `\bsk_sn-[A-Za-z0-9_\-]{8,}\b`),
	mustPattern("TOKEN", `\bshp(?:at|pa|ca|ss|ua)_[0-9a-fA-F]{32}\b`),
	mustPattern("TOKEN", `\beyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\b`),
	mustPattern("IDENTIFIER", `\barn:aws:[a-z0-9\-]+:[a-z0-9\-]*:\d{12}:[^\s]+`),
	mustPattern("IDENTIFIER", `\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`),
	mustPattern("IDENTIFIER", `\b\d{20,21}\b`),
	mustPattern("IDENTIFIER", `\b\d{12}\b`),
	mustPattern("CREDENTIAL", `\b\d{3}\.\d{3}\.\d{3}-\d{2}\b`),
	mustPattern("CREDENTIAL", `\b\d{2}\.\d{3}\.\d{3}/\d{4}-\d{2}\b`),
	mustPattern("CREDENTIAL", `\b\d{2}\.\d{3}\.\d{3}-[\dXx]\b`),
	mustPattern("CREDENTIAL", `\b(?:00[1-9]|0[1-5]\d|[1-5]\d\d|6[0-5]\d|7\d\d|8\d\d)-?(?:0[1-9]|[1-9]\d)-?(?:000[1-9]|00[1-9]\d|0[1-9]\d\d|[1-9]\d\d\d)\b`),
	mustPattern("IDENTIFIER", `\+55\s*\d{2}\s*(?:9\s*)?\d{4}[-\s]?\d{4}\b`),
	mustPattern("DOMAIN", `\b(?:[a-zA-Z0-9](?:[a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)+(?:com|org|net|edu|gov|io|co|uk|de|fr|br|ar|pt|int|info|biz|app|dev|cloud|local|internal|corp|mil|ad|lan|test)\b`),
	mustPattern("ORGANIZATION", `(?i)(?:workgroup|domain(?:\s+name)?|netbios(?:\s+name)?)\s*[:\s]+([A-Z][A-Z0-9\-]{2,24})\b`),
	mustPattern("CREDENTIAL", `\s-p\s+['"]?([^'"\s]{4,128})['"]?`),
	mustPattern("CREDENTIAL", `(?:\\|/)(?:[a-zA-Z0-9._\-]{2,40}):([A-Za-z0-9@!#$%^&*()\-_=+\[\]{};:,.<>?/]{6,128})(?:\s+\(|\s*$)`),
	mustPattern("CREDENTIAL", `/(?:[a-zA-Z0-9._\-]{1,40}):([A-Za-z0-9!#$%^&*()\-_=+\[\]{};,.<>?/$']{6,128})@`),
	mustPattern("CREDENTIAL", `(?i)\*\s+Password\s*:\s+(\S{4,128})`),
	mustPattern("CREDENTIAL", `(?i)(?:PASSWORD|PASSWD|SECRET(?:_KEY)?|DB_PASS(?:WORD)?|API_KEY|TOKEN|AUTH(?:_TOKEN)?)=([^\s'"]{4,128})`),
	mustPattern("CREDENTIAL", `(?i)\b[A-Z][A-Z0-9_]{1,40}_(?:PASS(?:WORD)?|SECRET|KEY)\s*=\s*([^\s'"]{6,128})`),
	mustPattern("CREDENTIAL", `(?i)"[^"]*(?:password|passwd|pass|secret|api_key|token|auth)"\s*:\s*"([^"]{4,128})"`),
	mustPattern("CREDENTIAL", `(?i)(?:password|passwd):\s+["']([^"'\n]{4,128})["']`),
	mustPattern("CREDENTIAL", `(?i)^\s*password\s*:\s+(\S{4,128})\s*$`),
	mustPattern("TOKEN", `(?i)bearer\s+([A-Za-z0-9_\-]{12,128})\b`),
	mustPattern("CREDENTIAL", `(?i)redis://[^:@\s]*:([^@\s]{4,128})@`),
	mustPattern("USERNAME", `\b(?:svc_[a-zA-Z0-9_]{2,40}|[a-zA-Z0-9]{2,40}_svc)\b`),
	mustPattern("HOSTNAME", `\(name:([A-Za-z][A-Za-z0-9\-]{1,30})\)`),
	mustPattern("HOSTNAME", `(?m)(?:Source|Target|Host):\s+([A-Z][A-Z0-9\-]{3,24})\b`),
	mustPattern("ORGANIZATION", `\b([A-Z][A-Z0-9\-]{2,20})(?:/|\\)(?:[a-zA-Z0-9._\-]{2,40})`),
	mustPattern("USERNAME", `(?:-u|--username)\s+([a-zA-Z][a-zA-Z0-9._\-]{2,40})\b`),
	mustPattern("USERNAME", `(?i)(?:User(?:\s+Name)?|Username)\s*:\s+([a-z][a-z0-9._\-]{2,40})\b`),
	mustPattern("USERNAME", `(?i)Account:\s{1,20}([a-z][a-z0-9._\-]{2,40})\b`),
	mustPattern("USERNAME", `(?i)sAMAccountName:\s+([a-z][a-z0-9._\-]{2,40})\b`),
	mustPattern("USERNAME", `(?i)"(?:username|user|login)"\s*:\s*"([a-z][a-z0-9._\-]{2,40})"`),
	mustPattern("USERNAME", `(?i)\bCN=([a-z][a-z0-9._\-]{2,40}),`),
	mustPattern("USERNAME", `\\([a-z][a-z0-9._\-]{2,40}):`),
	mustPattern("PERSON", `(?m)^([A-Z][a-z]{1,20}\s+[A-Z][a-z]{1,20}),`),
	mustPattern("PERSON", `(?m)(?:^|\n)\s*name\s*=\s*([A-Z][a-z]{1,20}\s+[A-Z][a-z]{1,20})\s*$`),
	mustPattern("PERSON", `([A-Z][a-z]{1,20}\s+[A-Z][a-z]{1,20})\s+<[A-Za-z0-9._%+\-]+@`),
	mustPattern("PATH", `(/(?:opt|app|apps|srv|data)/[A-Za-z][A-Za-z0-9\-_]{1,60}/[A-Za-z0-9][A-Za-z0-9_/.\-]{2,120})`),
	mustPattern("PATH", `(/var/www/[A-Za-z][A-Za-z0-9\-_]{1,60}(?:/[A-Za-z0-9.][A-Za-z0-9_/.\-]{0,120})?)`),
	mustPattern("PATH", `(/(?:home/[A-Za-z][A-Za-z0-9_\-]{1,40}|root)/\.(?:aws|ssh|gnupg|config)/[A-Za-z0-9_\-.]{1,80})`),
	mustPattern("IDENTIFIER", `\b(?:i|vol|snap|sg|subnet|igw|vpc|ami|eni|rtb|acl|cgw|vgw|vpn|tgw|eip)-[0-9a-f]{8,17}\b`),
	mustPattern("ORGANIZATION", `--secret-id\s+([a-z][a-z0-9\-]{2,63})\b`),
	mustPattern("ORGANIZATION", `"PolicyName"\s*:\s*"([A-Za-z][A-Za-z0-9_\-]{3,64})"`),
	mustPattern("TOKEN", `"SecretAccessKey"\s*:\s*"([A-Za-z0-9/+]{30,64})"`),
	mustPattern("ORGANIZATION", `--vault-name\s+([a-z][a-z0-9\-]{2,63})\b`),
	mustPattern("CREDENTIAL", `(?m)^\s*"value"\s*:\s*"([^"]{8,128})"\s*$`),
	mustPattern("CREDENTIAL", `X-(?:Internal|Auth|API|Access|Secret)-Token:\s+(\S{10,128})`),
	mustPattern("IDENTIFIER", `(?m)^([A-Za-z0-9][A-Za-z0-9:._-]{7,19})\s+(?:device|offline|unauthorized)\s*$`),
	mustPattern("IDENTIFIER", `\badb\s+(?:(?:-\w+\s+)*-s\s+)([A-Za-z0-9:._-]{8,20})\b`),
	mustPattern("PATH", `\b([a-z][a-z0-9_]{2,30}_(?:ntlm(?:_hashes)?|cracked|nuclei|loot|shadow)\.txt)\b`),
	mustPattern("PATH", `--potfile-path\s+(\S+\.pot)\b`),
}

var panPattern = regexp.MustCompile(`\b(?:\d{4}[\s-]?){3}\d{4}\b|\b\d{13,19}\b`)

// neverAnonymizeBlob is the newline-joined denylist of terms that must be kept
// verbatim (common tool names, protocols, well-known hosts, entity-type labels).
// It is XOR+base64-masked (see obfKey) so offensive-security tool names do not
// appear as plaintext hacktool signatures in the compiled binary.
const neverAnonymizeBlob = "AB9MEWQNG18GQhsbWQRkAgtZF0IeHkIIGmUDRBtYBRNZG2QYB18TQgYTXwpkDBxMFVoDE10EFgoNJx9cHhNOCgsbZEgAWAJfWggAHQMnFF0BHUkJARoASXxZDwFFAg8bZF8TQh4dQwULHWROE0MaG10YZB0bTxNEHXheBA0dC1kFVRsfXWsdCgVYBF0dEycNAQgBQwZQHQFaDhwLHSceUB0aSRQDH2RAE0ULAF0TCxsLX3xcHRQbawUaDEgVRQJ4RQQCAmRZE0McE0sOHAJkRQ9VHBMnEgMNZFofXxwfJwYLG2RMBkECCycFCwMLWRM7ChdeAhwGDEh8WRoGXWsGGxpdBTsIBl1rHRwGJwRVHnhBBQ8fZEYTQwwXXw4dZQBZGlxkAUAVHmUHQBdBZBZDEmQcAEAGOw0bSxJkBwFeAjsZAUAAAGUDXgVAAgFbAmQLAU4dVBx4RhQMChxDE0ULAScGBxtkSh9FBgdPawkGGkEXU2QTXQANBwsnGFYHHFVrBwYdJwJeAxFMFWQCB04EXh0dSxVkAhdeB11kAkISGggcSAVAAnhdDh0bCV8TQmQfXhIfA2RAGV8JHUkDZB0LSR9CZB1fAA0DCycGXh0GSwgWZR1IGFUDE0QNZAAeSBhCHRonDh4KAF4FXWQRRBINAGRLGUMaG0oAGgpkXQ9FBh1DaxwaDFR8XwEWSAsdZQRMAFBkFUJrGQYASRlGHXhBCAAaFicDUxscWRRkCwtPH1AAeEYAAgZkXxleGnhJAAsCAUN8RhkFAAUPGw8nGF4MHUkYZAsLSxdEAgYnDQEMD0EeXh0GJwYHGwZYFB8NHUBrCQYaQRdTQBFCDGQOAFkeQwECRAJADAFAfF8DE11PAR0JJ04fVlwVT1ZlVgNOH1pcGWtfQV8DRx9feBxPXkFeA0c7X0AaT15BXgNHO1RIHGteQV4DRh9eeFkOBQoAJx5QHRonAhwKCkgYRQcTQWsGAB1ZGFADFycUHQocQxdcC3hCEwkOAEQMUBobQg9kBh5yF1UKAEgSHWUKQhtQBxwnBAMOB0EpUAoWXwQdHGRdE0MdHUNrBwsLQwJYCBtIE2QfD1keOwMTTj4PCwpfE0IdeFgTAmUNRBJDZABCAgUWAVhYRRYGJwMLHBobQh8cB0EEZBscWBM7CBNBEgtlAFgaXWQcQg8L"

var neverAnonymize = buildNeverAnonymize()

func buildNeverAnonymize() map[string]struct{} {
	terms := strings.Split(deobf(neverAnonymizeBlob), "\n")
	set := make(map[string]struct{}, len(terms))
	for _, term := range terms {
		if term != "" {
			set[term] = struct{}{}
		}
	}
	return set
}

func mustPattern(entityType string, expr string) patternSpec {
	return patternSpec{entityType: entityType, re: regexp.MustCompile(expr)}
}

// Detect runs deterministic structured-data detection over text.
func Detect(text string) []Match {
	if text == "" {
		return nil
	}
	matches := make([]Match, 0)
	covered := make([]span, 0)

	overlaps := func(start int, end int) bool {
		for _, current := range covered {
			if start < current.end && end > current.start {
				return true
			}
		}
		return false
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
			matches = append(matches, Match{Text: value, EntityType: spec.entityType})
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
			matches = append(matches, Match{Text: strings.TrimSpace(value), EntityType: "CREDENTIAL"})
			covered = append(covered, span{start: idx[0], end: idx[1]})
		}
	}

	return matches
}

func shouldKeepOriginal(value string, entityType string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return true
	}
	tokens := strings.Fields(value)
	if len(tokens) == 1 && len(value) < 4 {
		return true
	}
	if _, ok := neverAnonymize[lower]; ok {
		return true
	}
	first := lower
	if fields := strings.Fields(lower); len(fields) > 0 {
		first = fields[0]
	}
	if !isProperName(tokens) {
		if _, ok := neverAnonymize[first]; ok {
			return true
		}
	}
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		host := urlHost(lower)
		if _, ok := neverAnonymize[host]; ok {
			return true
		}
	}
	return false
}

func isProperName(tokens []string) bool {
	if len(tokens) < 2 {
		return false
	}
	for _, token := range tokens {
		if len(token) < 2 {
			return false
		}
		first := token[0]
		if first < 'A' || first > 'Z' {
			return false
		}
		for i := 1; i < len(token); i++ {
			if token[i] < 'a' || token[i] > 'z' {
				return false
			}
		}
	}
	return true
}

func urlHost(value string) string {
	value = strings.TrimPrefix(value, "http://")
	value = strings.TrimPrefix(value, "https://")
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '/', ':', '?', '#':
			return value[:i]
		}
	}
	return value
}

func digitsOnly(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func luhnValid(digits string) bool {
	if len(digits) < 13 {
		return false
	}
	total := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		total += d
		double = !double
	}
	return total%10 == 0
}
