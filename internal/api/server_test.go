package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Roomen/cpa-live-routes/internal/config"
	"github.com/Roomen/cpa-live-routes/internal/store"
	"github.com/Roomen/cpa-live-routes/internal/wire"
)

type clk struct{ t time.Time }

func (c *clk) now() time.Time { return c.t }

func newServer(t *testing.T, mut func(*config.Config)) (*Server, *clk) {
	t.Helper()
	cfg := config.Default()
	if mut != nil {
		mut(&cfg)
	}
	c := &clk{t: time.Unix(1760500000, 0)}
	st := store.New(cfg, c.now)
	return &Server{Store: st, Tokens: NewTokenStore(c.now), Now: c.now, Page: []byte("<html>page</html>")}, c
}

func get(path, host string, query map[string][]string) Request {
	h := map[string][]string{}
	if host != "" {
		h["Host"] = []string{host}
	}
	return Request{Method: "GET", Path: "/v0/resource/plugins/live-routes" + path, Headers: h, Query: query}
}

func errCode(t *testing.T, r Response) string {
	t.Helper()
	var e Error
	if err := json.Unmarshal(r.Body, &e); err != nil {
		t.Fatalf("not an error body: %s", r.Body)
	}
	return e.Code
}

func TestLoopbackGate(t *testing.T) {
	s, _ := newServer(t, nil)
	for _, host := range []string{"127.0.0.1", "127.0.0.1:8317", "localhost", "LOCALHOST:8317", "[::1]:8317", "[::1]", "::1"} {
		if r := s.Handle(get(ResourceSnapshotPath, host, nil)); r.Status != 200 {
			t.Errorf("%s: status %d", host, r.Status)
		}
	}
	for _, host := range []string{"192.168.1.5:8317", "example.com", "127.0.0.1.evil.com", "localhost.evil.com:80", "0.0.0.0"} {
		r := s.Handle(get(ResourceEventsPath, host, nil))
		if r.Status != 403 || errCode(t, r) != "forbidden_host" {
			t.Errorf("%s: %d %s", host, r.Status, r.Body)
		}
	}
	// No Host header: Origin or Referer decide, otherwise refuse.
	req := get(ResourceSnapshotPath, "", nil)
	if r := s.Handle(req); r.Status != 403 {
		t.Errorf("no headers: %d", r.Status)
	}
	req.Headers["Referer"] = []string{"http://127.0.0.1:8317/v0/resource/plugins/live-routes/live"}
	if r := s.Handle(req); r.Status != 200 {
		t.Errorf("loopback referer: %d", r.Status)
	}
	req.Headers = map[string][]string{"Origin": {"http://evil.example"}}
	if r := s.Handle(req); r.Status != 403 {
		t.Errorf("foreign origin: %d", r.Status)
	}
}

func TestPageAlwaysServed(t *testing.T) {
	s, _ := newServer(t, func(c *config.Config) { c.RequireKey = true })
	r := s.Handle(get(ResourcePagePath, "evil.example", nil))
	if r.Status != 200 || string(r.Body) != "<html>page</html>" || r.Headers["Content-Type"][0] != "text/html; charset=utf-8" ||
		r.Headers["Content-Security-Policy"][0] != pageCSP || !strings.Contains(pageCSP, "connect-src 'self'") {
		t.Fatalf("%+v", r)
	}
}

func TestRequireKeyTokens(t *testing.T) {
	s, c := newServer(t, func(cfg *config.Config) { cfg.RequireKey = true })
	r := s.Handle(get(ResourceSnapshotPath, "127.0.0.1", nil))
	if r.Status != 401 || errCode(t, r) != "unauthorized" {
		t.Fatalf("missing token -> %d %s", r.Status, r.Body)
	}
	sess := s.Handle(Request{Method: "POST", Path: "/v0/management" + ManagementSessionPath})
	var tok SessionToken
	if err := json.Unmarshal(sess.Body, &tok); err != nil || sess.Status != 200 || tok.TTLS != 43200 || tok.ExpiresAt != c.t.Add(12*time.Hour).UnixMilli() || len(tok.Token) < 40 {
		t.Fatalf("session: %+v %s", tok, sess.Body)
	}
	with := func(token string) Response {
		req := get(ResourceEventsPath, "10.1.2.3", map[string][]string{"since": {"x"}})
		req.Headers[TokenHeader] = []string{token}
		return s.Handle(req)
	}
	if r := with(tok.Token); r.Status != 200 {
		t.Fatalf("valid session rejected (%d)", r.Status)
	}
	if r := with(tok.Token + "x"); r.Status != 401 {
		t.Fatalf("bad session accepted (%d)", r.Status)
	}
	c.t = c.t.Add(12*time.Hour + time.Second)
	if r := with(tok.Token); r.Status != 401 {
		t.Fatalf("expired session accepted (%d)", r.Status)
	}
}

