package anonymizer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func responsesTestEvent(t *testing.T, value map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestResponsesStreamArgumentsEscapeAndTextChannels(t *testing.T) {
	restorer := jsonTestSession().NewResponsesStreamRestorer()
	families := []string{"response.output_text", "response.refusal", "response.reasoning_text", "response.reasoning_summary_text", "response.custom_tool_call_input"}
	var got = make(map[string]string)
	for _, family := range families {
		for part := range 2 {
			event := map[string]any{"type": family + ".delta", "item_id": "msg", "output_index": 0, "content_index": part, "summary_index": part, "delta": "safe-"}
			messages, err := restorer.Restore(responsesTestEvent(t, event))
			if err != nil {
				t.Fatal(err)
			}
			key := family + string(rune('0'+part))
			for _, message := range messages {
				got[key] += gjson.GetBytes(message, "delta").String()
			}
		}
	}
	for _, family := range families {
		for part := range 2 {
			event := map[string]any{"type": family + ".delta", "item_id": "msg", "output_index": 0, "content_index": part, "summary_index": part, "delta": "name"}
			messages, err := restorer.Restore(responsesTestEvent(t, event))
			if err != nil {
				t.Fatal(err)
			}
			key := family + string(rune('0'+part))
			for _, message := range messages {
				got[key] += gjson.GetBytes(message, "delta").String()
			}
			if got[key] != "秘密 \"line\"\npath\\folder" {
				t.Fatalf("channel %q = %q", key, got[key])
			}
		}
	}
	arguments := map[int]string{}
	for _, delta := range []string{"{\"value\":\"\\u0073afe-", "name\"}"} {
		for outputIndex := range 2 {
			event := map[string]any{"type": "response.function_call_arguments.delta", "item_id": "tool", "output_index": outputIndex, "delta": delta}
			messages, err := restorer.Restore(responsesTestEvent(t, event))
			if err != nil {
				t.Fatal(err)
			}
			for _, message := range messages {
				arguments[outputIndex] += gjson.GetBytes(message, "delta").String()
			}
		}
	}
	for index, argument := range arguments {
		var body map[string]string
		if err := json.Unmarshal([]byte(argument), &body); err != nil {
			t.Fatalf("tool %d invalid JSON: %s: %v", index, argument, err)
		}
		if body["value"] != "秘密 \"line\"\npath\\folder" {
			t.Fatalf("tool %d = %#v", index, body)
		}
	}
}

func TestResponsesStreamFlushBeforeDoneAndTerminal(t *testing.T) {
	for _, terminal := range []string{"response.output_text.done", "response.output_item.done", "response.completed", "response.incomplete", "response.failed", "error"} {
		t.Run(terminal, func(t *testing.T) {
			restorer := jsonTestSession().NewResponsesStreamRestorer()
			delta := responsesTestEvent(t, map[string]any{"type": "response.output_text.delta", "item_id": "msg", "output_index": 1, "content_index": 2, "delta": "safe-", "sequence_number": 7})
			messages, err := restorer.Restore(delta)
			if err != nil || len(messages) != 1 {
				t.Fatalf("delta = %q: %v", messages, err)
			}
			if gjson.GetBytes(messages[0], "delta").String() != "" {
				t.Fatal("partial surrogate emitted")
			}
			done := responsesTestEvent(t, map[string]any{"type": terminal, "item_id": "msg", "sequence_number": 8, "text": "safe-name", "response": map[string]any{"usage": map[string]any{"output_tokens": 19}}})
			messages, err = restorer.Restore(done)
			if err != nil || len(messages) != 2 {
				t.Fatalf("terminal = %q: %v", messages, err)
			}
			if gjson.GetBytes(messages[0], "delta").String() != "safe-" || gjson.GetBytes(messages[0], "sequence_number").Exists() {
				t.Fatalf("invalid residual: %s", messages[0])
			}
			if gjson.GetBytes(messages[1], "sequence_number").Int() != 8 || gjson.GetBytes(messages[1], "response.usage.output_tokens").Int() != 19 {
				t.Fatal("terminal accounting changed")
			}
			if len(restorer.PendingKeys()) != 0 {
				t.Fatal("terminal left pending tail")
			}
		})
	}
}

func TestResponsesStreamEOFArgumentsAndOpaqueSnapshots(t *testing.T) {
	restorer := jsonTestSession().NewResponsesStreamRestorer()
	data := responsesTestEvent(t, map[string]any{"type": "response.function_call_arguments.delta", "item_id": "tool", "output_index": 0, "delta": "{\"value\":\"safe-"})
	messages, err := restorer.Restore(data)
	if err != nil {
		t.Fatal(err)
	}
	tail, err := restorer.Flush()
	if err != nil || len(tail) != 1 {
		t.Fatalf("flush = %q: %v", tail, err)
	}
	if got := gjson.GetBytes(messages[0], "delta").String() + gjson.GetBytes(tail[0], "delta").String(); got != "{\"value\":\"safe-" {
		t.Fatalf("truncated argument changed: %q", got)
	}
	opaque := []byte("{\"type\":\"response.completed\",\"response\":{\"output\":[{\"type\":\"reasoning\",\"encrypted_content\":\"safe-name\"},{\"type\":\"compaction\",\"encrypted_content\":\"safe-name\"}],\"usage\":{\"output_tokens\":1}}}")
	restored, err := restorer.Restore(opaque)
	if err != nil || !strings.Contains(string(restored[0]), "\"encrypted_content\":\"safe-name\"") {
		t.Fatalf("opaque state modified: %q: %v", restored, err)
	}
}
