package anonymizer

import (
	"sort"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ResponsesStreamRestorer applies the same protocol-aware restoration to HTTP
// SSE, WebSocket frames, and Responses events synthesized by other adaptors.
// Each logical output part has its own short text/JSON carry buffer.
type ResponsesStreamRestorer struct {
	session *Session
	stream  *StreamRestorer
	pending map[string][]byte
}

func (s *Session) NewResponsesStreamRestorer() *ResponsesStreamRestorer {
	if s == nil {
		return nil
	}
	return &ResponsesStreamRestorer{session: s, stream: s.NewStreamRestorer(), pending: make(map[string][]byte)}
}

func responsesDeltaFamily(eventType string) string {
	family := strings.TrimSuffix(strings.TrimSuffix(eventType, ".delta"), ".done")
	switch family {
	case "response.output_text", "response.refusal", "response.reasoning_text", "response.reasoning_summary_text",
		"response.function_call_arguments", "response.custom_tool_call_input":
		return family
	default:
		return ""
	}
}

func responsesEventKey(data []byte, family string) string {
	output := gjson.GetBytes(data, "output_index")
	slot := "item:" + gjson.GetBytes(data, "item_id").String()
	if output.Exists() {
		slot = "output:" + strconv.FormatInt(output.Int(), 10)
	}
	partField := "content_index"
	if family == "response.reasoning_summary_text" {
		partField = "summary_index"
	}
	part := gjson.GetBytes(data, partField).Int()
	return family + "\x00" + slot + "\x00" + strconv.FormatInt(part, 10)
}

// Restore returns residual events followed by the current event.
// Full snapshots are restored separately; synthesized residuals never reuse an
// upstream sequence_number or change usage/accounting metadata.
func (r *ResponsesStreamRestorer) Restore(data []byte) ([][]byte, error) {
	if r == nil {
		return [][]byte{data}, nil
	}
	eventType := gjson.GetBytes(data, "type").String()
	family := responsesDeltaFamily(eventType)
	if family != "" && strings.HasSuffix(eventType, ".delta") {
		delta := gjson.GetBytes(data, "delta")
		if delta.Type == gjson.String {
			key := responsesEventKey(data, family)
			template, err := sjson.SetBytes(data, "delta", "")
			if err != nil {
				return nil, err
			}
			r.pending[key] = template
			var restored string
			if family == "response.function_call_arguments" {
				restored = r.stream.PushJSON(key, delta.String())
			} else {
				restored = r.stream.Push(key, delta.String())
			}
			patched, err := sjson.SetBytes(data, "delta", restored)
			return [][]byte{patched}, err
		}
	}
	var output [][]byte
	var err error
	if family != "" && strings.HasSuffix(eventType, ".done") {
		output, err = r.flushMatching(data, family)
	} else {
		switch eventType {
		case "response.output_item.done", "response.content_part.done", "response.reasoning_summary_part.done":
			output, err = r.flushMatching(data, "")
		case "response.completed", "response.done", "response.incomplete", "response.failed",
			"response.cancelled", "response.canceled", "response.error", "error":
			output, err = r.Flush()
		}
	}
	if err != nil {
		return nil, err
	}
	restored, err := DeanonymizeTextFields(data, r.session)
	if err != nil {
		return nil, err
	}
	return append(output, restored), nil
}

func (r *ResponsesStreamRestorer) flushMatching(event []byte, family string) ([][]byte, error) {
	var keys []string
	for key, template := range r.pending {
		if family != "" && !strings.HasPrefix(key, family+"\x00") {
			continue
		}
		matches := true
		for _, field := range []string{"item_id", "output_index", "content_index", "summary_index"} {
			wanted := gjson.GetBytes(event, field)
			if field == "item_id" && !wanted.Exists() {
				wanted = gjson.GetBytes(event, "item.id")
			}
			actual := gjson.GetBytes(template, field)
			if wanted.Exists() && actual.Exists() && wanted.String() != actual.String() {
				matches = false
				break
			}
		}
		if matches {
			keys = append(keys, key)
		}
	}
	return r.flushKeys(keys)
}

func (r *ResponsesStreamRestorer) Flush() ([][]byte, error) {
	if r == nil {
		return nil, nil
	}
	keys := make([]string, 0, len(r.pending))
	for key := range r.pending {
		keys = append(keys, key)
	}
	return r.flushKeys(keys)
}

func (r *ResponsesStreamRestorer) PendingKeys() []string {
	if r == nil {
		return nil
	}
	keys := append(r.stream.PendingKeys(), r.stream.PendingJSONKeys()...)
	sort.Strings(keys)
	return keys
}

func (r *ResponsesStreamRestorer) flushKeys(keys []string) ([][]byte, error) {
	sort.Strings(keys)
	var output [][]byte
	for _, key := range keys {
		template := r.pending[key]
		delete(r.pending, key)
		var residual string
		if strings.HasPrefix(key, "response.function_call_arguments\x00") {
			residual = r.stream.FlushJSON(key)
		} else {
			residual = r.stream.Flush(key)
		}
		if residual == "" {
			continue
		}
		body, err := sjson.SetBytes(template, "delta", residual)
		if err != nil {
			return nil, err
		}
		body, err = sjson.DeleteBytes(body, "sequence_number")
		if err != nil {
			return nil, err
		}
		output = append(output, body)
	}
	return output, nil
}
