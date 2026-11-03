package anonymizer

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"new-router/common"
)

// GeminiStreamRestorer restores protocol text, tool values and partial argument
// strings without merging separate candidates/parts or changing signed parts.
// The stream and call identities belong to one response, never a global cache.
type GeminiStreamRestorer struct {
	session *Session
	stream  *StreamRestorer
	calls   map[string]geminiCallSlot
}

func (s *Session) NewGeminiStreamRestorer() *GeminiStreamRestorer {
	return NewGeminiStreamRestorer(s, nil)
}

// NewGeminiStreamRestorer also accepts an existing carry buffer for adaptors
// that already maintain it. A nil session yields a nil-safe passthrough helper.
func NewGeminiStreamRestorer(session *Session, stream *StreamRestorer) *GeminiStreamRestorer {
	if session == nil {
		return nil
	}
	if stream == nil {
		stream = session.NewStreamRestorer()
	}
	return &GeminiStreamRestorer{session: session, stream: stream, calls: make(map[string]geminiCallSlot)}
}

func (r *GeminiStreamRestorer) Restore(data []byte) ([][]byte, error) {
	if r == nil {
		return [][]byte{data}, nil
	}
	if !ojson.ValidBytes(data) {
		return nil, fmt.Errorf("invalid Gemini stream JSON")
	}
	out, err := r.restoreChunk(string(data))
	if err != nil {
		return nil, err
	}
	return [][]byte{[]byte(out)}, nil
}

// Flush emits held tails before finalization, including abnormal EOF. Callers
// have already consumed upstream usage; these client fragments are not rebilled.
func (r *GeminiStreamRestorer) Flush() ([][]byte, error) {
	if r == nil {
		return nil, nil
	}
	keys := r.stream.PendingKeys()
	return r.flushKeys(keys)
}

// A function can have several pending JSONPaths. Flush them as one call so a
// receiver does not complete/restart the same function once per argument.
func (r *GeminiStreamRestorer) flushKeys(keys []string) ([][]byte, error) {
	sort.Strings(keys)
	var output [][]byte
	type argumentTail struct {
		key       string
		arguments []any
	}
	groups := make(map[string]*argumentTail)
	for _, key := range keys {
		residual := r.stream.Flush(key)
		if residual == "" {
			continue
		}
		parts := strings.SplitN(key, ":", 3)
		if len(parts) == 3 {
			if slot, ok := decodeGeminiPartialSlot(parts[2]); ok {
				groupKey := parts[0] + ":" + parts[1] + "\x00" + slot.ID + "\x00" + slot.Name
				group := groups[groupKey]
				if group == nil {
					group = &argumentTail{key: key}
					groups[groupKey] = group
				}
				group.arguments = append(group.arguments, map[string]any{"jsonPath": slot.Path, "stringValue": residual, "willContinue": false})
				continue
			}
		}
		if body, ok := BuildGeminiNativeResidual(key, residual); ok {
			output = append(output, []byte(body))
		}
	}
	groupKeys := make([]string, 0, len(groups))
	for key := range groups {
		groupKeys = append(groupKeys, key)
	}
	sort.Strings(groupKeys)
	for _, key := range groupKeys {
		group := groups[key]
		body, ok := BuildGeminiNativeResidual(group.key, "")
		if !ok {
			continue
		}
		parts := strings.SplitN(group.key, ":", 3)
		body, err := ojson.Set(body, "candidates.0.content.parts."+parts[1]+".functionCall.partialArgs", group.arguments)
		if err != nil {
			return nil, err
		}
		output = append(output, []byte(body))
	}
	return output, nil
}

type geminiCallSlot struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

