package anonymizer

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// jsonStreamState keeps only incomplete JSON escapes and surrogate prefixes.
// Arguments are streamed as JSON source text after the outer SSE envelope has
// been decoded. Restored originals must be quoted for that inner JSON too.
type jsonStreamState struct {
	text     *StreamRestorer
	inString bool
	pending  string
}

// PushJSON restores JSON argument fragments without buffering the whole call.
// JSON syntax and numbers are preserved; string contents are decoded before
// restoration and re-escaped afterward, including split Unicode escapes.
func (r *StreamRestorer) PushJSON(key, fragment string) string {
	if r == nil {
		return fragment
	}
	if r.jsonStreams == nil {
		r.jsonStreams = make(map[string]*jsonStreamState)
	}
	state := r.jsonStreams[key]
	if state == nil {
		state = &jsonStreamState{text: &StreamRestorer{
			session: r.session, surrogates: r.surrogates, maxLen: r.maxLen,
			replacer: r.replacer, trie: r.trie,
			starts: r.starts, singleOriginal: r.singleOriginal,
			carry: make(map[string]string),
		}}
		r.jsonStreams[key] = state
	}
	data := state.pending + fragment
	state.pending = ""
	var output, decoded strings.Builder
	flushDecoded := func() {
		if decoded.Len() > 0 {
			output.WriteString(quoteJSONFragment(state.text.Push("string", decoded.String())))
			decoded.Reset()
		}
	}
	for offset := 0; offset < len(data); {
		if !state.inString {
			output.WriteByte(data[offset])
			if data[offset] == '"' {
				state.inString = true
			}
			offset++
			continue
		}
		if data[offset] == '"' {
			flushDecoded()
			output.WriteString(quoteJSONFragment(state.text.Flush("string")))
			output.WriteByte('"')
			state.inString = false
			offset++
			continue
		}
		if data[offset] == '\\' {
			value, consumed, complete := decodeJSONEscape(data[offset:])
			if !complete {
				state.pending = data[offset:]
				break
			}
			if consumed == 0 {
				// Malformed upstream JSON remains malformed rather than having
				// an escape silently dropped or turning into different syntax.
				flushDecoded()
				output.WriteString(quoteJSONFragment(state.text.Flush("string")))
				output.WriteByte(data[offset])
				offset++
				continue
			}
			decoded.WriteString(value)
			offset += consumed
			continue
		}
		if data[offset] >= utf8.RuneSelf && !utf8.FullRuneInString(data[offset:]) {
			state.pending = data[offset:]
			break
		}
		_, size := utf8.DecodeRuneInString(data[offset:])
		decoded.WriteString(data[offset : offset+size])
		offset += size
	}
	flushDecoded()
	return output.String()
}

// FlushJSON emits an incomplete argument tail without inventing missing JSON
// delimiters. Call it before the call's terminal event and at abnormal EOF.
func (r *StreamRestorer) FlushJSON(key string) string {
	if r == nil || r.jsonStreams == nil {
		return ""
	}
	state := r.jsonStreams[key]
	if state == nil {
		return ""
	}
	delete(r.jsonStreams, key)
	return quoteJSONFragment(state.text.Flush("string")) + state.pending
}

func (r *StreamRestorer) PendingJSONKeys() []string {
	if r == nil {
		return nil
	}
	var keys []string
	for key, state := range r.jsonStreams {
		if state.pending != "" || len(state.text.PendingKeys()) > 0 {
			keys = append(keys, key)
		}
	}
	return keys
}

func quoteJSONFragment(text string) string {
	if text == "" {
		return ""
	}
	encoded, _ := json.Marshal(text)
	return string(encoded[1 : len(encoded)-1])
}

func decodeJSONEscape(data string) (string, int, bool) {
	if len(data) < 2 {
		return "", 0, false
	}
	switch data[1] {
	case '"', '\\', '/':
		return string(data[1]), 2, true
	case 'b':
		return "\b", 2, true
	case 'f':
		return "\f", 2, true
	case 'n':
		return "\n", 2, true
	case 'r':
		return "\r", 2, true
	case 't':
		return "\t", 2, true
	case 'u':
		if len(data) < 6 {
			return "", 0, false
		}
		first, err := strconv.ParseUint(data[2:6], 16, 16)
		if err != nil {
			return "", 0, true
		}
		value := rune(first)
		if value >= 0xd800 && value <= 0xdbff {
			if len(data) < 12 && (len(data) == 6 || data[6] == '\\' && (len(data) == 7 || data[7] == 'u')) {
				return "", 0, false
			}
			if len(data) >= 12 && data[6:8] == "\\u" {
				second, err := strconv.ParseUint(data[8:12], 16, 16)
				if err == nil && second >= 0xdc00 && second <= 0xdfff {
					return string(utf16.DecodeRune(value, rune(second))), 12, true
				}
			}
		}
		if utf16.IsSurrogate(value) {
			value = utf8.RuneError
		}
		return string(value), 6, true
	default:
		return "", 0, true
	}
}
