// Package store keeps the live request records in memory and renders them as the
// page contract. Nothing here touches the disk.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"hash/maphash"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Roomen/cpa-live-routes/internal/config"
	"github.com/Roomen/cpa-live-routes/internal/redact"
	"github.com/Roomen/cpa-live-routes/internal/wire"
)

const (
	maxOpen       = 2048
	eventLog      = 8192
	maxEvents     = 2000
	snapshotCap   = 5000
	lostAfterMs   = int64(15 * 60 * 1000)
	mergeWindowMs = int64(5 * 60 * 1000)
	authListEvery = int64(60 * 1000)
	idMax         = 36
	traceMax      = 24
	nameMax       = 128
)

type state uint8

const (
	stQueued state = iota + 1
	stRouted
	stDone
	stFailed
	stCanceled
	stRejected
	stLost
)

var stateNames = [...]string{"", "queued", "routed", "done", "failed", "canceled", "rejected", "lost"}

const (
	fStream    uint8 = 1 << iota
	fTok             // usage counters present
	fUsageOnly       // created by usage; usage drives the state
)

// rec is one client request. It holds integers and fixed arrays only, apart from
// the optional sanitized error text.
type rec struct {
	t0, t1                                       int64
	arr                                          int64 // insertion time; non-decreasing in ring order, drives retention
	tokIn, tokOut, tokReason, tokCache, tokTotal uint32
	client, session, rule, exit                  int32
	ttft, lat, retryAfter                        int32
	mark                                         uint32
	status                                       uint16
	attempts                                     uint8
	state                                        state
	flags                                        uint8
	idLen, traceLen                              uint8
	id                                           [idMax]byte
	trace                                        [traceMax]byte
	err                                          string
}

const (
	evUpsert uint8 = iota + 1
	evEvict
	evConfig
	evDict
)

// idSlot is the ID of an evicted record, kept so evict events can name it.
type idSlot struct {
	n uint8
	b [idMax]byte
}

// evictIDCap bounds evictIDs; an evict event older than this many evictions can
// no longer name its records and the cursor is answered with a reset.
const evictIDCap = 8192

// evict events pack the eviction number of the first record in evictCountBits
// shifted past a count of at most evictCountMax.
const (
	evictCountBits = 21
	evictCountMax  = 1<<evictCountBits - 1
)

// event packs the kind into the low 3 bits of kindRef.
type event struct {
	seq     uint64
	kindRef uint64
}

func (e event) kind() uint8 { return uint8(e.kindRef & 7) }
func (e event) ref() uint64 { return e.kindRef >> 3 }

// Store is the in-memory request store. All methods are safe for concurrent use.
type Store struct {
	mu    sync.Mutex
	now   func() time.Time
	cfg   config.Config
	epoch string

	ring            []rec
	tail, next      uint64
	idIdx, traceIdx map[uint64]uint64
	open            map[uint64]struct{}
	evicted         int64
	dropped         int64
	lastSweep       int64
	renderGen       uint32
	evictN          int    // records evicted since the last flush
	evictFrom       uint64 // value of evicted when the pending run began
	lastArr, clk    int64
	evictIDs        []idSlot // IDs of the most recently evicted records, indexed by eviction number

	evs      [eventLog]event
	seq      uint64
	evN      int
	lastDict uint64
	authAt   int64

	clients  table[clientKey, clientVal]
	keys     table[string, keyVal]
	sessions table[string, sessionVal]
	rules    table[ruleVal, struct{}]
	exits    table[string, exitVal]
}

var hashSeed = maphash.MakeSeed()

// New returns an empty store. now may be nil for the wall clock.
func New(cfg config.Config, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	var e [4]byte
	_, _ = rand.Read(e[:])
	cfg = cfg.Clamp()
	return &Store{
		now:      now,
		cfg:      cfg,
		epoch:    hex.EncodeToString(e[:]),
		ring:     make([]rec, cfg.MaxRecords),
		idIdx:    map[uint64]uint64{},
		traceIdx: map[uint64]uint64{},
		open:     map[uint64]struct{}{},
		clients:  newTable[clientKey, clientVal](),
		keys:     newTable[string, keyVal](),
		sessions: newTable[string, sessionVal](),
		rules:    newTable[ruleVal, struct{}](),
		exits:    newTable[string, exitVal](),
	}
}

