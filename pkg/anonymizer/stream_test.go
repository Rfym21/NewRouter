package anonymizer

import (
	"strings"
	"testing"
)

func TestStreamRestorerDoesNotSplitCompleteSurrogateAtOverlappingPrefix(t *testing.T) {
	tests := []struct {
		name     string
		mappings []Mapping
		chunks   []string
		want     string
	}{
		{"self overlap", []Mapping{{Original: "private-value", Surrogate: "token-t"}}, []string{"token-", "t"}, "private-value"},
		{"email overlap", []Mapping{{Original: "alice@company.example", Surrogate: "testuser@example.pentest"}}, []string{"testuser@exam", "ple.pentest"}, "alice@company.example"},
		{"adjacent values", []Mapping{{Original: "private-value", Surrogate: "token-t"}}, []string{"token-", "ttoken-", "t"}, "private-valueprivate-value"},
		{"longer match wins", []Mapping{{Original: "short", Surrogate: "abc"}, {Original: "long", Surrogate: "abcd"}}, []string{"abc", "d "}, "long "},
		{"short match at end", []Mapping{{Original: "short", Surrogate: "abc"}, {Original: "long", Surrogate: "abcd"}}, []string{"abc"}, "short"},
		{"incomplete prefix preserved", []Mapping{{Original: "private-value", Surrogate: "token-t"}}, []string{"ordinary to", "ken"}, "ordinary token"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			vault := NewVault()
			vault.byEngage["test"] = test.mappings
			restorer := NewSession("test", vault).NewStreamRestorer()
			var got strings.Builder
			for _, chunk := range test.chunks {
				got.WriteString(restorer.Push("text", chunk))
			}
			got.WriteString(restorer.Flush("text"))
			if got.String() != test.want {
				t.Fatalf("restored text = %q, want %q", got.String(), test.want)
			}
			if len(restorer.PendingKeys()) != 0 {
				t.Fatal("completed stream still has a buffered tail")
			}
		})
	}
}
