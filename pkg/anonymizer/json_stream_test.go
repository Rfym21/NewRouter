package anonymizer

import (
	"encoding/json"
	"strings"
	"testing"
)

func jsonTestSession() *Session {
	vault := NewVault()
	vault.byEngage["json-test"] = []Mapping{{Original: "秘密 \"line\"\npath\\folder", Surrogate: "safe-name"}}
	return NewSession("json-test", vault)
}

func TestJSONStreamRestoresEverySplitAndEscapesOriginal(t *testing.T) {
	inputs := []string{
		`{"email":"safe-name","n":9007199254740993,"literal":"another value"}`,
		`{"email":"\u0073afe\u002dname","unicode":"\ud83d\ude00","backslash":"\\"}`,
		`["prefix safe-name suffix",{"nested":"safe-name"},"中文"]`,
	}
	for _, input := range inputs {
		var original any
		if err := json.Unmarshal([]byte(input), &original); err != nil {
			t.Fatal(err)
		}
		want := jsonTestSession().DeanonymizeValue(original)
		wantJSON, _ := json.Marshal(want)
		for split := 0; split <= len(input); split++ {
			restorer := jsonTestSession().NewStreamRestorer()
			got := restorer.PushJSON("args", input[:split]) + restorer.PushJSON("args", input[split:]) + restorer.FlushJSON("args")
			var restored any
			if err := json.Unmarshal([]byte(got), &restored); err != nil {
				t.Fatalf("split %d, invalid restored JSON %q: %v", split, got, err)
			}
			gotJSON, _ := json.Marshal(restored)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("split %d, got %s, want %s", split, gotJSON, wantJSON)
			}
			if len(restorer.PendingKeys()) != 0 || len(restorer.PendingJSONKeys()) != 0 {
				t.Fatalf("split %d left buffered data", split)
			}
		}
	}
}

func TestJSONStreamByteChunksAndIndependentCalls(t *testing.T) {
	restorer := jsonTestSession().NewStreamRestorer()
	input := `{"value":"safe-name","unicode":"\ud83d\ude00中文"}`
	var first, second strings.Builder
	for i := range len(input) {
		first.WriteString(restorer.PushJSON("first", input[i:i+1]))
		second.WriteString(restorer.PushJSON("second", input[i:i+1]))
	}
	first.WriteString(restorer.FlushJSON("first"))
	second.WriteString(restorer.FlushJSON("second"))
	for _, output := range []string{first.String(), second.String()} {
		var body map[string]any
		if err := json.Unmarshal([]byte(output), &body); err != nil {
			t.Fatalf("invalid byte-chunk JSON %q: %v", output, err)
		}
		if body["value"] != "秘密 \"line\"\npath\\folder" || body["unicode"] != "😀中文" {
			t.Fatalf("unexpected restored body: %#v", body)
		}
	}
}

func TestJSONStreamFlushKeepsTruncatedEscapeAndDoesNotHoldWholeCall(t *testing.T) {
	restorer := jsonTestSession().NewStreamRestorer()
	if got := restorer.PushJSON("args", `{"value":"ordinary text `); got != `{"value":"ordinary text ` {
		t.Fatalf("ordinary argument text was delayed: %q", got)
	}
	if got := restorer.PushJSON("args", `sa`); got != "" {
		t.Fatalf("partial surrogate emitted: %q", got)
	}
	if got := restorer.PushJSON("other", `{"value":"\u00`); got != `{"value":"` {
		t.Fatalf("partial escape emitted: %q", got)
	}
	if got := restorer.FlushJSON("args"); got != "sa" {
		t.Fatalf("truncated surrogate lost: %q", got)
	}
	if got := restorer.FlushJSON("other"); got != `\u00` {
		t.Fatalf("truncated escape lost: %q", got)
	}
	var disabled *StreamRestorer
	if got := disabled.PushJSON("args", `raw \u00`); got != `raw \u00` {
		t.Fatalf("disabled JSON restoration changed data: %q", got)
	}
}