// Config returns the active configuration.
func (s *Store) Config() config.Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// SetConfig applies a new configuration, evicting at once if the limits shrank.
func (s *Store) SetConfig(cfg config.Config) {
	cfg = cfg.Clamp()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
	if cfg.MaxRecords != len(s.ring) {
		s.resize(cfg.MaxRecords)
	}
	for _, id := range s.keys.ids(nil) {
		sl := s.keys.get(id)
		if label := redact.LabelFor(sl.val.sum, cfg.APIKeyLabels); label != sl.val.label {
			sl.val.label = label
			sl.ver = s.touchVer()
		}
	}
	s.expire(s.nowMs())
	s.emit(evConfig, 0)
}

func (s *Store) resize(n int) {
	for s.next-s.tail > uint64(n) {
		s.evictTail()
	}
	s.flushEvict()
	ring := make([]rec, n)
	for p := s.tail; p < s.next; p++ {
		ring[p%uint64(n)] = *s.at(p)
	}
	s.ring = ring
}

func (s *Store) nowMs() int64 { return s.now().UnixMilli() }

func (s *Store) at(pos uint64) *rec { return &s.ring[pos%uint64(len(s.ring))] }

func (s *Store) touchVer() uint64 {
	v := s.seq + 1
	s.lastDict = v
	return v
}

func (s *Store) emit(kind uint8, ref uint64) {
	s.seq++
	s.evs[s.seq%eventLog] = event{seq: s.seq, kindRef: ref<<3 | uint64(kind)}
	if s.evN < eventLog {
		s.evN++
	}
}

func hashStr(v string) uint64 { return maphash.String(hashSeed, v) }

func hashBytes(v []byte) uint64 { return maphash.Bytes(hashSeed, v) }

func clampTok(v int64) uint32 { return uint32(max(0, min(v, 1<<32-1))) }

func fit(v string, n int) string {
	if len(v) > n {
		return v[:n]
	}
	return v
}

// maintain runs the lazy housekeeping: retention eviction and the lost sweep.
func (s *Store) maintain() int64 {
	now := s.nowMs()
	s.clk = now
	s.expire(now)
	if now-s.lastSweep >= 1000 {
		s.lastSweep = now
		for pos := range s.open {
			r := s.at(pos)
			if now-r.t0 >= lostAfterMs {
				r.state, r.t1 = stLost, now
				delete(s.open, pos)
				s.emit(evUpsert, pos)
			}
		}
	}
	return now
}

// expire drops records inserted more than the retention ago. Insertion time is
// non-decreasing in ring order, so the oldest records are always at the tail,
// whatever T0 a late usage record carries.
func (s *Store) expire(now int64) {
	cutoff := now - int64(s.cfg.RetentionMinutes)*60_000
	for s.tail < s.next && s.at(s.tail).arr < cutoff {
		s.evictTail()
	}
	s.flushEvict()
}

func (s *Store) evictTail() {
	pos := s.tail
	r := s.at(pos)
	s.releaseClient(r.client)
	s.releaseSession(r.session)
	s.rules.release(r.rule)
	s.exits.release(r.exit)
	if h := hashBytes(r.id[:r.idLen]); s.idIdx[h] == pos {
		delete(s.idIdx, h)
	}
	if h := hashBytes(r.trace[:r.traceLen]); r.traceLen > 0 && s.traceIdx[h] == pos {
		delete(s.traceIdx, h)
	}
	delete(s.open, pos)
	if s.evictN == 0 {
		s.evictFrom = uint64(s.evicted)
	}
	slot := idSlot{n: r.idLen, b: r.id}
	if len(s.evictIDs) < evictIDCap {
		s.evictIDs = append(s.evictIDs, slot)
	} else {
		s.evictIDs[uint64(s.evicted)%evictIDCap] = slot
	}
	s.evictN++
	s.evicted++
	*r = rec{}
	s.tail++
}

func (s *Store) flushEvict() {
	if s.evictN == 0 {
		return
	}
	from, n := s.evictFrom, s.evictN
	s.evictN = 0
	for n > 0 {
		c := min(n, evictCountMax)
		s.emit(evEvict, from<<evictCountBits|uint64(c))
		from += uint64(c)
		n -= c
	}
}

func (s *Store) releaseClient(id int32) {
	if sl := s.clients.release(id); sl != nil {
		s.keys.release(sl.val.key)
	}
}

