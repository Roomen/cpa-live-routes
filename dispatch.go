package main

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/Roomen/cpa-live-routes/internal/api"
	"github.com/Roomen/cpa-live-routes/internal/config"
	"github.com/Roomen/cpa-live-routes/internal/store"
	"github.com/Roomen/cpa-live-routes/internal/wire"
	"github.com/Roomen/cpa-live-routes/web"
)

const schemaVersion = 6

// Method names of the plugin ABI.
const (
	methodRegister     = "plugin.register"
	methodReconfigure  = "plugin.reconfigure"
	methodQuiesce      = "plugin.quiesce"
	methodShutdown     = "plugin.shutdown"
	methodUsage        = "usage.handle"
	methodBefore       = "request.intercept_before"
	methodAfter        = "request.intercept_after"
	methodComplete     = "request.complete"
	methodMgmtRegister = "management.register"
	methodMgmtHandle   = "management.handle"
	hostAuthList       = "host.auth.list"
)

var (
	emptyOK     = []byte(`{"ok":true,"result":{}}`)
	errInternal = []byte(`{"ok":false,"error":{"code":"plugin_error","message":"internal error"}}`)
)

// hostFunc calls back into the host.
type hostFunc func(method string, payload []byte) ([]byte, error)

type plugin struct {
	st   *store.Store
	srv  *api.Server
	host hostFunc

	mu       sync.RWMutex
	schema   uint32
	inflight bool
}

func newPlugin(host hostFunc) *plugin {
	st := store.New(config.Default(), nil)
	p := &plugin{st: st, host: host, inflight: true, schema: schemaVersion}
	p.srv = &api.Server{Store: st, Page: web.IndexHTML, Auths: p.listAuths}
	return p
}

func (p *plugin) tracking() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.inflight && p.schema >= 2
}

// handle runs one ABI method and returns the response envelope. It never panics:
// a panic inside the host process would take CPA down with it.
func (p *plugin) handle(method string, req []byte) (out []byte) {
	defer func() {
		if recover() != nil {
			out = errInternal
			if method == methodBefore || method == methodAfter {
				out = emptyOK
			}
		}
	}()
	switch method {
	case methodBefore, methodAfter:
		if p.tracking() {
			if in, err := wire.ParseIntercept(req); err == nil {
				if method == methodBefore {
					p.st.Before(&in)
				} else {
					p.st.After(&in)
				}
			}
		}
		return emptyOK
	case methodComplete:
		if !p.tracking() {
			return emptyOK
		}
		c, err := wire.ParseCompletion(req)
		if err != nil {
			return errorEnvelope("invalid_request", "invalid completion payload")
		}
		p.st.Complete(c)
		return emptyOK
	case methodUsage:
		u, err := wire.ParseUsage(req)
		if err != nil {
			return errorEnvelope("invalid_request", "invalid usage payload")
		}
		p.st.Usage(u)
		return emptyOK
	case methodRegister, methodReconfigure:
		return p.configure(req)
	case methodQuiesce, methodShutdown:
		return emptyOK
	case methodMgmtRegister:
		return okEnvelope(managementRegistration())
	case methodMgmtHandle:
		return p.manage(req)
	}
	return errorEnvelope("unknown_method", "unknown method: "+method)
}

func (p *plugin) configure(raw []byte) []byte {
	l, err := wire.ParseLifecycle(raw)
	if err != nil {
		return errorEnvelope("invalid_request", "invalid lifecycle payload")
	}
	cfg, err := config.Parse(l.ConfigYAML)
	if err != nil {
		return errorEnvelope("invalid_config", err.Error())
	}
	p.mu.Lock()
	p.schema = l.SchemaVersion
	p.inflight = cfg.InflightTracking
	tracking := p.inflight && p.schema >= 2
	p.mu.Unlock()
	cfg.InflightTracking = tracking
	p.st.SetConfig(cfg)
	return okEnvelope(registration(tracking))
}

