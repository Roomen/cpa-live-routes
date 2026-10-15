package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Roomen/cpa-live-routes/internal/api"
)

func call(t testing.TB, p *plugin, method string, req any) (json.RawMessage, bool, string) {
	t.Helper()
	var raw []byte
	switch v := req.(type) {
	case []byte:
		raw = v
	default:
		raw, _ = json.Marshal(v)
	}
	var env struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  struct{ Code string }
	}
	if err := json.Unmarshal(p.handle(method, raw), &env); err != nil {
		t.Fatal(err)
	}
	return env.Result, env.OK, env.Error.Code
}

func register(t testing.TB, p *plugin, yaml string, schema int) json.RawMessage {
	res, ok, code := call(t, p, "plugin.register", map[string]any{"config_yaml": base64.StdEncoding.EncodeToString([]byte(yaml)), "schema_version": schema})
	if !ok {
		t.Fatalf("register: %s", code)
	}
	return res
}

func TestRegistration(t *testing.T) {
	p := newPlugin(nil)
	var reg struct {
		SchemaVersion int `json:"schema_version"`
		Metadata      struct {
			Name         string
			ConfigFields []struct{ Name, Type string }
		}
		Capabilities map[string]bool
	}
	if err := json.Unmarshal(register(t, p, "", 6), &reg); err != nil {
		t.Fatal(err)
	}
	c := reg.Capabilities
	if reg.SchemaVersion != 6 || reg.Metadata.Name != "Live Routes" || len(reg.Metadata.ConfigFields) != 8 ||
		!c["usage_plugin"] || !c["management_api"] || !c["request_interceptor"] || !c["request_lifecycle_plugin"] {
		t.Fatalf("%+v", reg)
	}
	json.Unmarshal(register(t, p, "inflight_tracking: false", 6), &reg)
	if reg.Capabilities["request_interceptor"] || reg.Capabilities["request_lifecycle_plugin"] {
		t.Fatalf("tracking off: %+v", reg.Capabilities)
	}
	json.Unmarshal(register(t, p, "", 1), &reg)
	if reg.Capabilities["request_interceptor"] {
		t.Fatalf("schema 1 must force tracking off: %+v", reg.Capabilities)
	}
	if _, ok, code := call(t, p, "plugin.reconfigure", map[string]any{"config_yaml": base64.StdEncoding.EncodeToString([]byte("max_records: [")), "schema_version": 6}); ok || code != "invalid_config" {
		t.Fatalf("bad yaml: ok=%v code=%s", ok, code)
	}
}

func TestMethods(t *testing.T) {
	p := newPlugin(nil)
	for _, m := range []string{"plugin.quiesce", "plugin.shutdown"} {
		if res, ok, _ := call(t, p, m, nil); !ok || string(res) != "{}" {
			t.Errorf("%s: %s", m, res)
		}
	}
	if _, ok, code := call(t, p, "bogus.method", nil); ok || code != "unknown_method" {
		t.Errorf("unknown: %v %s", ok, code)
	}
	res, ok, _ := call(t, p, "management.register", nil)
	var mr struct {
		Routes    []struct{ Method, Path string }
		Resources []struct{ Path, Menu string }
	}
	if err := json.Unmarshal(res, &mr); err != nil || !ok || len(mr.Routes) != 3 || len(mr.Resources) != 1 || mr.Resources[0].Path != "/live" || mr.Resources[0].Menu != "Live Routes" {
		t.Fatalf("%s", res)
	}
	for i, path := range []string{"/live-routes/snapshot", "/live-routes/events", "/live-routes/records"} {
		if mr.Routes[i].Method != "GET" || mr.Routes[i].Path != path {
			t.Fatalf("unexpected management routes: %s", res)
		}
	}
}

func interceptPayload(id, trace string, bodySize int) []byte {
	body := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7, 8, 9}, bodySize/4))
	return []byte(fmt.Sprintf(`{"RequestID":%q,"TraceID":%q,"Model":"claude-opus-4-1","RequestedModel":"opus","Stream":true,"Headers":{"User-Agent":["claude-cli/2.0.14 (external, cli)"],"Authorization":["Bearer sk-ant-abcdefghijklmnop3f9a"]},"Body":"%s","Metadata":{"selected_auth_index":"3","canonical_session_id":"claude:7b1e2c4d-5a8f"},"host_callback_id":"cb"}`, id, trace, body))
}