func (s *Store) releaseSession(id int32) {
	if sl := s.sessions.release(id); sl != nil {
		s.releaseSession(sl.val.parent)
		s.releaseClient(sl.val.client)
	}
}

func (s *Store) setClient(r *rec, id int32) {
	if r.client != id {
		s.clients.acquire(id)
		s.releaseClient(r.client)
		r.client = id
	}
}

func (s *Store) setSession(r *rec, id int32) {
	if r.session != id {
		s.sessions.acquire(id)
		s.releaseSession(r.session)
		r.session = id
	}
}

func (s *Store) setRule(r *rec, id int32) {
	if r.rule != id {
		s.rules.acquire(id)
		s.rules.release(r.rule)
		r.rule = id
	}
}

func (s *Store) setExit(r *rec, id int32) {
	if r.exit != id {
		s.exits.acquire(id)
		s.exits.release(r.exit)
		r.exit = id
	}
}

func (s *Store) internKey(k *redact.Key) int32 {
	if id, sl := s.keys.find(k.FP); sl != nil {
		return id
	}
	return s.keys.add(k.FP, keyVal{
		label: redact.LabelFor(k.Sum, s.cfg.APIKeyLabels), mask: k.Mask, sum: k.Sum,
	}, s.touchVer())
}

func (s *Store) internClient(p *clientPrep, keyID int32) int32 {
	ck := clientKey{kind: p.kind, label: p.label, key: keyID, ip: p.ip}
	if id, sl := s.clients.find(ck); sl != nil {
		return id
	}
	s.keys.acquire(keyID)
	return s.clients.add(ck, clientVal{ua: p.ua, key: keyID}, s.touchVer())
}

func (s *Store) internSession(sid, parent string, client int32) int32 {
	sid, parent = fit(sid, nameMax), fit(parent, nameMax)
	if sid == "" {
		return 0
	}
	if parent == sid {
		parent = ""
	}
	if id, sl := s.sessions.find(sid); sl != nil {
		if sl.val.parent == 0 && parent != "" {
			sl.val.parent = s.internSession(parent, "", client)
			s.sessions.acquire(sl.val.parent)
			sl.ver = s.touchVer()
		}
		return id
	}
	var pid int32
	if parent != "" {
		pid = s.internSession(parent, "", client)
	}
	id := s.sessions.add(sid, sessionVal{parent: pid, client: client}, s.touchVer())
	s.sessions.acquire(pid)
	s.clients.acquire(client)
	return id
}

func (s *Store) internRule(alias, model string) int32 {
	alias, model = fit(alias, nameMax), fit(model, nameMax)
	if alias == "" {
		alias = model
	}
	if model == "" {
		model = alias
	}
	if alias == "" {
		return 0
	}
	k := ruleVal{alias: alias, model: model}
	if id, sl := s.rules.find(k); sl != nil {
		return id
	}
	return s.rules.add(k, struct{}{}, s.touchVer())
}

func (s *Store) internExit(authIndex, provider string) int32 {
	authIndex = fit(authIndex, nameMax)
	if authIndex == "" {
		return 0
	}
	if id, sl := s.exits.find(authIndex); sl != nil {
		if provider != "" && sl.val.provider == "" {
			sl.val.provider = fit(provider, 64)
			sl.ver = s.touchVer()
		}
		return id
	}
	return s.exits.add(authIndex, exitVal{provider: fit(provider, 64)}, s.touchVer())
}

// clientPrep is the redacted identity of a client, computed before taking the lock.
type clientPrep struct {
	kind, label, ua, ip string
	key                 redact.Key
	hasKey              bool
}

func prepHeaders(h *wire.Headers) clientPrep {
	p := clientPrep{ip: redact.ClientIP(h.XForwardedFor, h.XRealIP, h.CFConnectingIP)}
	if raw := redact.KeyFromHeaders(h.Authorization, h.XAPIKey, h.GoogAPIKey); raw != "" {
		p.key, p.hasKey = redact.NewKey(raw), true
	}
	ua := redact.ParseUA(h.UA)
	p.kind, p.label, p.ua = ua.Kind, ua.Label, ua.Raw
	if p.kind == "" {
		if !p.hasKey && p.ip == "" {
			return clientPrep{}
		}
		p.kind, p.label = redact.KindOther, "unknown"
	}
	return p
}

