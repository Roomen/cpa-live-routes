package store

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Roomen/cpa-live-routes/internal/config"
	"github.com/Roomen/cpa-live-routes/internal/redact"
)

const contractVersion = 2

const hexDigits = "0123456789abcdef"

// appendString appends s as a JSON string literal.
func appendString[T ~string | ~[]byte](dst []byte, s T) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			dst = append(dst, '\\', c)
		case c == '\n':
			dst = append(dst, '\\', 'n')
		case c == '\r':
			dst = append(dst, '\\', 'r')
		case c == '\t':
			dst = append(dst, '\\', 't')
		case c < 0x20 || c == 0x7f:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
		case c < utf8.RuneSelf:
			dst = append(dst, c)
		default:
			r, size := utf8.DecodeRuneInString(string(s[i:min(i+utf8.UTFMax, len(s))]))
			if r == utf8.RuneError && size == 1 {
				dst = append(dst, `�`...)
			} else {
				for j := 0; j < size; j++ {
					dst = append(dst, s[i+j])
				}
			}
			i += size - 1
		}
	}
	return append(dst, '"')
}

func appendInt(dst []byte, v int64) []byte { return strconv.AppendInt(dst, v, 10) }

func appendStrField[T ~string | ~[]byte](dst []byte, name string, v T, omitEmpty bool) []byte {
	if omitEmpty && len(v) == 0 {
		return dst
	}
	dst = append(dst, ',', '"')
	dst = append(dst, name...)
	dst = append(dst, '"', ':')
	return appendString(dst, v)
}

func appendIntField(dst []byte, name string, v int64) []byte {
	if v == 0 {
		return dst
	}
	dst = append(dst, ',', '"')
	dst = append(dst, name...)
	dst = append(dst, '"', ':')
	return appendInt(dst, v)
}

func appendBoolField(dst []byte, name string, v bool) []byte {
	dst = append(dst, ',', '"')
	dst = append(dst, name...)
	dst = append(dst, '"', ':')
	return strconv.AppendBool(dst, v)
}

func appendConfig(dst []byte, c *config.Config) []byte {
	dst = append(dst, `{"active_window_s":`...)
	dst = appendInt(dst, int64(c.ActiveWindowMinutes)*60)
	dst = append(dst, `,"retention_s":`...)
	dst = appendInt(dst, int64(c.RetentionMinutes)*60)
	dst = append(dst, `,"max_records":`...)
	dst = appendInt(dst, int64(c.MaxRecords))
	dst = appendStrField(dst, "animation_speed", c.AnimationSpeed, false)
	dst = appendBoolField(dst, "reduced_animation", c.ReducedAnimation)
	dst = appendBoolField(dst, "inflight_tracking", c.InflightTracking)
	dst = appendStrField(dst, "layout", c.Layout, false)
	dst = appendBoolField(dst, "require_key", c.RequireKey)
	return append(dst, '}')
}

func (r *rec) appendJSON(dst []byte) []byte {
	dst = append(dst, `{"id":`...)
	dst = appendString(dst, r.id[:r.idLen])
	dst = appendStrField(dst, "trace", r.trace[:r.traceLen], true)
	dst = append(dst, `,"t0":`...)
	dst = appendInt(dst, r.t0)
	dst = appendIntField(dst, "t1", r.t1)
	dst = append(dst, `,"state":"`...)
	dst = append(dst, stateNames[r.state]...)
	dst = append(dst, '"')
	dst = appendIntField(dst, "client", int64(r.client))
	dst = appendIntField(dst, "session", int64(r.session))
	dst = appendIntField(dst, "rule", int64(r.rule))
	dst = appendIntField(dst, "exit", int64(r.exit))
	if r.flags&fStream != 0 {
		dst = append(dst, `,"stream":true`...)
	}
	dst = appendIntField(dst, "status", int64(r.status))
	dst = appendIntField(dst, "ttft", int64(r.ttft))
	dst = appendIntField(dst, "lat", int64(r.lat))
	if r.flags&fTok != 0 {
		dst = append(dst, `,"tok":{"in":`...)
		dst = appendInt(dst, int64(r.tokIn))
		dst = append(dst, `,"out":`...)
		dst = appendInt(dst, int64(r.tokOut))
		dst = appendIntField(dst, "reason", int64(r.tokReason))
		dst = appendIntField(dst, "cache", int64(r.tokCache))
		dst = append(dst, `,"total":`...)
		dst = appendInt(dst, int64(r.tokTotal))
		dst = append(dst, '}')
	}
	dst = appendIntField(dst, "attempts", int64(r.attempts))
	dst = appendStrField(dst, "err", r.err, true)
	dst = appendIntField(dst, "retry_after", int64(r.retryAfter))
	return append(dst, '}')
}