func (r *GeminiStreamRestorer) restoreChunk(data string) (string, error) {
	restorer, session := r.stream, r.session
	if restorer == nil {
		return data, nil
	}
	if session == nil {
		return data, nil
	}
	candidates := ojson.Get(data, "candidates")
	if !candidates.IsArray() {
		restored, err := DeanonymizeTextFields([]byte(data), session)
		return string(restored), err
	}
	out := data
	for ci, cand := range candidates.Array() {
		idx := ci
		if v := cand.Get("index"); v.Exists() {
			idx = int(v.Int())
		}
		parts := cand.Get("content.parts")
		for pi, part := range parts.Array() {
			// The signature covers this part's content, including tool arguments.
			if signature := part.Get("thoughtSignature"); signature.Exists() && signature.String() != "" {
				continue
			}
			if signature := part.Get("thought_signature"); signature.Exists() && signature.String() != "" {
				continue
			}
			partPath := "candidates." + strconv.Itoa(ci) + ".content.parts." + strconv.Itoa(pi)
			var partialErr error
			out, partialErr = r.restorePartialArguments(out, part, partPath, idx, pi)
			if partialErr != nil {
				return data, partialErr
			}
			for _, fieldPath := range []string{"functionCall.args", "functionResponse.response"} {
				value := part.Get(fieldPath)
				if !value.Exists() {
					continue
				}
				var decoded any
				if err := common.UnmarshalWithNumber([]byte(value.Raw), &decoded); err != nil {
					return data, err
				}
				encoded, err := common.Marshal(session.DeanonymizeValue(decoded))
				if err != nil {
					return data, err
				}
				patched, err := ojson.SetRawBytes([]byte(out), partPath+"."+fieldPath, encoded)
				if err != nil {
					return data, err
				}
				out = string(patched)
			}
			tv := part.Get("text")
			if tv.Type != ojson.String {
				continue
			}
			field := "content"
			if part.Get("thought").Bool() {
				field = "reasoning"
			}
			flushed := restorer.Push(strconv.Itoa(idx)+":"+strconv.Itoa(pi)+":"+field, tv.String())
			path := partPath + ".text"
			if patched, err := ojson.Set(out, path, flushed); err == nil {
				out = patched
			}
		}
		// Complete held parts before this candidate's terminal frame is sent.
		if reason := cand.Get("finishReason").String(); reason != "" && reason != "FINISH_REASON_UNSPECIFIED" {
			prefix := strconv.Itoa(idx) + ":"
			var keys []string
			for _, key := range restorer.PendingKeys() {
				if strings.HasPrefix(key, prefix) {
					keys = append(keys, key)
				}
			}
			tails, err := r.flushKeys(keys)
			if err != nil {
				return data, err
			}
			partsPath := "candidates." + strconv.Itoa(ci) + ".content.parts"
			for _, tail := range tails {
				for pi, part := range ojson.GetBytes(tail, "candidates.0.content.parts").Array() {
					if part.Get("functionCall").Exists() {
						index := len(ojson.Get(out, partsPath).Array())
						patched, setErr := ojson.SetRawBytes([]byte(out), partsPath+"."+strconv.Itoa(index), []byte(part.Raw))
						if setErr != nil {
							return data, setErr
						}
						out = string(patched)
						continue
					}
					if text := part.Get("text"); text.Type == ojson.String {
						path := partsPath + "." + strconv.Itoa(pi)
						out, _ = ojson.Set(out, path+".text", ojson.Get(out, path+".text").String()+text.String())
						if part.Get("thought").Bool() {
							out, _ = ojson.Set(out, path+".thought", true)
						}
					}
				}
			}
		}

	}
	return out, nil
}