func (s *Store) applyClient(r *rec, p *clientPrep) {
	if r.client != 0 || p.kind == "" {
		return
	}
	var keyID int32
	if p.hasKey {
		keyID = s.internKey(&p.key)
	}
	s.setClient(r, s.internClient(p, keyID))
}

func (s *Store) applySession(r *rec, sid, parent string) {
	if sid != "" {
		s.setSession(r, s.internSession(sid, parent, r.client))
	}
}

func (s *Store) applyRule(r *rec, alias, model string) {
	if id := s.internRule(alias, model); id != 0 {
		s.setRule(r, id)
	}
}

func (s *Store) byID(id string) (*rec, uint64) {
	id = fit(id, idMax)
	pos, ok := s.idIdx[hashStr(id)]
	if !ok || pos < s.tail || pos >= s.next {
		return nil, 0
	}
	r := s.at(pos)
	if string(r.id[:r.idLen]) != id {
		return nil, 0
	}
	return r, pos
}

func (s *Store) byTrace(trace string, now int64) (*rec, uint64) {
	trace = fit(trace, traceMax)
	if trace == "" {
		return nil, 0
	}
	pos, ok := s.traceIdx[hashStr(trace)]
	if !ok || pos < s.tail || pos >= s.next {
		return nil, 0
	}
	r := s.at(pos)
	if string(r.trace[:r.traceLen]) != trace {
		return nil, 0
	}
	if r.state != stQueued && r.state != stRouted && now-r.t1 > mergeWindowMs {
		return nil, 0
	}
	return r, pos
}

func (s *Store) create(id, trace string, t0 int64, st state) (*rec, uint64) {
	if s.next-s.tail >= uint64(len(s.ring)) {
		s.evictTail()
		s.flushEvict()
	}
	pos := s.next
	s.next++
	id, trace = fit(id, idMax), fit(trace, traceMax)
	r := s.at(pos)
	s.lastArr = max(s.lastArr, s.clk)
	*r = rec{t0: t0, arr: s.lastArr, state: st, idLen: uint8(len(id)), traceLen: uint8(len(trace))}
	copy(r.id[:], id)
	copy(r.trace[:], trace)
	s.idIdx[hashStr(id)] = pos
	if trace != "" {
		s.traceIdx[hashStr(trace)] = pos
	}
	return r, pos
}

// createOpen creates an in-flight record, or reports it dropped when the table is full.
func (s *Store) createOpen(id, trace string, t0 int64, st state) (*rec, uint64) {
	if len(s.open) >= maxOpen {
		s.dropped++
		return nil, 0
	}
	r, pos := s.create(id, trace, t0, st)
	s.open[pos] = struct{}{}
	return r, pos
}

func (s *Store) finish(r *rec, pos uint64, st state, t1 int64) {
	delete(s.open, pos)
	r.state = st
	r.t1 = max(t1, r.t0)
}

func setFlag(r *rec, flag uint8, on bool) {
	if on {
		r.flags |= flag
	} else {
		r.flags &^= flag
	}
}

// Before records request.intercept_before.
func (s *Store) Before(in *wire.Intercept) {
	if in.RequestID == "" {
		return
	}
	cp := prepHeaders(&in.Headers)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.maintain()
	if r, _ := s.byID(in.RequestID); r != nil {
		return
	}
	r, pos := s.createOpen(in.RequestID, in.TraceID, now, stQueued)
	if r == nil {
		return
	}
	setFlag(r, fStream, in.Stream)
	s.applyClient(r, &cp)
	s.applySession(r, in.SessionID, in.ParentSession)
	s.applyRule(r, in.RequestedModel, in.Model)
	s.emit(evUpsert, pos)
}

// After records request.intercept_after: one call per credential attempt.
func (s *Store) After(in *wire.Intercept) {
	if in.RequestID == "" {
		return
	}
	cp := prepHeaders(&in.Headers)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.maintain()
	r, pos := s.byID(in.RequestID)
	if r == nil {
		if r, pos = s.createOpen(in.RequestID, in.TraceID, now, stRouted); r == nil {
			return
		}
		setFlag(r, fStream, in.Stream)
	}
	s.applyClient(r, &cp)
	s.applySession(r, in.SessionID, in.ParentSession)
	s.applyRule(r, in.RequestedModel, in.Model)
	if id := s.internExit(in.AuthIndex, ""); id != 0 {
		s.setExit(r, id)
	}
	if r.attempts < 255 {
		r.attempts++
	}
	if r.state == stQueued || r.state == stRouted {
		r.state = stRouted
	}
	s.emit(evUpsert, pos)
}

