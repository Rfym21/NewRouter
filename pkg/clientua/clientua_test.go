package clientua

import (
	"strings"
	"testing"
)

func TestClassifyBuiltinClients(t *testing.T) {
	tests := []struct {
		name      string
		userAgent string
		wantKey   string
	}{
		{"claude code", "claude-cli/2.1.197 (external, cli)", "claude_code"},
		{"claude code variant", "claude-code/1.0.0", "claude_code"},
		{"codex cli", "codex-cli/0.132.0 (Windows 10.0.26200; x64)", "codex_cli"},
		{"codex rust package", "codex_cli_rs/0.130.0", "codex_cli"},
		{"cursor", "Cursor/3.27.12 (darwin arm64)", "cursor"},
		{"gemini cli", "GeminiCLI/0.40.1/gemini-3-flash (macos; arm64; terminal) google-api-nodejs-client/9.15.1", "gemini_cli"},
		{"opencode", "opencode/1.14.28 ai-sdk/provider-utils/4.0.23 runtime/bun/1.3.13", "opencode"},
		{"windsurf", "windsurf/1.0", "windsurf"},
		{"cline", "cline/3.0.0", "cline"},
		{"roo code", "roo-code/3.45.0 (darwin; arm64) node/22.21.1", "roo_code"},
		{"kilo code", "kilo-code/5.1.0 (darwin; arm64) node/22.21.1", "kilo_code"},
		{"openclaw", "openclaw/2026.8.2 (darwin; node/26.0.1; arm64; gateway)", "openclaw"},
		{"openclaw gateway", "OpenClaw-Gateway/1.0", "openclaw"},
		{"newapi custom", "New-API/1.0 (+https://example.com)", "newapi"},
		{"magpie", "magpie/1.0", "magpie"},
		{"openai sdk", "OpenAI/Python 1.99.1", "openai_sdk"},
		{"anthropic sdk", "Anthropic/Python 0.49.0", "anthropic_sdk"},
		{"google genai", "google-genai-sdk/1.0 gl-python/3.12", "google_genai_sdk"},
		{"langchain", "langchain/0.3.0", "langchain"},
		{"go net/http", "Go-http-client/1.1", "go_http"},
		{"fasthttp", "fasthttp", "fasthttp"},
		{"axios", "axios/1.8.4", "axios"},
		{"python requests", "python-requests/2.32.3", "python_requests"},
		{"python httpx", "python-httpx/0.28.1", "python_httpx"},
		{"curl", "curl/8.7.1", "curl"},
		{"postman", "PostmanRuntime/7.39.0", "postman"},
		{"chrome", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", "chrome"},
		{"edge", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0", "edge"},
		{"safari", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15", "safari"},
		{"chatgpt desktop", "ChatGPT/1.2026.133 (Windows_NT 10.0.26200; x86_64; build ) Electron/39.2.7 Chrome/142.0.0.0", "chatgpt_desktop"},
		{"claude desktop", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Claude/1.11187.1 Chrome/146.0.7680.216 Electron/41.6.1 Safari/537.36", "claude_desktop"},
		{"cherry studio", "CherryStudio/1.5.11", "cherry_studio"},
		{"lobehub", "lobehub/1.0.0", "lobehub"},
		{"librechat", "LibreChat/1.0", "librechat"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.userAgent, nil)
			if got.Key != tc.wantKey {
				t.Fatalf("Classify(%q).Key = %q, want %q (name %q)", tc.userAgent, got.Key, tc.wantKey, got.Name)
			}
			if got.Icon == "" {
				t.Fatalf("Classify(%q) returned no icon", tc.userAgent)
			}
		})
	}
}

func TestClassifyCustomRulesWin(t *testing.T) {
	rules := []CustomRule{
		{Name: "Internal Claude", Keywords: []string{"claude-cli/"}},
		{Name: "My App", Keywords: []string{"my-app/"}, Icon: "lu:Box"},
	}
	got := Classify("claude-cli/2.1.197 (external, cli)", rules)
	if got.Key != "custom" || got.Name != "Internal Claude" || !got.Custom {
		t.Fatalf("custom rule did not win: %#v", got)
	}
	if got.Category != string(CategoryCustom) {
		t.Fatalf("custom category = %q", got.Category)
	}
	got = Classify("my-app/1.0", rules)
	if got.Name != "My App" || got.Icon != "lu:Box" {
		t.Fatalf("custom icon/name = %#v", got)
	}
}

func TestClassifyEmptyAndUnknown(t *testing.T) {
	empty := Classify("   ", nil)
	if empty.Key != "unknown" || empty.Name != "" {
		t.Fatalf("empty UA = %#v", empty)
	}
	unknown := Classify("custom-client/1.0", nil)
	if unknown.Key != "other" || unknown.Name != "Other" {
		t.Fatalf("unknown UA = %#v", unknown)
	}
}

func TestParseCustomRules(t *testing.T) {
	rules, err := ParseCustomRules(`[
		{"name":"My App","keywords":["my-app/","myapp"],"icon":"lu:Box"},
		{"name":"Dup","keywords":["a","A","b"]}
	]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 || len(rules[1].Keywords) != 2 {
		t.Fatalf("normalized rules = %#v", rules)
	}
	if rules[0].Icon != "lu:Box" || rules[0].Name != "My App" {
		t.Fatalf("first rule = %#v", rules[0])
	}

	for _, raw := range []string{
		``,
		`null`,
		`[]`,
	} {
		if _, err := ParseCustomRules(raw); err != nil {
			t.Fatalf("ParseCustomRules(%q): %v", raw, err)
		}
	}

	for _, raw := range []string{
		`{}`,
		`[{"name":"","keywords":["x"]}]`,
		`[{"name":"A","keywords":[]}]`,
		`[{"name":"A","keywords":[""]}]`,
		`[{"name":"A","keywords":["x"],"extra":1}]`,
		`[{"name":"A","keywords":["x"]}, {"name":"a","keywords":["y"]}]`,
		`[{"name":"A","keywords":["x"]}] trailing`,
	} {
		if _, err := ParseCustomRules(raw); err == nil {
			t.Fatalf("ParseCustomRules(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestBuiltinRulesAreUsable(t *testing.T) {
	if len(builtinRules) < 180 {
		t.Fatalf("builtin rule count = %d, want at least 180", len(builtinRules))
	}
	keys := make(map[string]struct{}, len(builtinRules))
	for _, rule := range builtinRules {
		if rule.key == "" || rule.name == "" || rule.icon == "" {
			t.Fatalf("incomplete rule: %#v", rule)
		}
		if _, exists := keys[rule.key]; exists {
			t.Fatalf("duplicate rule key %q", rule.key)
		}
		keys[rule.key] = struct{}{}
		if len(rule.keywords) == 0 {
			t.Fatalf("rule %q has no keywords", rule.key)
		}
		if !strings.HasPrefix(rule.icon, "lobe:") &&
			!strings.HasPrefix(rule.icon, "si:") &&
			!strings.HasPrefix(rule.icon, "lu:") {
			t.Fatalf("rule %q has unsupported icon %q", rule.key, rule.icon)
		}
		for _, keyword := range rule.keywords {
			if keyword == "" || keyword != strings.ToLower(keyword) {
				t.Fatalf("rule %q has unnormalized keyword %q", rule.key, keyword)
			}
		}
	}
}