func BuildGeminiNativeResidual(key string, residual string) (string, bool) {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 {
		return "", false
	}
	idx, err := strconv.Atoi(parts[0])
	if err != nil {
		return "", false
	}
	partIndex, err := strconv.Atoi(parts[1])
	if err != nil || partIndex < 0 {
		return "", false
	}
	field := parts[2]
	chunk := `{"candidates":[{"content":{"parts":[{}],"role":"model"},"index":0}]}`
	chunk, _ = ojson.Set(chunk, "candidates.0.index", idx)
	path := "candidates.0.content.parts." + strconv.Itoa(partIndex)
	if slot, ok := decodeGeminiPartialSlot(field); ok {
		chunk, _ = ojson.Set(chunk, path+".functionCall", geminiPartialResidualCall(slot, residual))
		return chunk, true
	}
	if field != "content" && field != "reasoning" {
		return "", false
	}
	chunk, _ = ojson.Set(chunk, path+".text", residual)
	if field == "reasoning" {
		chunk, _ = ojson.Set(chunk, path+".thought", true)
	}
	return chunk, true
}

func (r *GeminiStreamRestorer) restorePartialArguments(out string, part ojson.Result, partPath string, candidateIndex, partIndex int) (string, error) {
	restorer := r.stream
	if restorer == nil || !part.Get("functionCall").Exists() {
		return out, nil
	}
	base := strconv.Itoa(candidateIndex) + ":" + strconv.Itoa(partIndex)
	call := part.Get("functionCall")
	slot := r.calls[base]
	if id := call.Get("id").String(); id != "" {
		if slot.ID != "" && id != slot.ID {
			slot = geminiCallSlot{}
		}
		slot.ID = id
	}
	if name := call.Get("name").String(); name != "" {
		slot.Name = name
	}
	r.calls[base] = slot
	for index, argument := range call.Get("partialArgs").Array() {
		value := argument.Get("stringValue")
		if value.Type != ojson.String {
			continue
		}
		argumentSlot := slot
		argumentSlot.Path = argument.Get("jsonPath").String()
		metadata, err := common.Marshal(argumentSlot)
		if err != nil {
			return out, err
		}
		key := base + ":partial:" + base64.RawURLEncoding.EncodeToString(metadata)
		text := restorer.Push(key, value.String())
		partDone := argument.Get("willContinue")
		callDone := call.Get("willContinue")
		if (partDone.Exists() && !partDone.Bool()) || (callDone.Exists() && !callDone.Bool()) {
			text += restorer.Flush(key)
		}
		out, err = ojson.Set(out, partPath+".functionCall.partialArgs."+strconv.Itoa(index)+".stringValue", text)
		if err != nil {
			return out, err
		}
	}
	if callDone := call.Get("willContinue"); callDone.Exists() && !callDone.Bool() {
		prefix := base + ":partial:"
		for _, key := range restorer.PendingKeys() {
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			pendingSlot, ok := decodeGeminiPartialSlot(strings.TrimPrefix(key, base+":"))
			if !ok || pendingSlot.ID != slot.ID || pendingSlot.Name != slot.Name {
				continue
			}
			residual := restorer.Flush(key)
			if residual == "" {
				continue
			}
			argsPath := partPath + ".functionCall.partialArgs"
			index := len(ojson.Get(out, argsPath).Array())
			argument := map[string]any{"jsonPath": pendingSlot.Path, "stringValue": residual, "willContinue": false}
			var err error
			out, err = ojson.Set(out, argsPath+"."+strconv.Itoa(index), argument)
			if err != nil {
				return out, err
			}
		}
	}
	return out, nil
}

func decodeGeminiPartialSlot(field string) (geminiCallSlot, bool) {
	var slot geminiCallSlot
	encoded, ok := strings.CutPrefix(field, "partial:")
	if !ok {
		return slot, false
	}
	metadata, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || common.Unmarshal(metadata, &slot) != nil {
		return slot, false
	}
	return slot, true
}

func geminiPartialResidualCall(slot geminiCallSlot, residual string) map[string]any {
	return map[string]any{
		"id": slot.ID, "name": slot.Name, "args": map[string]any{}, "willContinue": false,
		"partialArgs": []any{map[string]any{"jsonPath": slot.Path, "stringValue": residual, "willContinue": false}},
	}
}