func outcomeState(outcome string) state {
	switch outcome {
	case "succeeded":
		return stDone
	case "canceled":
		return stCanceled
	case "rejected":
		return stRejected
	}
	return stFailed
}

// Complete records request.complete.
func (s *Store) Complete(c *wire.Completion) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.maintain()
	r, pos := s.byID(c.RequestID)
	if r == nil {
		if cand, cpos := s.byTrace(c.TraceID, now); cand != nil && cand.flags&fUsageOnly != 0 {
			r, pos = cand, cpos
		}
	}
	if r == nil {
		t0 := now
		if !c.StartedAt.IsZero() {
			t0 = min(c.StartedAt.UnixMilli(), now)
		}
		r, pos = s.create(c.RequestID, c.TraceID, t0, stDone)
		setFlag(r, fStream, c.Stream)
	}
	r.flags &^= fUsageOnly
	if r.rule == 0 {
		s.applyRule(r, c.RequestedModel, c.Model)
	}
	s.applySession(r, c.SessionID(), c.ParentSession())
	if r.exit == 0 {
		s.setExit(r, s.internExit(c.AuthIndex(), ""))
	}
	t1 := now
	if !c.CompletedAt.IsZero() {
		t1 = min(c.CompletedAt.UnixMilli(), now)
	}
	st := outcomeState(c.Outcome)
	s.finish(r, pos, st, t1)
	if c.StatusCode > 0 && c.StatusCode < 1000 {
		r.status = uint16(c.StatusCode)
	}
	if st == stDone {
		// a successful final attempt wipes what earlier failed attempts left
		r.err, r.retryAfter = "", 0
		if r.status == 0 || r.status >= 400 && c.StatusCode <= 0 {
			r.status = 200
		}
	} else if c.Error != "" && r.err == "" {
		// The completion carries no API key. The raw key is never kept, so the
		// digest of the request's key (from the interceptor headers) finds it in
		// the text, on top of the key-shape regexes and the generic long-token rule.
		var sums [maxSums][32]byte
		r.err = redact.ErrorTextSums(c.Error, s.keySums(r, sums[:0]))
	}
	if r.lat == 0 {
		r.lat = clampMs(r.t1 - r.t0)
	}
	s.emit(evUpsert, pos)
}

