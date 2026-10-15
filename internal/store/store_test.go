package store_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Roomen/cpa-live-routes/internal/api"
	"github.com/Roomen/cpa-live-routes/internal/config"
	"github.com/Roomen/cpa-live-routes/internal/store"
	"github.com/Roomen/cpa-live-routes/internal/wire"
)

type clock struct{ t time.Time }

func (c *clock) Now() time.Time          { return c.t }
func (c *clock) Advance(d time.Duration) { c.t = c.t.Add(d) }

func newClock() *clock { return &clock{t: time.Unix(1760500000, 0)} }

func newStore(t testing.TB, mut func(*config.Config)) (*store.Store, *clock) {
	t.Helper()
	cfg := config.Default()
	if mut != nil {
		mut(&cfg)
	}
	c := newClock()
	return store.New(cfg, c.Now), c
}

func decode[T any](t testing.TB, b []byte) T {
	t.Helper()
	var v T
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decode %T: %v\n%s", v, err, b)
	}
	return v
}

func mustRecords(t testing.TB, s *store.Store, before string, limit int) []byte {
	t.Helper()
	b, ok := s.AppendRecords(nil, before, limit)
	if !ok {
		t.Fatalf("cursor %q rejected", before)
	}
	return b
}

func recordsPage(t testing.TB, s *store.Store, before string, limit int) api.RecordsPage {
	t.Helper()
	return decode[api.RecordsPage](t, mustRecords(t, s, before, limit))
}

func snap(t testing.TB, s *store.Store) api.Snapshot {
	return decode[api.Snapshot](t, s.AppendSnapshot(nil))
}

func events(t testing.TB, s *store.Store, since string) api.Events {
	return decode[api.Events](t, s.AppendEvents(nil, since))
}