func TestTokenLRU(t *testing.T) {
	c := &clk{t: time.Unix(1, 0)}
	ts := NewTokenStore(c.now)
	first, _ := ts.Issue()
	var rest []string
	for i := 0; i < 31; i++ {
		tok, _ := ts.Issue()
		rest = append(rest, tok)
	}
	if !ts.Valid(first) { // touching it makes rest[0] the least recently used
		t.Fatal("first token should still be valid at 32 entries")
	}
	extra, _ := ts.Issue()
	if ts.Valid(rest[0]) {
		t.Fatal("least recently used token should have been evicted")
	}
	if !ts.Valid(first) || !ts.Valid(extra) || !ts.Valid(rest[1]) {
		t.Fatal("recent tokens must survive")
	}
	if ts.Valid("") {
		t.Fatal("empty token")
	}
}

func TestManagementRoutes(t *testing.T) {
	s, _ := newServer(t, func(c *config.Config) { c.RequireKey = true })
	r := s.Handle(Request{Method: "GET", Path: "/v0/management" + ManagementSnapshotPath})
	if r.Status != 200 || r.Headers["Content-Type"][0] != "application/json" || r.Headers["Cache-Control"][0] != "no-store" || r.Headers["X-Content-Type-Options"][0] != "nosniff" {
		t.Fatalf("snapshot: %d %+v", r.Status, r.Headers)
	}
	var snap Snapshot
	if err := json.Unmarshal(r.Body, &snap); err != nil || snap.V != ContractVersion || !snap.Config.RequireKey {
		t.Fatalf("%v %s", err, r.Body)
	}
	if r := s.Handle(Request{Method: "GET", Path: "/v0/management" + ManagementSessionPath}); r.Status != http.StatusMethodNotAllowed {
		t.Fatalf("GET session: %d", r.Status)
	}
	if r := s.Handle(get("/nope", "127.0.0.1", nil)); r.Status != 404 || errCode(t, r) != "not_found" {
		t.Fatalf("unknown: %d", r.Status)
	}
}

func TestRecordsQueryValidation(t *testing.T) {
	s, _ := newServer(t, nil)
	for _, q := range []map[string][]string{{"before": {"abc"}}, {"limit": {"0"}}, {"limit": {"x"}}} {
		if r := s.Handle(get(ResourceRecordsPath, "localhost", q)); r.Status != 400 || errCode(t, r) != "bad_request" {
			t.Errorf("%v: %d", q, r.Status)
		}
	}
	if r := s.Handle(get(ResourceRecordsPath, "localhost", map[string][]string{"limit": {"99999"}})); r.Status != 200 {
		t.Errorf("big limit: %d", r.Status)
	}
}

func TestExitLabelsFetchedOnReadOnly(t *testing.T) {
	s, c := newServer(t, nil)
	calls := 0
	var gotCB string
	s.Auths = func(cb string) ([]store.AuthEntry, error) {
		calls++
		gotCB = cb
		return []store.AuthEntry{{AuthIndex: "3", Provider: "claude", Label: "primary"}}, nil
	}
	in := &wire.Intercept{RequestID: "r", TraceID: "t", AuthIndex: "3", Model: "m"}
	s.Store.Before(in)
	s.Store.After(in)
	req := get(ResourceSnapshotPath, "localhost", nil)
	req.HostCallbackID = "cb-7"
	body := s.Handle(req).Body
	if calls != 1 || gotCB != "cb-7" || !strings.Contains(string(body), `"label":"primary"`) {
		t.Fatalf("calls=%d cb=%q body=%s", calls, gotCB, body)
	}
	s.Handle(req)
	c.t = c.t.Add(2 * time.Minute)
	s.Handle(req)
	if calls != 1 {
		t.Fatalf("labels already resolved, calls = %d", calls)
	}

	s2, _ := newServer(t, nil)
	s2.Auths = func(string) ([]store.AuthEntry, error) { return nil, errors.New("down") }
	s2.Store.After(in)
	if r := s2.Handle(req); r.Status != 200 {
		t.Fatalf("host failure must not break the page: %d", r.Status)
	}
}
