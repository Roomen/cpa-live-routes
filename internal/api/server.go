package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

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
	Store *store.Store
	Auths AuthLister
	Page  []byte
}

// Handle serves only registered, exact paths. CPA authenticates management
// requests before forwarding them; resource requests can only retrieve the page.
func (s *Server) Handle(req Request) Response {
	p := strings.TrimRight(req.Path, "/")
	switch p {
	case "/v0/resource/plugins/" + PluginID + ResourcePagePath:
		if req.Method != http.MethodGet {
			return errorResponse(http.StatusMethodNotAllowed, "bad_request", "use GET")
		}
		return s.page()
	case "/v0/management" + ManagementSnapshotPath,
		"/v0/management" + ManagementEventsPath,
		"/v0/management" + ManagementRecordsPath:
		if req.Method != http.MethodGet {
			return errorResponse(http.StatusMethodNotAllowed, "bad_request", "use GET")
		}
		switch p {
		case "/v0/management" + ManagementSnapshotPath:
			return s.snapshot(req)
		case "/v0/management" + ManagementEventsPath:
			return s.events(req)
		}
		return s.records(req)
	}
	return errorResponse(http.StatusNotFound, "not_found", "unknown route")
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

func queryValue(q map[string][]string, name string) string {
	if v := q[name]; len(v) > 0 {
		return v[0]
	}
	return ""
}
