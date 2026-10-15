package api

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Roomen/cpa-live-routes/internal/store"
)

const (
	pageCSP      = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; img-src data:; base-uri 'none'; form-action 'none'"
	defaultLimit = 500
	maxLimit     = 2000
)

// Request is a management or resource call as the host delivers it.
type Request struct {
	Method         string
	Path           string
	Headers        map[string][]string
	Query          map[string][]string
	HostCallbackID string
}

// Response is the reply to a Request.
type Response struct {
	Status  int
	Headers map[string][]string
	Body    []byte
}

// AuthLister fetches the host credential list; callbackID is the in-flight host call.
type AuthLister func(callbackID string) ([]store.AuthEntry, error)

// Server routes plugin HTTP calls.
type Server struct {
	Store  *store.Store
	Tokens *TokenStore
	Auths  AuthLister
	Now    func() time.Time
	Page   []byte
}

// Handle serves one request. The path is matched by suffix, so it works for both
// the management and the resource prefixes.
func (s *Server) Handle(req Request) Response {
	p := strings.TrimRight(req.Path, "/")
	switch {
	case strings.HasSuffix(p, ManagementSessionPath):
		if req.Method != http.MethodPost {
			return errorResponse(http.StatusMethodNotAllowed, "bad_request", "use POST")
		}
		return s.session()
	case strings.HasSuffix(p, ManagementSnapshotPath):
		if req.Method != http.MethodGet {
			return errorResponse(http.StatusMethodNotAllowed, "bad_request", "use GET")
		}
		return s.snapshot(req)
	case strings.HasSuffix(p, ResourcePagePath):
		return s.page()
	case strings.HasSuffix(p, ResourceSnapshotPath), strings.HasSuffix(p, ResourceEventsPath), strings.HasSuffix(p, ResourceRecordsPath):
		if req.Method != http.MethodGet {
			return errorResponse(http.StatusMethodNotAllowed, "bad_request", "use GET")
		}
		if denied := s.gate(req); denied != nil {
			return *denied
		}
		switch {
		case strings.HasSuffix(p, ResourceSnapshotPath):
			return s.snapshot(req)
		case strings.HasSuffix(p, ResourceEventsPath):
			return s.events(req)
		}
		return s.records(req)
	}
	return errorResponse(http.StatusNotFound, "not_found", "unknown route")
}

func (s *Server) session() Response {
	tok, exp := s.Tokens.Issue()
	return jsonResponse(http.StatusOK, mustJSON(SessionToken{
		tok, exp.UnixMilli(), int(tokenTTL / time.Second),
	}))
}

func (s *Server) page() Response {
	return Response{
		Status: http.StatusOK,
		Headers: map[string][]string{
			"Content-Type":            {"text/html; charset=utf-8"},
			"Content-Security-Policy": {pageCSP},
			"Cache-Control":           {"no-store"},
			"X-Content-Type-Options":  {"nosniff"},
		},
		Body: s.Page,
	}
}

func (s *Server) gate(req Request) *Response {
	if s.Store.Config().RequireKey {
		if s.Tokens.Valid(headerValue(req.Headers, TokenHeader)) {
			return nil
		}
		r := errorResponse(http.StatusUnauthorized, "unauthorized", "a view token from the management session is required")
		return &r
	}
	if !loopbackRequest(req.Headers) {
		r := errorResponse(http.StatusForbidden, "forbidden_host", "the live view is served to loopback hosts only; enable require_key to use it remotely")
		return &r
	}
	return nil
}

// loopbackRequest checks the Host header. The host process does not forward
// Host to plugins today, so Origin and then Referer stand in for it; with none
// of them the request is refused.
func loopbackRequest(h map[string][]string) bool {
	if host := headerValue(h, "Host"); host != "" {
		return isLoopbackHost(host)
	}
	for _, name := range []string{"Origin", "Referer"} {
		if v := headerValue(h, name); v != "" {
			u, err := url.Parse(v)
			return err == nil && isLoopbackHost(u.Host)
		}
	}
	return false
}

func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	switch strings.ToLower(host) {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

func (s *Server) refreshExits(callbackID string) {
	if s.Auths == nil || !s.Store.WantAuthList() {
		return
	}
	if files, err := s.Auths(callbackID); err == nil {
		s.Store.ApplyAuthList(files)
	}
}

func (s *Server) snapshot(req Request) Response {
	s.refreshExits(req.HostCallbackID)
	return jsonResponse(http.StatusOK, s.Store.AppendSnapshot(make([]byte, 0, 64<<10)))
}

func (s *Server) events(req Request) Response {
	s.refreshExits(req.HostCallbackID)
	return jsonResponse(http.StatusOK, s.Store.AppendEvents(make([]byte, 0, 8<<10), queryValue(req.Query, "since")))
}

func (s *Server) records(req Request) Response {
	limit := defaultLimit
	if v := queryValue(req.Query, "limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return errorResponse(http.StatusBadRequest, "bad_request", "limit must be a positive integer")
		}
		limit = min(n, maxLimit)
	}
	body, ok := s.Store.AppendRecords(make([]byte, 0, 16<<10), queryValue(req.Query, "before"), limit)
	if !ok {
		return errorResponse(http.StatusBadRequest, "bad_request", "before must be a cursor from the snapshot or a previous page")
	}
	return jsonResponse(http.StatusOK, body)
}

func jsonResponse(status int, body []byte) Response {
	return Response{
		Status: status,
		Headers: map[string][]string{
			"Content-Type":           {"application/json"},
			"Cache-Control":          {"no-store"},
			"X-Content-Type-Options": {"nosniff"},
		},
		Body: body,
	}
}

func errorResponse(status int, code, msg string) Response {
	return jsonResponse(status, mustJSON(Error{Code: code, Message: msg}))
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func headerValue(h map[string][]string, name string) string {
	for k, v := range h {
		if strings.EqualFold(k, name) && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

func queryValue(q map[string][]string, name string) string {
	if v := q[name]; len(v) > 0 {
		return v[0]
	}
	return ""
}
