package openai

import (
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// encryptedContentErrorMarkers are lowercased substrings that identify an upstream
// rejection caused by a Responses reasoning item whose encrypted_content cannot be
// verified by the target account/resource (cross-account or cross-resource routing).
var encryptedContentErrorMarkers = []string{
	"encrypted content",
	"encrypted_content",
	"invalid_encrypted_content",
	"could not be decrypted",
	"created under a different",
	"item was created under",
}

// IsEncryptedContentError reports whether a >= 400 response body looks like a
// Responses encrypted-content cross-resource verification failure.
func IsEncryptedContentError(respBody []byte) bool {
	if len(respBody) == 0 {
		return false
	}
	msg := strings.ToLower(string(respBody))
	for _, marker := range encryptedContentErrorMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// StripEncryptedReasoning removes reasoning items carrying encrypted_content from a
// Responses request's input array so the request can be replayed without the
// unverifiable ciphertext. It returns the rewritten body and whether anything changed.
// A non-array or absent input yields (jsonData, false).
func StripEncryptedReasoning(jsonData []byte) ([]byte, bool) {
	if len(jsonData) == 0 {
		return jsonData, false
	}
	input := gjson.GetBytes(jsonData, "input")
	if !input.IsArray() {
		return jsonData, false
	}

	// Collect indices to drop, then delete from highest to lowest to keep earlier
	// indices valid across deletions.
	var dropIdx []int
	items := input.Array()
	for i, item := range items {
		if item.Get("type").String() == "reasoning" && item.Get("encrypted_content").Exists() {
			dropIdx = append(dropIdx, i)
		}
	}
	if len(dropIdx) == 0 {
		return jsonData, false
	}

	out := jsonData
	changed := false
	for i := len(dropIdx) - 1; i >= 0; i-- {
		patched, err := sjson.DeleteBytes(out, "input."+strconv.Itoa(dropIdx[i]))
		if err != nil {
			continue
		}
		out = patched
		changed = true
	}
	return out, changed
}