func TestInterceptorAlwaysReturnsEmptyResult(t *testing.T) {
	p := newPlugin(nil)
	huge := interceptPayload("big", "tb", 8<<20)
	inputs := [][]byte{interceptPayload("r1", "t1", 64), []byte("not json"), nil, []byte(`{"RequestID":`), huge}
	for _, m := range []string{"request.intercept_before", "request.intercept_after"} {
		for _, in := range inputs {
			if got := p.handle(m, in); !bytes.Equal(got, []byte(`{"ok":true,"result":{}}`)) {
				t.Fatalf("%s returned %.80s", m, got)
			}
		}
	}
	// Tracking disabled: still a pass-through.
	register(t, p, "inflight_tracking: false", 6)
	if got := p.handle("request.intercept_before", inputs[0]); !bytes.Equal(got, emptyOK) {
		t.Fatalf("%s", got)
	}
}

func TestEndToEndThroughDispatch(t *testing.T) {
	p := newPlugin(nil)
	register(t, p, "", 6)
	p.handle("request.intercept_before", interceptPayload("r1", "t1", 64))
	p.handle("request.intercept_after", interceptPayload("r1", "t1", 64))
	usage := `{"RequestID":"u1","TraceID":"t1","Provider":"claude","Model":"claude-opus-4-1","Alias":"opus","APIKey":
"sk-ant-abcdefghijklmnop3f9a","IsNativeKey":true,"SessionID":"claude:7b1e2c4d-5a8f","AuthIndex":"3","Latency":2000000000,"TTFT":500000000,"Detail":{"InputTokens":10,"OutputTokens":5,"TotalTokens":15}}`
	if _, ok, _ := call(t, p, "usage.handle", []byte(usage)); !ok {
		t.Fatal("usage")
	}
	if _, ok, _ := call(t, p, "request.complete", []byte(`{"RequestID":"r1","TraceID":"t1","Outcome":"succeeded","StatusCode":200}`)); !ok {
		t.Fatal("complete")
	}
	if _, ok, code := call(t, p, "usage.handle", []byte("{")); ok || code != "invalid_request" {
		t.Fatal("bad usage should be an error envelope")
	}

	res, ok, _ := call(t, p, "management.handle", map[string]any{
		"Method": "GET", "Path": "/v0/management/live-routes/snapshot",
		"Headers": map[string][]string{"Host": {"127.0.0.1:8317"}}, "host_callback_id": "cb",
	})
	var mr struct {
		StatusCode int
		Headers    map[string][]string
		Body       []byte
	}
	if err := json.Unmarshal(res, &mr); err != nil || !ok || mr.StatusCode != 200 {
		t.Fatalf("%s", res)
	}
	var snap api.Snapshot
	if err := json.Unmarshal(mr.Body, &snap); err != nil || len(snap.Records) != 1 {
		t.Fatalf("%v %s", err, mr.Body)
	}
	r := snap.Records[0]
	if r.State != api.StateDone || r.Tok == nil || r.Tok.Total != 15 || r.Attempts != 1 || r.TTFT != 500 {
		t.Fatalf("%+v", r)
	}
	if strings.Contains(string(mr.Body), "abcdefghijklmnop") {
		t.Fatal("raw key leaked")
	}
}

func TestHostAuthListEnvelope(t *testing.T) {
	var gotMethod string
	var gotPayload []byte
	p := newPlugin(func(method string, payload []byte) ([]byte, error) {
		gotMethod, gotPayload = method, payload
		return []byte(`{"ok":true,"result":{"files":[{"auth_index":"3","provider":"claude","email":"lisa@gmail.com"}]}}`), nil
	})
	register(t, p, "", 6)
	p.handle("request.intercept_after", interceptPayload("r1", "t1", 64))
	mgmt := map[string]any{"Method": "GET", "Path": "/v0/management/live-routes/snapshot", "Headers": map[string][]string{"Host": {"localhost"}}, "host_callback_id": "cb-1"}
	res, _, _ := call(t, p, "management.handle", mgmt)
	var mr struct{ Body []byte }
	json.Unmarshal(res, &mr)
	if gotMethod != "host.auth.list" || !strings.Contains(string(gotPayload), `"cb-1"`) || !strings.Contains(string(mr.Body), "l***@gmail.com") {
		t.Fatalf("method=%s payload=%s body=%s", gotMethod, gotPayload, mr.Body)
	}
}
