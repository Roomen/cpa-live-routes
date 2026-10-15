package redact

import (
	"strings"
	"testing"
)

func TestNewKey(t *testing.T) {
	k := NewKey("sk-ant-abcdefghijklmnop3f9a")
	if k.Mask != "sk-…3f9a" || len(k.FP) != 8 {
		t.Fatalf("%+v", k)
	}
	if short := NewKey("shortkey"); short.Mask != "key#"+short.FP {
		t.Fatalf("short mask %q", short.Mask)
	}
	p := Principal("my-plugin", strings.Repeat("u", 60))
	if len([]rune(p.Mask)) != 32 || !strings.HasPrefix(p.Mask, "my-plugin:") {
		t.Fatalf("principal mask %q", p.Mask)
	}
}

func TestKeyFromHeaders(t *testing.T) {
	cases := []struct{ auth, x, g, want string }{
		{"Bearer abc", "", "", "abc"},
		{"bearer abc", "x", "", "abc"},
		{"", "xk", "gk", "xk"},
		{"Basic zzz", "", "gk", "gk"},
		{"", "", "", ""},
	}
	for _, c := range cases {
		if got := KeyFromHeaders(c.auth, c.x, c.g); got != c.want {
			t.Errorf("%+v -> %q", c, got)
		}
	}
}

func TestLabelFor(t *testing.T) {
	k := NewKey("sk-test-key-0123456789")
	labels := map[string]string{"short": "no", strings.ToUpper(k.FP): "mine"}
	if got := LabelFor(k.Sum, labels); got != "mine" {
		t.Fatalf("got %q", got)
	}
	if got := LabelFor(k.Sum, map[string]string{k.FP[:6]: "x"}); got != "" {
		t.Fatalf("prefix under 8 chars must not match, got %q", got)
	}
}

func TestErrorText(t *testing.T) {
	in := "bad\n key  Bearer sk-ant-abcdefghijkl and AIzaSyAbcdefghijk or sk_live_abcdefgh " + strings.Repeat("x", 400)
	out := ErrorText(in)
	for _, leak := range []string{"abcdefghijkl", "SyAbcdefghijk", "live_abcdefgh", "\n", "  "} {
		if strings.Contains(out, leak) {
			t.Errorf("leaked %q in %q", leak, out)
		}
	}
	if !strings.HasPrefix(out, "bad key Bearer …") || len([]rune(out)) > 200 {
		t.Errorf("out = %q", out)
	}
	if got := ErrorText("raw secret-value-123 here", "secret-value-123"); strings.Contains(got, "secret-value") {
		t.Errorf("verbatim secret kept: %q", got)
	}
}

func TestParseUA(t *testing.T) {
	cases := []struct{ ua, kind, label string }{
		{"claude-cli/2.0.14 (external, cli)", KindClaudeCode, "Claude Code 2.0.14"},
		{"codex_cli_rs/0.46.0 (Mac OS 15.6.0; arm64) iTerm.app/3.6.1", KindCodex, "Codex 0.46.0"},
		{"codex_exec/0.50.0", KindCodex, "Codex 0.50.0"},
		{"opencode/1.0.3", KindOpenCode, "OpenCode 1.0.3"},
		{"GeminiCLI/0.8.0/gemini-2.5-pro (darwin; arm64)", KindGeminiCLI, "Gemini CLI 0.8.0"},
		{"antigravity/1.11.2 darwin/arm64", KindAntigravity, "Antigravity 1.11.2"},
		{"agy/1.0", KindAntigravity, "Antigravity 1.0"},
		{"OpenAI/Python 1.40.0", KindSDK, "OpenAI/Python"},
		{"Anthropic/JS 0.30.0", KindSDK, "Anthropic/JS"},
		{"python-requests/2.31", KindSDK, "python-requests/2.31"},
		{"curl/8.4.0", KindOther, "curl/8.4.0"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := ParseUA(c.ua); got.Kind != c.kind || got.Label != c.label {
			t.Errorf("%q -> %+v", c.ua, got)
		}
	}
	if got := ParseUA(strings.Repeat("a", 500)); len(got.Raw) != 160 {
		t.Errorf("UA not truncated: %d", len(got.Raw))
	}
}

func TestClientFromSessionAndIP(t *testing.T) {
	if k, _ := ClientFromSession("claude:abc"); k != KindClaudeCode {
		t.Fatal(k)
	}
	if k, _ := ClientFromSession("agy:abc"); k != KindAntigravity {
		t.Fatal(k)
	}
	if k, _ := ClientFromSession("nothing"); k != "" {
		t.Fatal(k)
	}
	if ip := ClientIP("203.0.113.9, 10.0.0.1", "1.1.1.1", ""); ip != "203.0.113.9" {
		t.Fatal(ip)
	}
	if ip := ClientIP("garbage", "198.51.100.2", ""); ip != "198.51.100.2" {
		t.Fatal(ip)
	}
	if ip := ClientIP("", "", "2001:db8::1"); ip != "2001:db8::1" {
		t.Fatal(ip)
	}
	if ip := ClientIP("", "", ""); ip != "" {
		t.Fatal(ip)
	}
}

func TestSessionShortAndEmail(t *testing.T) {
	cases := map[string]string{
		"claude:7b1e2c4d-5a8f":                    "7b1e2c4d",
		"claude:7b1e2c4d-5a8f:agent:explore-1":    "explore-",
		"plain":                                   "plain",
		"codex:0199a2f4-1c3b-7d55-8e21-4b6c9d0e1": "0199a2f4",
	}
	for in, want := range cases {
		if got := SessionShort(in); got != want {
			t.Errorf("%q -> %q", in, got)
		}
	}
	if MaskEmail("lisa@gmail.com") != "l***@gmail.com" || MaskEmail("nope") != "" {
		t.Error("MaskEmail")
	}
}

func TestErrorTextGenericLongTokens(t *testing.T) {
	const key = "zz9-CUSTOMKEY-q8w7e6r5t4y3u2i1"
	got := ErrorText("bad key " + key + " given")
	if strings.Contains(got, "CUSTOMKEY") || !strings.Contains(got, "bad key") {
		t.Fatalf("got %q", got)
	}
	for _, keep := range []string{
		"model claude-3-5-sonnet-20241022 not found",
		"model gpt-4o-mini-2024-07-18 is overloaded",
		"ResourceExhaustedException raised",
		"please retry the request in a few moments, internationalization",
		"https-style context_length_exceeded_error happened",
	} {
		if got := ErrorText(keep); got != keep {
			t.Errorf("plain text changed: %q -> %q", keep, got)
		}
	}
	for _, mask := range []string{"AbCdEfGhIjKlMnOpQrSt", "0123456789abcdef0123456789abcdef", "my_key_ABCDEF123456789012", "9f86d081884c7d659a2feaa0c55ad015"} {
		if got := ErrorText("x " + mask + " y"); got != "x … y" {
			t.Errorf("secret kept: %q -> %q", mask, got)
		}
	}
}

func TestErrorTextSums(t *testing.T) {
	k := NewKey("hunter2!")
	got := ErrorTextSums("wrong key hunter2!, try again; hunter2!!", [][32]byte{k.Sum})
	if strings.Contains(got, "hunter2!,") || !strings.Contains(got, "wrong key …") || !strings.Contains(got, "hunter2!!") {
		t.Fatalf("got %q", got)
	}
}
