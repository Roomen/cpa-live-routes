// Package api defines the JSON contract between the plugin and its embedded page.
//
// Every type here is a wire type: field names and json tags are frozen. The page
// (web/index.html) and the sample payloads in testdata/contract/ follow them exactly.
package api

// ContractVersion is reported as Snapshot.V. Bump it on any breaking change.
// Version 2: the evict_before event became "evict" with exact record IDs, and
// history paging moved from a T0 bound to an opaque cursor.
const ContractVersion = 2

// Routes. Resource paths are relative to /v0/resource/plugins/<PluginID>/ and are
// not authenticated by CPA; the plugin gates every data route itself:
//   - require_key=false (default): served only when the request Host is loopback
//     (127.0.0.1, localhost, [::1], any port); otherwise 403 Error{Code:"forbidden_host"}.
//   - require_key=true: TokenHeader must carry a token from ManagementSessionPath;
//     otherwise 401 Error{Code:"unauthorized"}, and the page asks for the management key.
//
// Management paths are relative to /v0/management/ and require the management key.
const (
	PluginID = "live-routes"

	ResourcePagePath     = "/live"         // GET, HTML page; ?demo=1 runs offline with simulated data
	ResourceSnapshotPath = "/api/snapshot" // GET, gated
	ResourceEventsPath   = "/api/events"   // GET ?since=<cursor>, gated
	ResourceRecordsPath  = "/api/records"  // GET ?before=<cursor>&limit=<n<=2000>, gated; no before = from the newest record

	ManagementSessionPath  = "/live-routes/session"  // POST, exchanges the management key for a view token
	ManagementSnapshotPath = "/live-routes/snapshot" // GET, same body as ResourceSnapshotPath, for scripts

	// TokenHeader carries the view token on gated resource routes. Tokens are never
	// accepted from cookies, query strings or the URL.
	TokenHeader = "X-Live-Routes-Token"
)

// Enumerations used by the page.
const (
	StateQueued   = "queued"   // request.intercept_before seen, no credential yet; dot waits at the CPA hub
	StateRouted   = "routed"   // request.intercept_after seen, credential known; dot waits at the exit node
	StateDone     = "done"     // completed successfully
	StateFailed   = "failed"   // upstream or executor failure
	StateCanceled = "canceled" // client disconnected or context canceled
	StateRejected = "rejected" // another interceptor terminated the request
	StateLost     = "lost"     // in flight longer than the lost timeout without a terminal event

	EventUpsert = "upsert" // Rec is the full current record; replace by Rec.ID
	EventEvict  = "evict"  // drop exactly the records named in IDs (one batch per eviction run)
	EventConfig = "config" // plugin was reconfigured; Config is the new value

	ClientClaudeCode  = "claude-code"
	ClientCodex       = "codex"
	ClientOpenCode    = "opencode"
	ClientGeminiCLI   = "gemini-cli"
	ClientAntigravity = "antigravity"
	ClientSDK         = "sdk"
	ClientOther       = "other"

	LayoutTabs  = "tabs"  // flow and timeline as switchable views (prototype/combined.html)
	LayoutSplit = "split" // flow strip above exit lanes, both visible (prototype/lanes.html)
)

// Config is the page-relevant part of the plugin configuration.
type Config struct {
	ActiveWindowS    int    `json:"active_window_s"`   // flow view: nodes without requests in this window fade out
	RetentionS       int    `json:"retention_s"`       // how long a record stays after it was added to the plugin (arrival time, not T0)
	MaxRecords       int    `json:"max_records"`       // record cap, oldest evicted first
	AnimationSpeed   string `json:"animation_speed"`   // "0.5x" | "1x" | "2x"; default for the page
	ReducedAnimation bool   `json:"reduced_animation"` // no flying dots, only link widths and counters
	InflightTracking bool   `json:"inflight_tracking"` // false: usage replay only, no queued/routed states
	Layout           string `json:"layout"`            // LayoutTabs | LayoutSplit; default for the page
	RequireKey       bool   `json:"require_key"`       // data routes need a view token instead of a loopback Host
}

// Dictionary entries. IDs start at 1; a reference of 0 (field absent) means unknown.
// Entries are upserts by ID: a later Dict may resend an ID with updated fields
// (for example an exit label resolved after the first request).

// Client is one inbound endpoint: UA family plus API key plus client IP.
type Client struct {
	ID    int    `json:"id"`
	Kind  string `json:"kind"`         // Client* constant
	Label string `json:"label"`        // e.g. "Claude Code 2.0.14"
	UA    string `json:"ua,omitempty"` // raw User-Agent, truncated to 160 chars
	IP    string `json:"ip,omitempty"` // only from X-Forwarded-For / X-Real-IP / CF-Connecting-IP
	Key   int    `json:"key,omitempty"`
}

// Key is a redacted client API key. The raw key never leaves the plugin.
type Key struct {
	ID    int    `json:"id"`
	Label string `json:"label,omitempty"` // from config api_key_labels
	Mask  string `json:"mask"`            // "sk-…3f9a", or "key#<fp>" for short keys and non-key principals
	FP    string `json:"fp"`              // first 8 hex chars of sha256(raw key)
}

// Session is a client session; Parent links a sub-agent or fork to its parent session.
type Session struct {
	ID     int    `json:"id"`
	SID    string `json:"sid"`   // full canonical session ID, e.g. "claude:7b1e…:agent:explore"
	Short  string `json:"short"` // 8 chars for display
	Parent int    `json:"parent,omitempty"`
	Client int    `json:"client,omitempty"`
}