// dictSet selects the dictionary entries to render.
type dictSet struct {
	clients, keys, sessions, rules, exits []int32
}

func (s *Store) appendDict(dst []byte, d *dictSet) []byte {
	dst = append(dst, `"dict":{`...)
	sep := byte(0)
	group := func(name string) {
		if sep != 0 {
			dst = append(dst, sep)
		}
		sep = ','
		dst = append(dst, '"')
		dst = append(dst, name...)
		dst = append(dst, `":[`...)
	}
	if len(d.clients) > 0 {
		group("clients")
		for i, id := range d.clients {
			if i > 0 {
				dst = append(dst, ',')
			}
			sl := s.clients.get(id)
			v := &sl.val
			dst = append(dst, `{"id":`...)
			dst = appendInt(dst, int64(id))
			dst = appendStrField(dst, "kind", sl.key.kind, false)
			dst = appendStrField(dst, "label", sl.key.label, false)
			dst = appendStrField(dst, "ua", v.ua, true)
			dst = appendStrField(dst, "ip", sl.key.ip, true)
			dst = appendIntField(dst, "key", int64(v.key))
			dst = append(dst, '}')
		}
		dst = append(dst, ']')
	}
	if len(d.keys) > 0 {
		group("keys")
		for i, id := range d.keys {
			if i > 0 {
				dst = append(dst, ',')
			}
			sl := s.keys.get(id)
			v := &sl.val
			dst = append(dst, `{"id":`...)
			dst = appendInt(dst, int64(id))
			dst = appendStrField(dst, "label", v.label, true)
			dst = appendStrField(dst, "mask", v.mask, false)
			dst = appendStrField(dst, "fp", sl.key, false)
			dst = append(dst, '}')
		}
		dst = append(dst, ']')
	}
	if len(d.sessions) > 0 {
		group("sessions")
		for i, id := range d.sessions {
			if i > 0 {
				dst = append(dst, ',')
			}
			sl := s.sessions.get(id)
			v := &sl.val
			dst = append(dst, `{"id":`...)
			dst = appendInt(dst, int64(id))
			dst = appendStrField(dst, "sid", sl.key, false)
			dst = appendStrField(dst, "short", redact.SessionShort(sl.key), false)
			dst = appendIntField(dst, "parent", int64(v.parent))
			dst = appendIntField(dst, "client", int64(v.client))
			dst = append(dst, '}')
		}
		dst = append(dst, ']')
	}
	if len(d.rules) > 0 {
		group("rules")
		for i, id := range d.rules {
			if i > 0 {
				dst = append(dst, ',')
			}
			v := &s.rules.get(id).key
			dst = append(dst, `{"id":`...)
			dst = appendInt(dst, int64(id))
			dst = appendStrField(dst, "alias", v.alias, false)
			dst = appendStrField(dst, "model", v.model, false)
			dst = append(dst, '}')
		}
		dst = append(dst, ']')
	}
	if len(d.exits) > 0 {
		group("exits")
		for i, id := range d.exits {
			if i > 0 {
				dst = append(dst, ',')
			}
			sl := s.exits.get(id)
			v := &sl.val
			dst = append(dst, `{"id":`...)
			dst = appendInt(dst, int64(id))
			dst = appendStrField(dst, "auth_index", sl.key, false)
			dst = appendStrField(dst, "provider", v.provider, true)
			dst = appendStrField(dst, "label", v.label, true)
			dst = append(dst, '}')
		}
		dst = append(dst, ']')
	}
	return append(dst, '}')
}

func (s *Store) allDict() *dictSet {
	return &dictSet{
		clients:  s.clients.ids(nil),
		keys:     s.keys.ids(nil),
		sessions: s.sessions.ids(nil),
		rules:    s.rules.ids(nil),
		exits:    s.exits.ids(nil),
	}
}

func (s *Store) dictSince(since uint64) *dictSet {
	if s.lastDict <= since {
		return &dictSet{}
	}
	return &dictSet{
		clients:  s.clients.ids(func(sl *slot[clientKey, clientVal]) bool { return sl.ver > since }),
		keys:     s.keys.ids(func(sl *slot[string, keyVal]) bool { return sl.ver > since }),
		sessions: s.sessions.ids(func(sl *slot[string, sessionVal]) bool { return sl.ver > since }),
		rules:    s.rules.ids(func(sl *slot[ruleVal, struct{}]) bool { return sl.ver > since }),
		exits:    s.exits.ids(func(sl *slot[string, exitVal]) bool { return sl.ver > since }),
	}
}

