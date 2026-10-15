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
	return &Server{Store: st, Page: []byte("<html>page</html>")}, c
}

func get(path, host string, query map[string][]string) Request {
	h := map[string][]string{}
	if host != "" {
		h["Host"] = []string{host}
	}
	return Request{Method: "GET", Path: "/v0/management" + path, Headers: h, Query: query}
}

func errCode(t *testing.T, r Response) string {
	t.Helper()
	var e Error
	if err := json.Unmarshal(r.Body, &e); err != nil {
		t.Fatalf("not an error body: %s", r.Body)
	}
	return e.Code
}

// Even forged loopback headers and old view tokens must never expose data on
// resource paths. Rejected requests must not trigger host credential callbacks.
func TestResourceDataRoutesRemoved(t *testing.T) {
	for _, requireKey := range []bool{false, true} {
		s, _ := newServer(t, func(c *config.Config) { c.RequireKey = requireKey })
		s.Auths = func(string) ([]store.AuthEntry, error) { t.Fatal("unexpected host call"); return nil, nil }
		for _, path := range []string{
			"/v0/resource/plugins/live-routes/api/snapshot",
			"/v0/resource/plugins/live-routes/api/events",
			"/v0/resource/plugins/live-routes/api/records",
			"/v0/resource/plugins/live-routes/snapshot",
			"/v0/resource/plugins/live-routes/live-routes/snapshot",
			"/other/v0/management/live-routes/snapshot",
			"/v0/management/live-routes/session",
		} {
			h := map[string][]string{"Host": {"localhost"}, "Origin": {"http://localhost"}}
			h["X-Live-Routes-Token"] = []string{"legacy"}
			r := s.Handle(Request{Method: "GET", Path: path, Headers: h})
			if r.Status != http.StatusNotFound || errCode(t, r) != "not_found" {
				t.Errorf("%s: %d", path, r.Status)
			}
		}
	}
}

func TestPageAlwaysServed(t *testing.T) {
	s, _ := newServer(t, func(c *config.Config) { c.RequireKey = true })
	r := s.Handle(Request{Method: "GET", Path: "/v0/resource/plugins/live-routes" + ResourcePagePath})
	if r.Status != 200 || string(r.Body) != "<html>page</html>" || r.Headers["Content-Type"][0] != "text/html; charset=utf-8" ||
		r.Headers["Content-Security-Policy"][0] != pageCSP || !strings.Contains(pageCSP, "connect-src 'self'") {
		t.Fatalf("%+v", r)
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
	for _, path := range []string{ManagementSnapshotPath, ManagementEventsPath, ManagementRecordsPath} {
		if r := s.Handle(Request{Method: "POST", Path: "/v0/management" + path}); r.Status != http.StatusMethodNotAllowed {
			t.Fatalf("POST %s: %d", path, r.Status)
		}
		if r := s.Handle(Request{Method: "GET", Path: "/v0/management" + path}); r.Status != http.StatusOK {
			t.Fatalf("GET %s: %d", path, r.Status)
		}
	}
	if r := s.Handle(get("/nope", "127.0.0.1", nil)); r.Status != 404 || errCode(t, r) != "not_found" {
		t.Fatalf("unknown: %d", r.Status)
	}
}

func TestRecordsQueryValidation(t *testing.T) {
	s, _ := newServer(t, nil)
	for _, q := range []map[string][]string{{"before": {"abc"}}, {"limit": {"0"}}, {"limit": {"x"}}} {
		if r := s.Handle(get(ManagementRecordsPath, "localhost", q)); r.Status != 400 || errCode(t, r) != "bad_request" {
			t.Errorf("%v: %d", q, r.Status)
		}
	}
	if r := s.Handle(get(ManagementRecordsPath, "localhost", map[string][]string{"limit": {"99999"}})); r.Status != 200 {
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
	req := get(ManagementSnapshotPath, "localhost", nil)
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
