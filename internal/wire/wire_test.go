package wire

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func interceptJSON(t testing.TB, bodySize int, hdr map[string][]string, meta any) []byte {
	t.Helper()
	body := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xA5, 0x5A, 0x01}, bodySize/4))
	var b bytes.Buffer
	enc := func(v any) []byte {
		r, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	b.WriteString(`{"RequestID":"req-1","TraceID":"abcd1234","SourceFormat":"openai","ToFormat":"claude","Model":"claude-opus-4-1","RequestedModel":"opus","Stream":true,"Headers":`)
	b.Write(enc(hdr))
	b.WriteString(`,"Body":"` + body + `","Metadata":`)
	b.Write(enc(meta))
	b.WriteString(`,"host_callback_id":"cb-9"}`)
	return b.Bytes()
}

var testHeaders = map[string][]string{
	"User-Agent":      {"claude-cli/2.0.14 (external, cli)"},
	"Authorization":   {"Bearer sk-secret-1234567890"},
	"X-Forwarded-For": {"203.0.113.9, 10.0.0.1"},
}

var testMeta = map[string]any{
	"selected_auth_id": "a.json", "selected_auth_index": "3", "canonical_session_id": "claude:7b1e", "parent_session_id": "claude:0000", "other": map[string]any{"x": 1},
}

func check(t *testing.T, in Intercept) {
	t.Helper()
	if in.RequestID != "req-1" || in.TraceID != "abcd1234" || in.Model != "claude-opus-4-1" || in.RequestedModel != "opus" || !in.Stream ||
		in.Headers.UA != "claude-cli/2.0.14 (external, cli)" || in.Headers.Authorization != "Bearer sk-secret-1234567890" || in.Headers.XForwardedFor != "203.0.113.9, 10.0.0.1" ||
		in.AuthIndex != "3" || in.SessionID != "claude:7b1e" || in.ParentSession != "claude:0000" || in.HostCallbackID != "cb-9" {
		t.Fatalf("parsed: %+v", in)
	}
}

func TestParseInterceptFast(t *testing.T) {
	data := interceptJSON(t, 4096, testHeaders, testMeta)
	if _, ok := parseFast(data); !ok {
		t.Fatal("fast path rejected a regular payload")
	}
	in, err := ParseIntercept(data)
	if err != nil {
		t.Fatal(err)
	}
	check(t, in)
}

func TestParseInterceptTricky(t *testing.T) {
	hdr := map[string][]string{
		"Body":            {"x"},
		"User-Agent":      {"claude-cli/2.0.14 (external, cli)"},
		"Authorization":   {"Bearer sk-secret-1234567890"},
		"X-Forwarded-For": {"203.0.113.9, 10.0.0.1"},
		"X-Note":          {`has "Body": and \" quotes`},
	}
	in, err := ParseIntercept(interceptJSON(t, 64, hdr, testMeta))
	if err != nil {
		t.Fatal(err)
	}
	check(t, in)

	nullBody := `{"RequestID":"req-1","TraceID":"abcd1234","Model":"claude-opus-4-1","RequestedModel":"opus","Stream":true,"Headers":null,"Body":null,"Metadata":{"selected_auth_index":"3"},"host_callback_id":"cb-9"}`
	in, err = ParseIntercept([]byte(nullBody))
	if err != nil || in.AuthIndex != "3" || in.RequestID != "req-1" {
		t.Fatalf("null body: %+v %v", in, err)
	}

	odd := `{"RequestID":"req-1","Headers":{},"Body":"AAAA","Metadata":{"selected_auth_index":5,"canonical_session_id":"s"}}`
	in, err = ParseIntercept([]byte(odd))
	if err != nil || in.AuthIndex != "" || in.SessionID != "s" {
		t.Fatalf("non-string metadata: %+v %v", in, err)
	}
}

func TestParseInterceptInvalid(t *testing.T) {
	for _, s := range []string{"", "{", "not json", `{"RequestID":"x","Body":"abc`, `[1,2]`, strings.Repeat("x", 100000)} {
		if _, err := ParseIntercept([]byte(s)); err == nil {
			t.Errorf("expected an error for %.20q", s)
		}
	}
}

func TestParseUsage(t *testing.T) {
	raw := `{"RequestID":"u","TraceID":"t","Provider":"claude","BaseURL":"https://secret.example","Model":"m","Alias":"a","APIKey":
"k","IsNativeKey":true,
	"RequestedAt":"2025-10-15T03:46:30.123456789Z","Latency":12300000000,"TTFT":1450000000,"Failed":true,"Failure":{"StatusCode":429,"Body":"slow down"},
	"Detail":{"InputTokens":5,"OutputTokens":6,"CachedTokens":7,"TotalTokens":11},"ResponseHeaders":{"Retry-After":["30"],"X-Other":["1"]}}`
	u, err := ParseUsage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if u.RequestID != "u" || u.Latency.Milliseconds() != 12300 || u.TTFT.Milliseconds() != 1450 || u.Failure.StatusCode != 429 ||
		u.Detail.TotalTokens != 11 || u.ResponseHeaders.RetryAfter[0] != "30" {
		t.Fatalf("%+v", u)
	}
	if u.RequestedAt.Year() != 2025 {
		t.Fatalf("time: %v", u.RequestedAt)
	}
}

func TestParseCompletionLifecycleManagement(t *testing.T) {
	c, err := ParseCompletion([]byte(`{"RequestID":"r","Outcome":"failed","StatusCode":502,"Error":"e","StartedAt":"2025-10-15T03:46:30Z","Metadata":{"selected_auth_index":"4"}}`))
	if err != nil || c.Outcome != "failed" || c.StatusCode != 502 || c.AuthIndex() != "4" {
		t.Fatalf("%+v %v", c, err)
	}
	l, err := ParseLifecycle([]byte(`{"config_yaml":"` + base64.StdEncoding.EncodeToString([]byte("layout: split")) + `","schema_version":6}`))
	if err != nil || string(l.ConfigYAML) != "layout: split" || l.SchemaVersion != 6 {
		t.Fatalf("%+v %v", l, err)
	}
	m, err := ParseManagement([]byte(`{"Method":"GET","Path":"/v0/management/live-routes/events","Headers":{"Host":["127.0.0.1:8317"]},"Query":{"since":["a:1"]},"Body":null,"host_callback_id":"cb"}`))
	if err != nil || m.Headers["Host"][0] != "127.0.0.1:8317" || m.Query["since"][0] != "a:1" || m.HostCallbackID != "cb" {
		t.Fatalf("%+v %v", m, err)
	}
}

func BenchmarkInterceptParse(b *testing.B) {
	data := interceptJSON(b, 2<<20, testHeaders, testMeta)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ParseIntercept(data); err != nil {
			b.Fatal(err)
		}
	}
}