// dictFor collects the entries the given records refer to, with their dependencies.
func (s *Store) dictFor(positions []uint64) *dictSet {
	var d dictSet
	seenClient, seenKey, seenSess := map[int32]bool{}, map[int32]bool{}, map[int32]bool{}
	seenRule, seenExit := map[int32]bool{}, map[int32]bool{}
	addClient := func(id int32) {
		if id == 0 || seenClient[id] {
			return
		}
		seenClient[id] = true
		d.clients = append(d.clients, id)
		if k := s.clients.get(id).val.key; k != 0 && !seenKey[k] {
			seenKey[k] = true
			d.keys = append(d.keys, k)
		}
	}
	var addSession func(id int32)
	addSession = func(id int32) {
		if id == 0 || seenSess[id] {
			return
		}
		seenSess[id] = true
		d.sessions = append(d.sessions, id)
		v := &s.sessions.get(id).val
		addClient(v.client)
		addSession(v.parent)
	}
	for _, pos := range positions {
		r := s.at(pos)
		addClient(r.client)
		addSession(r.session)
		if r.rule != 0 && !seenRule[r.rule] {
			seenRule[r.rule] = true
			d.rules = append(d.rules, r.rule)
		}
		if r.exit != 0 && !seenExit[r.exit] {
			seenExit[r.exit] = true
			d.exits = append(d.exits, r.exit)
		}
	}
	slices.Sort(d.clients)
	slices.Sort(d.keys)
	slices.Sort(d.sessions)
	slices.Sort(d.rules)
	slices.Sort(d.exits)
	return &d
}

func (s *Store) cursor() string { return s.epoch + ":" + strconv.FormatUint(s.seq, 10) }

// oldest is the smallest T0 among the kept records, 0 when there are none. Ring
// order is arrival order, so the tail is not necessarily the oldest by T0.
func (s *Store) oldest() int64 {
	var m int64
	for pos := s.tail; pos < s.next; pos++ {
		if t := s.at(pos).t0; m == 0 || t < m {
			m = t
		}
	}
	return m
}

func (s *Store) posCursor(pos uint64) string {
	return s.epoch + ":" + strconv.FormatUint(pos, 10)
}

func appendRecords(dst []byte, s *Store, positions []uint64) []byte {
	dst = append(dst, `"records":[`...)
	for i, pos := range positions {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = s.at(pos).appendJSON(dst)
	}
	return append(dst, ']')
}

// AppendSnapshot appends the Snapshot JSON.
func (s *Store) AppendSnapshot(dst []byte) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.maintain()

	var positions []uint64
	completed, truncated := 0, false
	histPos := s.next // lowest ring position of an included completed record
	for pos := s.next; pos > s.tail; pos-- {
		r := s.at(pos - 1)
		if r.state == stQueued || r.state == stRouted {
			positions = append(positions, pos-1)
		} else if completed < snapshotCap {
			completed++
			histPos = pos - 1
			positions = append(positions, pos-1)
		} else {
			truncated = true
		}
	}

	dst = append(dst, `{"v":`...)
	dst = appendInt(dst, contractVersion)
	dst = appendStrField(dst, "epoch", s.epoch, false)
	dst = appendStrField(dst, "cursor", s.cursor(), false)
	dst = append(dst, `,"now":`...)
	dst = appendInt(dst, now)
	dst = append(dst, `,"oldest":`...)
	dst = appendInt(dst, s.oldest())
	dst = append(dst, `,"config":`...)
	dst = appendConfig(dst, &s.cfg)
	dst = append(dst, ',')
	dst = s.appendDict(dst, s.allDict())
	dst = append(dst, ',')
	dst = appendRecords(dst, s, positions)
	if truncated {
		dst = append(dst, `,"truncated":true`...)
		dst = appendStrField(dst, "next", s.posCursor(histPos), false)
	}
	dst = append(dst, `,"stats":{"records":`...)
	dst = appendInt(dst, int64(s.next-s.tail))
	dst = append(dst, `,"inflight":`...)
	dst = appendInt(dst, int64(len(s.open)))
	dst = append(dst, `,"evicted":`...)
	dst = appendInt(dst, s.evicted)
	dst = append(dst, `,"inflight_dropped":`...)
	dst = appendInt(dst, s.dropped)
	return append(dst, `}}`...)
}