// Usage records usage.handle. Records merge by trace; an unmatched usage
// record becomes a record of its own.
func (s *Store) Usage(u *wire.Usage) {
	var key redact.Key
	hasKey := false
	if u.APIKey != "" {
		if u.IsNativeKey || u.AccessProvider == "config-inline" {
			key = redact.NewKey(u.APIKey)
		} else {
			key = redact.Principal(u.AccessProvider, u.APIKey)
		}
		hasKey = true
	}
	var errText string
	if u.Failed {
		errText = redact.ErrorText(u.Failure.Body, u.APIKey)
	}
	kind, label := redact.ClientFromSession(u.SessionID)
	if kind == "" && hasKey {
		kind, label = redact.KindOther, "unknown"
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.maintain()
	lat := clampMs(u.Latency.Milliseconds())
	r, pos := s.byTrace(u.TraceID, now)
	if r == nil {
		t0 := now - int64(lat)
		if !u.RequestedAt.IsZero() {
			t0 = min(u.RequestedAt.UnixMilli(), now)
		}
		if t0 < now-int64(s.cfg.RetentionMinutes)*60_000 {
			return
		}
		id := u.RequestID
		if id == "" {
			id = "u" + strconv.FormatUint(s.seq+1, 36)
		}
		r, pos = s.create(id, u.TraceID, t0, stDone)
		r.flags |= fUsageOnly
		setFlag(r, fStream, u.Stream)
	}

	if r.client == 0 && kind != "" {
		cp := clientPrep{kind: kind, label: label, key: key, hasKey: hasKey}
		s.applyClient(r, &cp)
	}
	s.applySession(r, u.SessionID, u.ParentSessionID)
	if r.rule == 0 {
		s.applyRule(r, u.Alias, u.Model)
	}
	if id := s.internExit(u.AuthIndex, u.Provider); id != 0 {
		s.setExit(r, id)
	}
	if u.Stream {
		r.flags |= fStream
	}
	d := &u.Detail
	if d.InputTokens != 0 || d.OutputTokens != 0 || d.TotalTokens != 0 {
		r.flags |= fTok
		r.tokIn, r.tokOut, r.tokReason = clampTok(d.InputTokens), clampTok(d.OutputTokens), clampTok(d.ReasoningTokens)
		r.tokCache = clampTok(d.CachedTokens)
		if r.tokCache == 0 {
			r.tokCache = clampTok(d.CacheReadTokens)
		}
		r.tokTotal = clampTok(d.TotalTokens)
		if r.tokTotal == 0 {
			r.tokTotal = clampTok(d.InputTokens + d.OutputTokens)
		}
	}
	r.ttft, r.lat = clampMs(u.TTFT.Milliseconds()), lat
	if u.Failed {
		if u.Failure.StatusCode > 0 && u.Failure.StatusCode < 1000 {
			r.status = uint16(u.Failure.StatusCode)
		}
		r.err = errText
		r.retryAfter = 0
		if r.status == 429 && len(u.ResponseHeaders.RetryAfter) > 0 {
			if n, err := strconv.Atoi(strings.TrimSpace(u.ResponseHeaders.RetryAfter[0])); err == nil && n > 0 {
				r.retryAfter = clampMs(int64(n))
			}
		}
	} else {
		// success: whatever an earlier failed attempt of this request left is stale
		r.err, r.retryAfter = "", 0
		if r.status >= 400 && (r.flags&fUsageOnly != 0 || r.state == stDone) {
			r.status = 0
		}
	}
	if r.flags&fUsageOnly != 0 {
		if r.attempts < 255 {
			r.attempts++
		}
		st := stDone
		if u.Failed {
			st = stFailed
		}
		r.state, r.t1 = st, max(r.t0+int64(lat), r.t0)
	}
	if !u.Failed && r.status == 0 && r.state == stDone {
		r.status = 200
	}
	s.emit(evUpsert, pos)
}

// keySums appends the digests of the client key of r, or of every known key
// (bounded) when the record has no client yet.
func (s *Store) keySums(r *rec, dst [][32]byte) [][32]byte {
	if r.client != 0 {
		if cl := s.clients.get(r.client); cl != nil {
			if k := s.keys.get(cl.val.key); k != nil {
				return append(dst, k.val.sum)
			}
		}
		return dst
	}
	for _, k := range s.keys.slots {
		if k != nil && len(dst) < maxSums {
			dst = append(dst, k.val.sum)
		}
	}
	return dst
}

const maxSums = 32

func clampMs(v int64) int32 {
	return int32(max(0, min(v, 1<<31-1)))
}

// AuthEntry is the part of a host credential listing the plugin uses.
type AuthEntry struct {
	AuthIndex, Provider, Label, Email string
}

// WantAuthList reports whether exit labels should be fetched now. It returns
// true at most once per minute and only while an exit lacks a label.
func (s *Store) WantAuthList() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.nowMs()
	if s.authAt != 0 && now-s.authAt < authListEvery {
		return false
	}
	for _, sl := range s.exits.slots {
		if sl != nil && sl.val.label == "" && !sl.val.tried {
			s.authAt = now
			return true
		}
	}
	return false
}

// ApplyAuthList resolves exit labels from a host credential listing.
func (s *Store) ApplyAuthList(files []AuthEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	byIndex := make(map[string]*AuthEntry, len(files))
	for i := range files {
		byIndex[files[i].AuthIndex] = &files[i]
	}
	changed := false
	for _, sl := range s.exits.slots {
		if sl == nil {
			continue
		}
		sl.val.tried = true
		f := byIndex[sl.key]
		if f == nil {
			continue
		}
		label := f.Label
		if strings.Contains(label, "@") {
			label = redact.MaskEmail(label)
		}
		if label == "" {
			label = redact.MaskEmail(f.Email)
		}
		label = fit(label, 64)
		if label != sl.val.label || (sl.val.provider == "" && f.Provider != "") {
			sl.val.label = label
			if sl.val.provider == "" {
				sl.val.provider = fit(f.Provider, 64)
			}
			sl.ver = s.touchVer()
			changed = true
		}
	}
	if changed {
		s.emit(evDict, 0)
	}
}