func recByID(t testing.TB, sn api.Snapshot, id string) api.Record {
	t.Helper()
	for _, r := range sn.Records {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("record %q not in snapshot", id)
	return api.Record{}
}

func intercept(id, trace string) *wire.Intercept {
	return &wire.Intercept{
		RequestID: id, TraceID: trace, Model: "claude-opus-4-1", RequestedModel: "opus", Stream: true,
		Headers: wire.Headers{UA: "claude-cli/2.0.14 (external, cli)", Authorization: "Bearer sk-ant-abcdefghijklmnop3f9a", XForwardedFor: "192.168.1.23, 10.0.0.1"},
	}
}

func afterOf(id, trace, authIndex, sid string) *wire.Intercept {
	in := intercept(id, trace)
	in.AuthIndex, in.SessionID = authIndex, sid
	return in
}

func usageOf(trace string) *wire.Usage {
	u := &wire.Usage{RequestID: "usage-" + trace, TraceID: trace, Provider: "claude", Model: "claude-opus-4-1", Alias: "opus",
		APIKey: SyntheticKey, IsNativeKey: true, AccessProvider: "config-inline",
		SessionID: "claude:7b1e2c4d-5a8f", AuthIndex: "1", Stream: true,
		Latency: 12 * time.Second, TTFT: 1450 * time.Millisecond}
	u.Detail.InputTokens, u.Detail.OutputTokens, u.Detail.ReasoningTokens, u.Detail.CachedTokens, u.Detail.TotalTokens = 48211, 1320, 640, 40960, 49531
	return u
}

func completeOf(id, trace, outcome string, status int) *wire.Completion {
	return &wire.Completion{RequestID: id, TraceID: trace, Outcome: outcome, StatusCode: status, Model: "claude-opus-4-1", RequestedModel: "opus", Stream: true}
}

func TestLifecycleBeforeAfterCompleteUsage(t *testing.T) {
	s, c := newStore(t, nil)
	s.Before(intercept("req-1", "tr1"))
	r := recByID(t, snap(t, s), "req-1")
	if r.State != api.StateQueued || r.Client == 0 || r.Rule == 0 || r.Exit != 0 || !r.Stream {
		t.Fatalf("queued: %+v", r)
	}
	c.Advance(time.Second)
	s.After(afterOf("req-1", "tr1", "1", "claude:7b1e2c4d-5a8f"))
	sn := snap(t, s)
	r = recByID(t, sn, "req-1")
	if r.State != api.StateRouted || r.Exit == 0 || r.Session == 0 || r.Attempts != 1 {
		t.Fatalf("routed: %+v", r)
	}
	if sn.Stats.Inflight != 1 {
		t.Fatalf("inflight = %d", sn.Stats.Inflight)
	}
	s.Usage(usageOf("tr1"))
	c.Advance(time.Second)
	s.Complete(completeOf("req-1", "tr1", "succeeded", 200))
	sn = snap(t, s)
	r = recByID(t, sn, "req-1")
	if r.State != api.StateDone || r.Status != 200 || r.T1 == 0 || r.Tok == nil || r.Tok.Total != 49531 || r.Tok.Cache != 40960 || r.TTFT != 1450 || r.Lat != 12000 {
		t.Fatalf("done: %+v tok=%+v", r, r.Tok)
	}
	if len(sn.Records) != 1 || sn.Stats.Inflight != 0 {
		t.Fatalf("records=%d inflight=%d", len(sn.Records), sn.Stats.Inflight)
	}
	d := sn.Dict
	if len(d.Clients) != 1 || d.Clients[0].Label != "Claude Code 2.0.14" || d.Clients[0].IP != "192.168.1.23" || d.Clients[0].Kind != api.ClientClaudeCode {
		t.Fatalf("clients: %+v", d.Clients)
	}
	if len(d.Keys) != 1 || d.Keys[0].Mask != "sk-…3f9a" || len(d.Keys[0].FP) != 8 {
		t.Fatalf("keys: %+v", d.Keys)
	}
	if len(d.Sessions) != 1 || d.Sessions[0].Short != "7b1e2c4d" {
		t.Fatalf("sessions: %+v", d.Sessions)
	}
	if len(d.Rules) != 1 || d.Rules[0].Alias != "opus" || len(d.Exits) != 1 || d.Exits[0].Provider != "claude" {
		t.Fatalf("rules/exits: %+v %+v", d.Rules, d.Exits)
	}
}

func TestRetriesCountAttempts(t *testing.T) {
	s, _ := newStore(t, nil)
	s.Before(intercept("r", "t"))
	s.After(afterOf("r", "t", "1", ""))
	s.After(afterOf("r", "t", "2", ""))
	r := recByID(t, snap(t, s), "r")
	if r.Attempts != 2 {
		t.Fatalf("attempts = %d", r.Attempts)
	}
	fail := usageOf("t")
	fail.Failed = true
	fail.Failure.StatusCode, fail.Failure.Body = 429, "rate   limit\nexceeded for Bearer sk-ant-abcdefghijklmnop3f9a"
	fail.ResponseHeaders.RetryAfter = []string{"30"}
	s.Usage(fail)
	s.Complete(completeOf("r", "t", "failed", 429))
	r = recByID(t, snap(t, s), "r")
	if r.State != api.StateFailed || r.Status != 429 || r.RetryAfterS != 30 || r.Attempts != 2 {
		t.Fatalf("failed: %+v", r)
	}
	if strings.Contains(r.Err, "sk-ant-abcdefghijklmnop3f9a") || !strings.HasPrefix(r.Err, "rate limit exceeded") {
		t.Fatalf("err = %q", r.Err)
	}
}

func TestUsageOnly(t *testing.T) {
	s, _ := newStore(t, func(c *config.Config) { c.InflightTracking = false })
	u := usageOf("tr9")
	u.RequestedAt = time.Unix(1760499990, 0)
	s.Usage(u)
	sn := snap(t, s)
	r := recByID(t, sn, "usage-tr9")
	if r.State != api.StateDone || r.T0 != 1760499990000 || r.T1 != 1760500002000 || r.Status != 200 || r.Client == 0 || r.Exit == 0 || r.Attempts != 1 {
		t.Fatalf("usage-only: %+v", r)
	}
	// A retry of the same trace merges and a later failure flips the state.
	u2 := usageOf("tr9")
	u2.RequestID = "usage-tr9-b"
	u2.Failed, u2.Failure.StatusCode, u2.Failure.Body = true, 500, "boom"
	s.Usage(u2)
	sn = snap(t, s)
	if len(sn.Records) != 1 {
		t.Fatalf("records = %d", len(sn.Records))
	}
	r = sn.Records[0]
	if r.State != api.StateFailed || r.Attempts != 2 || r.Status != 500 || r.Err != "boom" {
		t.Fatalf("merged: %+v", r)
	}
}

func TestCompleteOnly(t *testing.T) {
	s, _ := newStore(t, nil)
	cmp := completeOf("lonely", "trx", "canceled", 0)
	cmp.StartedAt, cmp.CompletedAt = time.Unix(1760499990, 0), time.Unix(1760499995, 0)
	s.Complete(cmp)
	r := recByID(t, snap(t, s), "lonely")
	if r.State != api.StateCanceled || r.T0 != 1760499990000 || r.T1 != 1760499995000 || r.Lat != 5000 {
		t.Fatalf("complete-only: %+v", r)
	}
}

func TestCompleteWithoutModelIgnored(t *testing.T) {
	s, _ := newStore(t, nil)
	s.Complete(&wire.Completion{RequestID: "models-list", TraceID: "tm", Outcome: "succeeded", StatusCode: 200})
	if sn := snap(t, s); len(sn.Records) != 0 {
		t.Fatalf("records: %+v", sn.Records)
	}
	// a known request still completes even when the completion carries no model
	s.Before(intercept("req-m", "tm2"))
	s.Complete(&wire.Completion{RequestID: "req-m", Outcome: "succeeded", StatusCode: 200})
	if r := recByID(t, snap(t, s), "req-m"); r.State != api.StateDone {
		t.Fatalf("known request: %+v", r)
	}
}

func TestCompleteMergesUsageOnlyByTrace(t *testing.T) {
	s, _ := newStore(t, nil)
	s.Usage(usageOf("tz"))
	s.Complete(completeOf("other-id", "tz", "succeeded", 200))
	sn := snap(t, s)
	if len(sn.Records) != 1 || sn.Records[0].State != api.StateDone {
		t.Fatalf("records: %+v", sn.Records)
	}
}

func TestLostTimeout(t *testing.T) {
	s, c := newStore(t, func(cfg *config.Config) { cfg.RetentionMinutes = 60 })
	s.Before(intercept("stuck", "ts"))
	c.Advance(14 * time.Minute)
	if r := recByID(t, snap(t, s), "stuck"); r.State != api.StateQueued {
		t.Fatalf("state = %s", r.State)
	}
	c.Advance(2 * time.Minute)
	sn := snap(t, s)
	r := recByID(t, sn, "stuck")
	if r.State != api.StateLost || r.T1 == 0 || sn.Stats.Inflight != 0 {
		t.Fatalf("lost: %+v inflight=%d", r, sn.Stats.Inflight)
	}
}

func TestEvictionByAge(t *testing.T) {
	s, c := newStore(t, func(cfg *config.Config) { cfg.RetentionMinutes = 10 })
	for i := 0; i < 3; i++ {
		s.Complete(completeOf(fmt.Sprintf("old-%d", i), "", "succeeded", 200))
	}
	cur := snap(t, s).Cursor
	c.Advance(11 * time.Minute)
	s.Complete(completeOf("fresh", "", "succeeded", 200))
	sn := snap(t, s)
	if len(sn.Records) != 1 || sn.Records[0].ID != "fresh" || sn.Stats.Evicted != 3 {
		t.Fatalf("records=%d evicted=%d", len(sn.Records), sn.Stats.Evicted)
	}
	if len(sn.Dict.Clients) != 0 && len(sn.Dict.Rules) > 1 {
		t.Fatalf("dictionary not pruned: %+v", sn.Dict)
	}
	ev := events(t, s, cur)
	var sawEvict, sawFresh bool
	for _, e := range ev.Events {
		switch e.Type {
		case api.EventEvict:
			sawEvict = reflect.DeepEqual(e.IDs, []string{"old-0", "old-1", "old-2"})
		case api.EventUpsert:
			sawFresh = e.Rec.ID == "fresh"
		}
	}
	if !sawEvict || !sawFresh {
		t.Fatalf("events: %+v", ev.Events)
	}
}

func TestEvictionByCount(t *testing.T) {
	s, c := newStore(t, func(cfg *config.Config) { cfg.MaxRecords = 1000 })
	for i := 0; i < 1100; i++ {
		c.Advance(time.Millisecond)
		u := usageOf(fmt.Sprintf("t%d", i))
		u.SessionID = fmt.Sprintf("claude:session-%05d", i)
		s.Usage(u)
	}
	sn := snap(t, s)
	if len(sn.Records) != 1000 || sn.Stats.Evicted != 100 || sn.Stats.Records != 1000 {
		t.Fatalf("records=%d evicted=%d", len(sn.Records), sn.Stats.Evicted)
	}
	if len(sn.Dict.Sessions) != 1000 {
		t.Fatalf("sessions = %d, want 1000 (unreferenced entries dropped)", len(sn.Dict.Sessions))
	}
}

func TestInflightCap(t *testing.T) {
	s, _ := newStore(t, nil)
	for i := 0; i < 2100; i++ {
		s.Before(intercept(fmt.Sprintf("r%d", i), fmt.Sprintf("t%d", i)))
	}
	sn := snap(t, s)
	if sn.Stats.Inflight != 2048 || sn.Stats.InflightDropped != 52 || len(sn.Records) != 2048 {
		t.Fatalf("%+v records=%d", sn.Stats, len(sn.Records))
	}
}

func TestSetConfigShrinksImmediately(t *testing.T) {
	s, _ := newStore(t, func(cfg *config.Config) { cfg.MaxRecords = 2000 })
	for i := 0; i < 1500; i++ {
		s.Complete(completeOf(fmt.Sprintf("r%d", i), "", "succeeded", 200))
	}
	cur := snap(t, s).Cursor
	cfg := s.Config()
	cfg.MaxRecords = 1000
	cfg.Layout = "split"
	s.SetConfig(cfg)
	sn := snap(t, s)
	if len(sn.Records) != 1000 || sn.Config.Layout != "split" {
		t.Fatalf("records=%d cfg=%+v", len(sn.Records), sn.Config)
	}
	ev := events(t, s, cur)
	var types []string
	for _, e := range ev.Events {
		types = append(types, e.Type)
	}
	if !reflect.DeepEqual(types, []string{"evict", "config"}) || ev.Events[1].Config.Layout != "split" {
		t.Fatalf("events: %v", types)
	}
	if ids := ev.Events[0].IDs; len(ids) != 500 || ids[0] != "r0" || ids[499] != "r499" {
		t.Fatalf("evicted ids: %d", len(ids))
	}
}

func TestCursorSemantics(t *testing.T) {
	s, _ := newStore(t, nil)
	s.Before(intercept("a", "ta"))
	sn := snap(t, s)

	if ev := events(t, s, sn.Cursor); ev.Reset || len(ev.Events) != 0 || ev.Cursor != sn.Cursor {
		t.Fatalf("idle poll: %+v", ev)
	}
	s.After(afterOf("a", "ta", "1", ""))
	ev := events(t, s, sn.Cursor)
	if ev.Reset || len(ev.Events) != 1 || ev.Events[0].Rec.State != api.StateRouted || ev.Cursor == sn.Cursor {
		t.Fatalf("delta: %+v", ev)
	}
	if len(ev.Dict.Exits) != 1 {
		t.Fatalf("new exit not sent: %+v", ev.Dict)
	}
	if ev2 := events(t, s, ev.Cursor); len(ev2.Dict.Exits) != 0 || len(ev2.Events) != 0 {
		t.Fatalf("dict resent: %+v", ev2)
	}

	epoch := strings.SplitN(sn.Cursor, ":", 2)[0]
	for name, cur := range map[string]string{
		"other epoch": "deadbeef:1",
		"ahead":       epoch + ":999999",
		"garbage":     "nonsense",
		"empty":       "",
		"negative":    epoch + ":-1",
	} {
		if r := events(t, s, cur); !r.Reset || len(r.Events) != 0 || r.Cursor == "" {
			t.Fatalf("%s: %+v", name, r)
		}
	}
}

func TestCursorTooOldAndPaging(t *testing.T) {
	s, _ := newStore(t, func(cfg *config.Config) { cfg.MaxRecords = 5000 })
	start := snap(t, s).Cursor
	for i := 0; i < 2100; i++ {
		s.Complete(completeOf(fmt.Sprintf("r%d", i), "", "succeeded", 200))
	}
	first := events(t, s, start)
	if first.Reset || !first.More || len(first.Events) != 2000 {
		t.Fatalf("page 1: reset=%v more=%v n=%d", first.Reset, first.More, len(first.Events))
	}
	second := events(t, s, first.Cursor)
	if second.More || len(second.Events) != 100 || second.Events[0].Seq != first.Events[1999].Seq+1 {
		t.Fatalf("page 2: more=%v n=%d", second.More, len(second.Events))
	}

	for i := 0; i < 8200; i++ {
		s.Complete(completeOf(fmt.Sprintf("x%d", i), "", "succeeded", 200))
	}
	if r := events(t, s, start); !r.Reset {
		t.Fatalf("expected reset for a cursor older than the event log, got %d events", len(r.Events))
	}
}

func TestEventsRenderCurrentStateAndSkipEvicted(t *testing.T) {
	s, _ := newStore(t, func(cfg *config.Config) { cfg.MaxRecords = 1000 })
	start := snap(t, s).Cursor
	s.Before(intercept("keep", "tk"))
	s.After(afterOf("keep", "tk", "1", ""))
	s.Complete(completeOf("keep", "tk", "succeeded", 200))
	ev := events(t, s, start)
	if len(ev.Events) != 1 || ev.Events[0].Rec.State != api.StateDone {
		t.Fatalf("collapsed upserts: %+v", ev.Events)
	}

	cur := snap(t, s).Cursor
	s.Complete(completeOf("gone", "", "succeeded", 200))
	for i := 0; i < 1000; i++ {
		s.Complete(completeOf(fmt.Sprintf("n%d", i), "", "succeeded", 200))
	}
	for _, e := range events(t, s, cur).Events {
		if e.Rec != nil && (e.Rec.ID == "gone" || e.Rec.ID == "keep") {
			t.Fatalf("evicted record rendered: %+v", e.Rec)
		}
	}
}

func TestExitLabelResolution(t *testing.T) {
	s, c := newStore(t, nil)
	s.Before(intercept("a", "ta"))
	s.After(afterOf("a", "ta", "7", ""))
	if !s.WantAuthList() {
		t.Fatal("expected a label fetch for an unlabeled exit")
	}
	if s.WantAuthList() {
		t.Fatal("fetch must be rate limited")
	}
	cur := snap(t, s).Cursor
	s.ApplyAuthList([]store.AuthEntry{{AuthIndex: "7", Provider: "claude", Email: "lisa@gmail.com"}})
	ev := events(t, s, cur)
	if len(ev.Events) != 0 || len(ev.Dict.Exits) != 1 || ev.Dict.Exits[0].Label != "l***@gmail.com" || ev.Dict.Exits[0].Provider != "claude" {
		t.Fatalf("label update: %+v", ev)
	}
	c.Advance(2 * time.Minute)
	if s.WantAuthList() {
		t.Fatal("no refetch once every exit was tried")
	}
	s.After(afterOf("a", "ta", "8", ""))
	if !s.WantAuthList() {
		t.Fatal("new exit should trigger a fetch")
	}
	s.ApplyAuthList([]store.AuthEntry{{AuthIndex: "8", Label: "team-b@corp.example"}})
	for _, e := range snap(t, s).Dict.Exits {
		if e.AuthIndex == "8" && e.Label != "t***@corp.example" {
			t.Fatalf("label containing an address must be masked: %+v", e)
		}
	}
}

func TestAPIKeyLabels(t *testing.T) {
	s, _ := newStore(t, nil)
	s.Before(intercept("a", "ta"))
	if k := snap(t, s).Dict.Keys[0]; k.Label != "" {
		t.Fatalf("unexpected label %q", k.Label)
	}
	cur := snap(t, s).Cursor
	cfg := s.Config()
	// sha256 hex prefix of the key used by intercept()
	fp := snap(t, s).Dict.Keys[0].FP
	cfg.APIKeyLabels = map[string]string{strings.ToUpper(fp): "MacBook"}
	s.SetConfig(cfg)
	if k := snap(t, s).Dict.Keys[0]; k.Label != "MacBook" {
		t.Fatalf("relabel failed: %+v", k)
	}
	if ev := events(t, s, cur); len(ev.Dict.Keys) != 1 || ev.Dict.Keys[0].Label != "MacBook" {
		t.Fatalf("relabel not delivered: %+v", ev.Dict)
	}
}

func TestRecordsPage(t *testing.T) {
	s, c := newStore(t, nil)
	for i := 0; i < 30; i++ {
		c.Advance(time.Second)
		s.Complete(completeOf(fmt.Sprintf("r%02d", i), "", "succeeded", 200))
	}
	page := recordsPage(t, s, "", 5)
	if len(page.Records) != 5 || !page.More || page.Next == "" || page.Records[0].ID != "r29" || page.Records[4].ID != "r25" {
		t.Fatalf("page: more=%v %+v", page.More, page.Records)
	}
	if page.Oldest == 0 {
		t.Fatal("oldest missing")
	}
	page = recordsPage(t, s, page.Next, 5)
	if len(page.Records) != 5 || page.Records[0].ID != "r24" || page.Records[4].ID != "r20" {
		t.Fatalf("second page: %+v", page.Records)
	}
}

func TestEncoderMatchesContract(t *testing.T) {
	s, c := newStore(t, nil)
	s.Before(intercept("q", "tq"))
	s.Before(intercept("r", "tr"))
	s.After(afterOf("r", "tr", "2", "claude:7b1e:agent:explore-1"))
	for i, outcome := range []string{"succeeded", "failed", "canceled", "rejected"} {
		id, tr := fmt.Sprintf("c%d", i), fmt.Sprintf("tc%d", i)
		s.Before(intercept(id, tr))
		s.After(afterOf(id, tr, "1", "claude:7b1e"))
		u := usageOf(tr)
		if outcome == "failed" {
			u.Failed, u.Failure.StatusCode, u.Failure.Body = true, 429, "quo\"ta\t<b>é</b> \x01 \xff"
			u.ResponseHeaders.RetryAfter = []string{"12"}
		}
		s.Usage(u)
		c.Advance(time.Second)
		s.Complete(completeOf(id, tr, outcome, 200))
	}
	s.Usage(usageOf("only"))
	w := &wire.Usage{RequestID: "child", TraceID: "tchild", SessionID: "claude:7b1e:agent:explore-1", ParentSessionID: "claude:7b1e", APIKey: PluginPrincipal, AccessProvider: "plug"}
	s.Usage(w)

	cases := map[string][]byte{
		"snapshot": s.AppendSnapshot(nil),
		"events":   s.AppendEvents(nil, "bogus"),
		"records":  mustRecords(t, s, "", 100),
	}
	cur := snap(t, s).Cursor
	s.Complete(completeOf("q", "tq", "succeeded", 200))
	cases["events-delta"] = s.AppendEvents(nil, cur)
	cfg := s.Config()
	cfg.ReducedAnimation = true
	s.SetConfig(cfg)
	cases["events-config"] = s.AppendEvents(nil, strings.Split(cur, ":")[0]+":"+fmt.Sprint(mustSeq(t, cur)+1))

	for name, body := range cases {
		var generic any
		if err := json.Unmarshal(body, &generic); err != nil {
			t.Fatalf("%s: invalid JSON: %v\n%s", name, err, body)
		}
		var typed any
		switch name {
		case "snapshot":
			typed = ptr(decode[api.Snapshot](t, body))
		case "records":
			typed = ptr(decode[api.RecordsPage](t, body))
		default:
			typed = ptr(decode[api.Events](t, body))
		}
		again, err := json.Marshal(typed)
		if err != nil {
			t.Fatal(err)
		}
		var want any
		if err := json.Unmarshal(again, &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(generic, want) {
			t.Fatalf("%s: encoder output differs from contract round trip\n got: %s\nwant: %s", name, body, again)
		}
	}
}

func mustSeq(t testing.TB, cursor string) int {
	t.Helper()
	var n int
	if _, err := fmt.Sscanf(cursor[strings.IndexByte(cursor, ':')+1:], "%d", &n); err != nil {
		t.Fatal(err)
	}
	return n
}

func ptr[T any](v T) *T { return &v }

func TestRedactionNeverLeaksRawKeys(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-"
	s, c := newStore(t, func(cfg *config.Config) { cfg.MaxRecords = 5000 })
	start := snap(t, s).Cursor
	var keys []string
	for i := 0; i < 300; i++ {
		n := 8 + rng.Intn(60)
		b := make([]byte, n)
		for j := range b {
			b[j] = charset[rng.Intn(len(charset))]
		}
		key := string(b)
		switch i % 3 {
		case 0:
			key = "sk-" + key
		case 1:
			key = "AIza" + key
		}
		keys = append(keys, key)
		id, tr := fmt.Sprintf("id-%d", i), fmt.Sprintf("tr-%d", i)
		in := intercept(id, tr)
		switch i % 3 {
		case 0:
			in.Headers.Authorization = "Bearer " + key
		case 1:
			in.Headers.Authorization, in.Headers.XAPIKey = "", key
		default:
			in.Headers.Authorization, in.Headers.GoogAPIKey = "", key
		}
		s.Before(in)
		u := usageOf(tr)
		u.APIKey = key
		u.Failed, u.Failure.StatusCode = true, 401
		u.Failure.Body = fmt.Sprintf("invalid key %s; header was Bearer %s; sk-%s", key, key, key)
		s.Usage(u)
		cmp := completeOf(id, tr, "failed", 401)
		cmp.Error = "unauthorized: Bearer " + key
		s.Complete(cmp)
		c.Advance(time.Millisecond)
	}
	outputs := [][]byte{
		s.AppendSnapshot(nil),
		s.AppendEvents(nil, start),
		mustRecords(t, s, "", 2000),
	}
	for _, key := range keys {
		for i, out := range outputs {
			if bytes.Contains(out, []byte(key)) {
				t.Fatalf("raw key %q leaked into output %d", key, i)
			}
		}
	}
	for _, r := range snap(t, s).Records {
		if len(r.Err) > 200 {
			t.Fatalf("error text too long: %d", len(r.Err))
		}
	}
}

func TestMemoryBudget(t *testing.T) {
	var before, after runtimeMem
	before.read()
	s, c := newStore(t, nil)
	for i := 0; i < 5000; i++ {
		c.Advance(time.Millisecond)
		id, tr := fmt.Sprintf("%08x-0000-4000-8000-%012x", i, i*7919), fmt.Sprintf("%08x", i)
		in := intercept(id, tr)
		in.Headers.UA = fmt.Sprintf("claude-cli/2.%d.%d (external, cli)", i%50, i)
		in.Headers.Authorization = fmt.Sprintf("Bearer sk-ant-%032d", i)
		in.Headers.XForwardedFor = fmt.Sprintf("10.%d.%d.%d", i>>16&255, i>>8&255, i&255)
		s.Before(in)
		a := afterOf(id, tr, fmt.Sprint(i%20), fmt.Sprintf("claude:%08x-5a8f-4e0b-9c1d-2f6a7e8b9c0d", i))
		s.After(a)
		s.Complete(completeOf(id, tr, "succeeded", 200))
		s.Usage(usageOf(tr))
	}
	after.read()
	if got := snap(t, s).Stats.Records; got != 5000 {
		t.Fatalf("records = %d", got)
	}
	delta := int64(after.heapLive) - int64(before.heapLive)
	t.Logf("live heap delta: %.2f MB", float64(delta)/(1<<20))
	if delta > 4<<20 {
		t.Fatalf("heap grew by %d bytes, budget 4 MB", delta)
	}
	runtimeKeepAlive(s)
}

// pageAll follows Next until the end and returns the IDs in the order served.
func pageAll(t *testing.T, s *store.Store, limit int) []string {
	t.Helper()
	var ids []string
	cur := ""
	for i := 0; i < 1000; i++ {
		p := recordsPage(t, s, cur, limit)
		for _, r := range p.Records {
			ids = append(ids, r.ID)
		}
		if !p.More {
			if p.Next != "" {
				t.Fatalf("next without more: %q", p.Next)
			}
			return ids
		}
		if p.Next == "" {
			t.Fatal("more without next")
		}
		cur = p.Next
	}
	t.Fatal("paging does not terminate")
	return nil
}

func TestRecordsPagingEqualMilliseconds(t *testing.T) {
	s, _ := newStore(t, nil)
	for i := 0; i < 3; i++ { // the clock never moves: every T0 is equal
		s.Complete(completeOf(fmt.Sprintf("eq%d", i), "", "succeeded", 200))
	}
	got := pageAll(t, s, 2)
	if !reflect.DeepEqual(got, []string{"eq2", "eq1", "eq0"}) {
		t.Fatalf("paged %v", got)
	}
}

func TestRecordsPagingOutOfOrder(t *testing.T) {
	s, c := newStore(t, nil)
	now := c.Now()
	var want []string
	for i, age := range []int{50, 5, 40, 40, 1, 30, 20, 20, 59} {
		u := usageOf(fmt.Sprintf("o%d", i))
		u.RequestID = fmt.Sprintf("o%d", i)
		u.RequestedAt = now.Add(-time.Duration(age) * time.Second)
		s.Usage(u)
		want = append([]string{u.RequestID}, want...)
	}
	for _, limit := range []int{1, 2, 4, 100} {
		if got := pageAll(t, s, limit); !reflect.DeepEqual(got, want) {
			t.Fatalf("limit %d: paged %v, want %v", limit, got, want)
		}
	}
	if o := snap(t, s).Oldest; o != now.Add(-59*time.Second).UnixMilli() {
		t.Fatalf("oldest = %d, want the smallest T0 of any kept record", o)
	}
}

func TestRecordsPagingSurvivesEviction(t *testing.T) {
	s, c := newStore(t, func(cfg *config.Config) { cfg.RetentionMinutes = 1 })
	for i := 0; i < 10; i++ {
		s.Complete(completeOf(fmt.Sprintf("a%d", i), "", "succeeded", 200))
		c.Advance(5 * time.Second)
	}
	// a0..a9 arrived at 0,5,..45 s; now is 50 s
	p1 := recordsPage(t, s, "", 3)
	if len(p1.Records) != 3 || !p1.More {
		t.Fatalf("p1: %+v", p1)
	}
	c.Advance(26 * time.Second) // now 76 s: a0..a3 (arrived at <= 15 s) are gone
	var rest []string
	for cur := p1.Next; cur != ""; {
		p := recordsPage(t, s, cur, 3)
		for _, r := range p.Records {
			rest = append(rest, r.ID)
		}
		cur = p.Next
	}
	if !reflect.DeepEqual(rest, []string{"a6", "a5", "a4"}) {
		t.Fatalf("after eviction: %v", rest)
	}
	// everything evicted: the old cursor yields an empty page
	c.Advance(10 * time.Minute)
	if p := recordsPage(t, s, p1.Next, 3); len(p.Records) != 0 || p.More || p.Oldest != 0 {
		t.Fatalf("empty store: %+v", p)
	}
}

func TestRecordsCursorValidation(t *testing.T) {
	s, _ := newStore(t, nil)
	s.Complete(completeOf("x", "", "succeeded", 200))
	for _, bad := range []string{"garbage", "abc:xyz", "123456"} {
		if _, ok := s.AppendRecords(nil, bad, 10); ok {
			t.Fatalf("cursor %q accepted", bad)
		}
	}
	if p := recordsPage(t, s, "deadbeef:5", 10); len(p.Records) != 0 {
		t.Fatalf("foreign epoch must give an empty page: %+v", p)
	}
}

func TestSnapshotNextContinuesPastTruncation(t *testing.T) {
	s, c := newStore(t, func(cfg *config.Config) { cfg.MaxRecords = 8000 })
	const total = 5300
	for i := 0; i < total; i++ {
		c.Advance(time.Millisecond)
		s.Complete(completeOf(fmt.Sprintf("s%d", i), "", "succeeded", 200))
	}
	s.Before(intercept("live-old", "tl")) // in flight, newest position
	sn := snap(t, s)
	if !sn.Truncated || sn.Next == "" {
		t.Fatalf("truncated=%v next=%q", sn.Truncated, sn.Next)
	}
	seen := map[string]bool{}
	for _, r := range sn.Records {
		seen[r.ID] = true
	}
	for cur := sn.Next; cur != ""; {
		p := recordsPage(t, s, cur, 2000)
		for _, r := range p.Records {
			seen[r.ID] = true
		}
		cur = p.Next
	}
	if len(seen) != total+1 {
		t.Fatalf("snapshot + history cover %d of %d records", len(seen), total+1)
	}
}

func TestRetentionFollowsArrivalNotT0(t *testing.T) {
	s, c := newStore(t, func(cfg *config.Config) { cfg.RetentionMinutes = 1 })
	now := c.Now()
	cur := snap(t, s).Cursor
	// a late usage record: T0 is 55 s old when it arrives
	late := usageOf("late")
	late.RequestID = "late"
	late.RequestedAt = now.Add(-55 * time.Second)
	s.Usage(late)
	c.Advance(30 * time.Second)
	fresh := usageOf("fresh")
	fresh.RequestID = "fresh"
	fresh.RequestedAt = c.Now()
	s.Usage(fresh)
	// the first record's T0 is 105 s old, but it arrived 50 s ago: both stay
	c.Advance(20 * time.Second)
	sn := snap(t, s)
	if len(sn.Records) != 2 {
		t.Fatalf("records = %d, want 2 (retention counts from arrival)", len(sn.Records))
	}
	if sn.Oldest != now.Add(-55*time.Second).UnixMilli() {
		t.Fatalf("oldest = %d", sn.Oldest)
	}
	// 65 s after arrival the first one goes; the later one stays although its T0 is older than its neighbour's
	c.Advance(15 * time.Second)
	sn = snap(t, s)
	if len(sn.Records) != 1 || sn.Records[0].ID != "fresh" || sn.Stats.Evicted != 1 {
		t.Fatalf("after expiry: %+v", sn.Records)
	}
	var ids []string
	for _, e := range events(t, s, cur).Events {
		if e.Type == api.EventEvict {
			ids = append(ids, e.IDs...)
		}
	}
	if !reflect.DeepEqual(ids, []string{"late"}) {
		t.Fatalf("evicted ids = %v", ids)
	}
}

func TestEvictEventsStayExactAcrossBatches(t *testing.T) {
	s, c := newStore(t, func(cfg *config.Config) { cfg.MaxRecords = 1000 })
	cur := snap(t, s).Cursor
	for i := 0; i < 1300; i++ {
		c.Advance(time.Millisecond)
		s.Complete(completeOf(fmt.Sprintf("e%d", i), "", "succeeded", 200))
	}
	kept := map[string]bool{}
	for _, r := range snap(t, s).Records {
		kept[r.ID] = true
	}
	// replaying the log must leave exactly the kept records
	view := map[string]bool{}
	for more := true; more; {
		ev := events(t, s, cur)
		if ev.Reset {
			t.Fatal("unexpected reset")
		}
		for _, e := range ev.Events {
			switch e.Type {
			case api.EventUpsert:
				view[e.Rec.ID] = true
			case api.EventEvict:
				for _, id := range e.IDs {
					delete(view, id)
				}
			}
		}
		cur, more = ev.Cursor, ev.More
	}
	if !reflect.DeepEqual(view, kept) {
		t.Fatalf("replay has %d records, store keeps %d", len(view), len(kept))
	}
}

func TestEvictEventResetWhenIDsLost(t *testing.T) {
	s, c := newStore(t, func(cfg *config.Config) { cfg.MaxRecords = 1000 })
	for i := 0; i < 1000; i++ {
		s.Complete(completeOf(fmt.Sprintf("f%d", i), "", "succeeded", 200))
	}
	cur := snap(t, s).Cursor
	for i := 0; i < 9500; i++ { // more evictions than the ID memory holds
		c.Advance(time.Millisecond)
		s.Complete(completeOf(fmt.Sprintf("g%d", i), "", "succeeded", 200))
	}
	if ev := events(t, s, cur); !ev.Reset {
		t.Fatalf("expected reset, got %d events", len(ev.Events))
	}
}

func TestSuccessfulRetryClearsFailure(t *testing.T) {
	s, c := newStore(t, nil)
	fail := usageOf("rt")
	fail.RequestID = "rt-1"
	fail.Failed, fail.Failure.StatusCode, fail.Failure.Body = true, 429, ""
	fail.ResponseHeaders.RetryAfter = []string{"30"}
	s.Usage(fail)
	r := recByID(t, snap(t, s), "rt-1")
	if r.State != api.StateFailed || r.Status != 429 || r.RetryAfterS != 30 {
		t.Fatalf("after failure: %+v", r)
	}
	c.Advance(time.Second)
	ok := usageOf("rt")
	ok.RequestID = "rt-2"
	s.Usage(ok)
	r = recByID(t, snap(t, s), "rt-1")
	if r.State != api.StateDone || r.Status != 200 || r.RetryAfterS != 0 || r.Err != "" || r.Attempts != 2 {
		t.Fatalf("after success: %+v", r)
	}
}

func TestCompleteSuccessAfterFailedAttemptClearsFailure(t *testing.T) {
	s, _ := newStore(t, nil)
	fail := usageOf("rc")
	fail.RequestID = "rc-1"
	fail.Failed, fail.Failure.StatusCode, fail.Failure.Body = true, 429, ""
	fail.ResponseHeaders.RetryAfter = []string{"30"}
	s.Usage(fail)
	s.Complete(completeOf("rc-9", "rc", "succeeded", 0))
	for _, r := range snap(t, s).Records {
		if r.State != api.StateDone || r.Status != 200 || r.RetryAfterS != 0 || r.Err != "" {
			t.Fatalf("after complete: %+v", r)
		}
	}
}

const customKey = "zz9-CUSTOMKEY-q8w7e6r5t4y3u2i1"

func leakCheck(t *testing.T, s *store.Store, start string, secrets ...string) {
	t.Helper()
	outs := map[string][]byte{
		"snapshot": s.AppendSnapshot(nil),
		"events":   s.AppendEvents(nil, start),
		"records":  mustRecords(t, s, "", 2000),
	}
	for name, out := range outs {
		for _, sec := range secrets {
			if bytes.Contains(out, []byte(sec)) {
				t.Fatalf("%s leaks %q: %s", name, sec, out)
			}
		}
	}
}

func TestCompletionErrorNeverEchoesCustomKey(t *testing.T) {
	s, _ := newStore(t, nil)
	start := snap(t, s).Cursor
	cases := map[string]string{
		"embedded":   "invalid api key " + customKey + " for this endpoint",
		"quoted":     `{"error":{"message":"bad key '` + customKey + `'"}}`,
		"colon":      "key=" + customKey,
		"authHeader": "Authorization: Bearer " + customKey + " rejected",
	}
	for name, msg := range cases {
		id, tr := "k-"+name, "tk-"+name
		in := intercept(id, tr)
		in.Headers.Authorization = "Bearer " + customKey
		s.Before(in)
		cmp := completeOf(id, tr, "failed", 401)
		cmp.Error = msg
		s.Complete(cmp)
		r := recByID(t, snap(t, s), id)
		if r.Err == "" || strings.Contains(r.Err, customKey) {
			t.Fatalf("%s: err = %q", name, r.Err)
		}
	}
	leakCheck(t, s, start, customKey, "q8w7e6r5t4y3u2i1")
}

func TestCompletionErrorKeyKnownOnlyByDigest(t *testing.T) {
	// a short key the long-token rule and the regexes cannot find; only its digest can
	s, _ := newStore(t, nil)
	in := intercept("sh", "tsh")
	in.Headers.Authorization = "Bearer hunter2!"
	s.Before(in)
	cmp := completeOf("sh", "tsh", "failed", 401)
	cmp.Error = "unknown key hunter2! (or hunter2!, hunter2!)."
	s.Complete(cmp)
	if r := recByID(t, snap(t, s), "sh"); strings.Contains(r.Err, "hunter2") || r.Err == "" {
		t.Fatalf("err = %q", r.Err)
	}
	// a record without a client falls back to every key the store knows
	cmp = completeOf("nokey", "", "failed", 401)
	cmp.Error = "got hunter2! here"
	s.Complete(cmp)
	if r := recByID(t, snap(t, s), "nokey"); strings.Contains(r.Err, "hunter2") {
		t.Fatalf("err = %q", r.Err)
	}
}

func TestUsageErrorNeverEchoesCustomKey(t *testing.T) {
	s, _ := newStore(t, nil)
	start := snap(t, s).Cursor
	u := usageOf("uk")
	u.APIKey, u.IsNativeKey = customKey, true
	u.Failed, u.Failure.StatusCode = true, 401
	u.Failure.Body = "bad key " + customKey + " / " + customKey[:25] + "XYZXYZXYZXYZ0123"
	s.Usage(u)
	if r := recByID(t, snap(t, s), "usage-uk"); r.Err == "" || strings.Contains(r.Err, "CUSTOMKEY") {
		t.Fatalf("err = %q", r.Err)
	}
	leakCheck(t, s, start, customKey)
}

// Synthetic fixtures; not real credentials.
const (
	SyntheticKey    = "sk-ant-abcdefghijklmnop3f9a"
	PluginPrincipal = "pluginprincipal"
)