// AppendEvents appends the Events JSON for a cursor.
func (s *Store) AppendEvents(dst []byte, since string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.maintain()

	from, ok := s.parseCursor(since)
	if !ok {
		return s.appendReset(dst, now)
	}

	s.renderGen++
	n0 := len(dst)
	var body []byte
	emitted, last := 0, from
	more := false
	for q := from + 1; q <= s.seq; q++ {
		if emitted >= maxEvents {
			more = true
			break
		}
		last = q
		ev := s.evs[q%eventLog]
		switch ev.kind() {
		case evUpsert:
			if ev.ref() < s.tail {
				continue
			}
			r := s.at(ev.ref())
			if r.mark == s.renderGen {
				continue
			}
			r.mark = s.renderGen
			body = appendEventHead(body, emitted, q, "upsert")
			body = append(body, `,"rec":`...)
			body = r.appendJSON(body)
		case evEvict:
			from, n := ev.ref()>>evictCountBits, ev.ref()&evictCountMax
			if s.evicted > evictIDCap && from+evictIDCap < uint64(s.evicted) {
				// the IDs of this run were overwritten: the page cannot be told what went
				return s.appendReset(dst[:n0], now)
			}
			body = appendEventHead(body, emitted, q, "evict")
			body = append(body, `,"ids":[`...)
			for i := uint64(0); i < n; i++ {
				sl := &s.evictIDs[(from+i)%evictIDCap]
				if i > 0 {
					body = append(body, ',')
				}
				body = appendString(body, sl.b[:sl.n])
			}
			body = append(body, ']')
		case evConfig:
			body = appendEventHead(body, emitted, q, "config")
			body = append(body, `,"config":`...)
			body = appendConfig(body, &s.cfg)
		default:
			continue
		}
		body = append(body, '}')
		emitted++
	}

	dst = append(dst, `{"cursor":`...)
	dst = appendString(dst, s.epoch+":"+strconv.FormatUint(last, 10))
	dst = append(dst, `,"now":`...)
	dst = appendInt(dst, now)
	if more {
		dst = append(dst, `,"more":true`...)
	}
	dst = append(dst, ',')
	dst = s.appendDict(dst, s.dictSince(from))
	dst = append(dst, `,"events":[`...)
	dst = append(dst, body...)
	return append(dst, `]}`...)
}

func (s *Store) appendReset(dst []byte, now int64) []byte {
	dst = append(dst, `{"cursor":`...)
	dst = appendString(dst, s.cursor())
	dst = append(dst, `,"now":`...)
	dst = appendInt(dst, now)
	return append(dst, `,"reset":true,"dict":{},"events":[]}`...)
}

func appendEventHead(dst []byte, n int, seq uint64, typ string) []byte {
	if n > 0 {
		dst = append(dst, ',')
	}
	dst = append(dst, `{"seq":`...)
	dst = strconv.AppendUint(dst, seq, 10)
	dst = append(dst, `,"type":"`...)
	dst = append(dst, typ...)
	return append(dst, '"')
}

// parseCursor returns the sequence number of a usable cursor.
func (s *Store) parseCursor(c string) (uint64, bool) {
	n := len(s.epoch)
	if len(c) < n+2 || c[:n] != s.epoch || c[n] != ':' {
		return 0, false
	}
	v, err := strconv.ParseUint(c[n+1:], 10, 64)
	if err != nil || v > s.seq {
		return 0, false
	}
	first := s.seq - uint64(s.evN) + 1
	if v+1 < first {
		return 0, false
	}
	return v, true
}

// AppendRecords appends the RecordsPage JSON: up to limit records that arrived
// before the position the cursor names, newest arrival first. An empty cursor
// starts at the newest record. Positions are never reused, so repeated paging
// returns every record that stays kept exactly once, whatever their T0; records
// evicted between two calls are skipped. A cursor of another epoch yields an empty
// page (the page learns of the restart from the events route). ok is false for a
// malformed cursor.
func (s *Store) AppendRecords(dst []byte, cursor string, limit int) (out []byte, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maintain()

	upper := s.next
	stale := false
	if cursor != "" {
		n := len(s.epoch)
		pos, err := uint64(0), error(nil)
		i := strings.IndexByte(cursor, ':')
		if i < 0 {
			return dst, false
		}
		if pos, err = strconv.ParseUint(cursor[i+1:], 10, 64); err != nil {
			return dst, false
		}
		if cursor[:i] != s.epoch || i != n {
			stale = true
		}
		upper = min(pos, s.next)
	}

	var positions []uint64
	more := false
	if !stale {
		for pos := upper; pos > s.tail; pos-- {
			if len(positions) >= limit {
				more = true
				break
			}
			positions = append(positions, pos-1)
		}
	}
	dst = append(dst, '{')
	dst = appendRecords(dst, s, positions)
	dst = append(dst, ',')
	dst = s.appendDict(dst, s.dictFor(positions))
	dst = append(dst, `,"oldest":`...)
	dst = appendInt(dst, s.oldest())
	if more {
		dst = append(dst, `,"more":true`...)
		dst = appendStrField(dst, "next", s.posCursor(positions[len(positions)-1]), false)
	}
	return append(dst, '}'), true
}