// Rule is a CPA routing rule as observed: requested alias to upstream model.
type Rule struct {
	ID    int    `json:"id"`
	Alias string `json:"alias"`
	Model string `json:"model"` // equals Alias when no rewrite happened
}

// Exit is an outbound credential.
type Exit struct {
	ID        int    `json:"id"`
	AuthIndex string `json:"auth_index"`
	Provider  string `json:"provider,omitempty"`
	Label     string `json:"label,omitempty"` // credential label; e-mail addresses are masked "a***@gmail.com"
}

// Dict groups dictionary entries. Empty groups are omitted.
type Dict struct {
	Clients  []Client  `json:"clients,omitempty"`
	Keys     []Key     `json:"keys,omitempty"`
	Sessions []Session `json:"sessions,omitempty"`
	Rules    []Rule    `json:"rules,omitempty"`
	Exits    []Exit    `json:"exits,omitempty"`
}

// Tokens are usage counters; Total is the host-normalized total token count.
type Tokens struct {
	In     int64 `json:"in"`
	Out    int64 `json:"out"`
	Reason int64 `json:"reason,omitempty"`
	Cache  int64 `json:"cache,omitempty"`
	Total  int64 `json:"total"`
}

// Record is one client request. Times are Unix milliseconds.
type Record struct {
	ID          string  `json:"id"`
	Trace       string  `json:"trace,omitempty"`
	T0          int64   `json:"t0"`           // start
	T1          int64   `json:"t1,omitempty"` // end; absent while queued or routed
	State       string  `json:"state"`        // State* constant
	Client      int     `json:"client,omitempty"`
	Session     int     `json:"session,omitempty"`
	Rule        int     `json:"rule,omitempty"`
	Exit        int     `json:"exit,omitempty"`
	Stream      bool    `json:"stream,omitempty"`
	Status      int     `json:"status,omitempty"`      // HTTP status when known
	TTFT        int64   `json:"ttft,omitempty"`        // ms
	Lat         int64   `json:"lat,omitempty"`         // ms
	Tok         *Tokens `json:"tok,omitempty"`         // absent until usage arrives
	Attempts    int     `json:"attempts,omitempty"`    // credential attempts, >1 means retries
	Err         string  `json:"err,omitempty"`         // sanitized, at most 200 characters
	RetryAfterS int     `json:"retry_after,omitempty"` // from Retry-After on 429
}

// Stats are plugin-side counters for diagnostics.
type Stats struct {
	Records         int   `json:"records"`
	Inflight        int   `json:"inflight"`
	Evicted         int64 `json:"evicted"`
	InflightDropped int64 `json:"inflight_dropped"` // in-flight table full, request tracked by usage only
}

// Snapshot is the full state. Records holds every in-flight record plus the newest
// completed ones (at most 5000); Truncated reports that older records exist and can be
// fetched through ResourceRecordsPath.
type Snapshot struct {
	V         int      `json:"v"`
	Epoch     string   `json:"epoch"`          // random per plugin process
	Cursor    string   `json:"cursor"`         // "<epoch>:<seq>", opaque to the page
	Now       int64    `json:"now"`            // server clock, for skew correction
	Oldest    int64    `json:"oldest"`         // smallest T0 among the records kept, 0 when empty (records can arrive out of T0 order)
	Next      string   `json:"next,omitempty"` // with Truncated: the ResourceRecordsPath "before" cursor that continues past the loaded records
	Config    Config   `json:"config"`
	Dict      Dict     `json:"dict"`
	Records   []Record `json:"records"`
	Truncated bool     `json:"truncated,omitempty"`
	Stats     Stats    `json:"stats"`
}

// Event is one change since a cursor.
type Event struct {
	Seq    uint64   `json:"seq"`
	Type   string   `json:"type"` // Event* constant
	Rec    *Record  `json:"rec,omitempty"`
	IDs    []string `json:"ids,omitempty"` // EventEvict: IDs of the evicted records; there is no time bound, T0 order is not arrival order
	Config *Config  `json:"config,omitempty"`
}

// Events answers ResourceEventsPath. Reset means the cursor is unusable (other epoch,
// older than the retained change log, or ahead of the server) and the page must fetch
// a new Snapshot. More means the response was capped at 2000 events; poll again at once.
type Events struct {
	Cursor string  `json:"cursor"`
	Now    int64   `json:"now"`
	Reset  bool    `json:"reset,omitempty"`
	More   bool    `json:"more,omitempty"`
	Dict   Dict    `json:"dict"`
	Events []Event `json:"events"`
}

// RecordsPage answers ResourceRecordsPath: up to limit records that arrived before
// the position "before" names, newest arrival first (not T0 order). Following Next
// returns every kept record exactly once, also with equal or out-of-order T0;
// records evicted meanwhile are skipped. A cursor from another epoch gives an
// empty page.
type RecordsPage struct {
	Records []Record `json:"records"`
	Dict    Dict     `json:"dict"`
	Oldest  int64    `json:"oldest"`         // as Snapshot.Oldest
	More    bool     `json:"more,omitempty"` // older records remain
	Next    string   `json:"next,omitempty"` // present exactly when More: pass it as "before"
}

// SessionToken answers ManagementSessionPath.
type SessionToken struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"` // Unix ms
	TTLS      int    `json:"ttl_s"`
}

// Error is the body of every non-2xx response. Gated routes answer 401 "unauthorized"
// (require_key=true and the token is missing or invalid; the page asks for the
// management key, keeps the token in memory only) or 403 "forbidden_host"
// (require_key=false and the Host is not loopback; the page explains how to enable
// require_key).
type Error struct {
	Code    string `json:"code"` // "unauthorized" | "forbidden_host" | "bad_request" | "not_found"
	Message string `json:"message"`
}