type capabilities struct {
	UsagePlugin      bool `json:"usage_plugin"`
	ManagementAPI    bool `json:"management_api"`
	RequestIntercept bool `json:"request_interceptor"`
	RequestLifecycle bool `json:"request_lifecycle_plugin"`
}

type metadata struct {
	Name             string
	Version          string
	Author           string
	GitHubRepository string
	Logo             string
	ConfigFields     []config.Field
}

type registrationResult struct {
	SchemaVersion uint32       `json:"schema_version"`
	Metadata      metadata     `json:"metadata"`
	Capabilities  capabilities `json:"capabilities"`
}

func registration(tracking bool) registrationResult {
	return registrationResult{
		SchemaVersion: schemaVersion,
		Metadata: metadata{
			Name:             "Live Routes",
			Version:          "0.1.4",
			Author:           "Roomen",
			GitHubRepository: "https://github.com/Roomen/cpa-live-routes",
			ConfigFields:     config.Fields(),
		},
		Capabilities: capabilities{UsagePlugin: true, ManagementAPI: true, RequestIntercept: tracking, RequestLifecycle: tracking},
	}
}

type route struct {
	Method string
	Path   string
}

type resource struct {
	Path        string
	Menu        string `json:",omitempty"`
	Description string `json:",omitempty"`
}

func managementRegistration() any {
	return struct {
		Routes    []route    `json:"routes"`
		Resources []resource `json:"resources"`
	}{
		Routes: []route{
			{"GET", api.ManagementSnapshotPath},
			{"GET", api.ManagementEventsPath},
			{"GET", api.ManagementRecordsPath},
		},
		Resources: []resource{
			{Path: api.ResourcePagePath, Menu: "Live Routes", Description: "Live view of client, session, rule and exit credential routing."},
		},
	}
}

type managementResponse struct {
	StatusCode int
	Headers    map[string][]string
	Body       []byte
}

func (p *plugin) manage(raw []byte) []byte {
	m, err := wire.ParseManagement(raw)
	if err != nil {
		return errorEnvelope("invalid_request", "invalid management payload")
	}
	resp := p.srv.Handle(api.Request{
		Method: m.Method, Path: m.Path, Headers: m.Headers, Query: m.Query, HostCallbackID: m.HostCallbackID,
	})
	return okEnvelope(managementResponse{StatusCode: resp.Status, Headers: resp.Headers, Body: resp.Body})
}

type authListResult struct {
	Files []struct {
		AuthIndex string `json:"auth_index"`
		Provider  string `json:"provider"`
		Type      string `json:"type"`
		Label     string `json:"label"`
		Email     string `json:"email"`
	} `json:"files"`
}

// listAuths asks the host for its credential list. It only runs from a
// management request, never from the traffic paths.
func (p *plugin) listAuths(callbackID string) ([]store.AuthEntry, error) {
	if p.host == nil {
		return nil, errors.New("no host")
	}
	payload := []byte(`{}`)
	if callbackID != "" {
		payload, _ = json.Marshal(map[string]string{"host_callback_id": callbackID})
	}
	raw, err := p.host(hostAuthList, payload)
	if err != nil {
		return nil, err
	}
	var env struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	body := raw
	if json.Unmarshal(raw, &env) == nil && env.Result != nil {
		body = env.Result
	}
	var res authListResult
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	out := make([]store.AuthEntry, 0, len(res.Files))
	for _, f := range res.Files {
		prov := f.Provider
		if prov == "" {
			prov = f.Type
		}
		out = append(out, store.AuthEntry{AuthIndex: f.AuthIndex, Provider: prov, Label: f.Label, Email: f.Email})
	}
	return out, nil
}

func okEnvelope(v any) []byte {
	result, err := json.Marshal(v)
	if err != nil {
		return errInternal
	}
	out := make([]byte, 0, len(result)+32)
	out = append(out, `{"ok":true,"result":`...)
	out = append(out, result...)
	return append(out, '}')
}

func errorEnvelope(code, message string) []byte {
	raw, err := json.Marshal(struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{Error: struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{code, message}})
	if err != nil {
		return errInternal
	}
	return raw
}
