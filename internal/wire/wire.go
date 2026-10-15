// Package wire decodes the JSON the host sends to the plugin.
package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// Headers holds the few inbound client headers the plugin looks at.
type Headers struct {
	UA, Authorization, XAPIKey, GoogAPIKey, XForwardedFor, XRealIP, CFConnectingIP string
}

// Intercept is the part of a request interceptor call the plugin keeps.
// The request body is never decoded or copied.
type Intercept struct {
	RequestID, TraceID       string
	Model, RequestedModel    string
	Stream                   bool
	Headers                  Headers
	AuthIndex                string
	SessionID, ParentSession string
	HostCallbackID           string
}

type headerSet struct {
	UA            []string `json:"User-Agent"`
	Authorization []string `json:"Authorization"`
	XAPIKey       []string `json:"X-Api-Key"`
	GoogAPIKey    []string `json:"X-Goog-Api-Key"`
	XForwardedFor []string `json:"X-Forwarded-For"`
	XRealIP       []string `json:"X-Real-Ip"`
	CFIP          []string `json:"Cf-Connecting-Ip"`
}

func (h *headerSet) flat() Headers {
	return Headers{
		UA: first(h.UA), Authorization: first(h.Authorization), XAPIKey: first(h.XAPIKey),
		GoogAPIKey: first(h.GoogAPIKey), XForwardedFor: first(h.XForwardedFor),
		XRealIP: first(h.XRealIP), CFConnectingIP: first(h.CFIP),
	}
}

func first(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

type interceptHead struct {
	RequestID      string
	TraceID        string
	Model          string
	RequestedModel string
	Stream         bool
	Headers        headerSet
}

// lenString decodes a JSON string and ignores any other JSON type.
type lenString string

func (l *lenString) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if json.Unmarshal(b, &s) == nil {
			*l = lenString(s)
		}
	}
	return nil
}

type sessionMeta struct {
	SelectedAuthIndex lenString `json:"selected_auth_index"`
	CanonicalSession  lenString `json:"canonical_session_id"`
	ParentSession     lenString `json:"parent_session_id"`
}

type interceptTail struct {
	Metadata       sessionMeta `json:"Metadata"`
	HostCallbackID string      `json:"host_callback_id"`
}

type interceptFull struct {
	interceptHead
	interceptTail
}

const (
	bodyKey    = `"Body":`
	metaKey    = `","Metadata":`
	tailWindow = 64 << 10
)

var bufPool = sync.Pool{New: func() any { b := make([]byte, 0, 2048); return &b }}

// ParseIntercept decodes a request.intercept_before/after payload. The common
// case skips the (possibly multi-megabyte) Body field without touching it; any
// payload the fast path does not recognise goes through a full decode.
func ParseIntercept(data []byte) (Intercept, error) {
	if in, ok := parseFast(data); ok {
		return in, nil
	}
	var f interceptFull
	if err := json.Unmarshal(data, &f); err != nil {
		return Intercept{}, err
	}
	return f.result(), nil
}

func (f *interceptFull) result() Intercept {
	return Intercept{
		RequestID: f.RequestID, TraceID: f.TraceID, Model: f.Model, RequestedModel: f.RequestedModel,
		Stream: f.Stream, Headers: f.Headers.flat(),
		AuthIndex: string(f.Metadata.SelectedAuthIndex), SessionID: string(f.Metadata.CanonicalSession),
		ParentSession: string(f.Metadata.ParentSession), HostCallbackID: f.HostCallbackID,
	}
}

func parseFast(data []byte) (Intercept, bool) {
	pb := bufPool.Get().(*[]byte)
	defer bufPool.Put(pb)

	from := 0
	for try := 0; try < 4; try++ {
		j := bytes.Index(data[from:], []byte(bodyKey))
		if j < 0 {
			return Intercept{}, false
		}
		j += from
		from = j + 1
		if j == 0 || data[j-1] != ',' {
			continue
		}
		open := j + len(bodyKey)
		if open >= len(data) {
			return Intercept{}, false
		}
		var suffix int
		switch {
		case data[open] == '"':
			win := max(open, len(data)-tailWindow)
			k := bytes.LastIndex(data[win:], []byte(metaKey))
			if k < 0 {
				return Intercept{}, false
			}
			if win+k <= open {
				continue
			}
			suffix = win + k + 1
		case bytes.HasPrefix(data[open:], []byte(`null,`)):
			suffix = open + 4
		default:
			return Intercept{}, false
		}

		buf := append((*pb)[:0], data[:j-1]...)
		buf = append(buf, '}')
		var head interceptHead
		err := json.Unmarshal(buf, &head)
		if err != nil {
			*pb = buf
			continue
		}
		buf = append(buf[:0], '{')
		buf = append(buf, data[suffix+1:]...)
		*pb = buf
		var tail interceptTail
		if json.Unmarshal(buf, &tail) != nil {
			return Intercept{}, false
		}
		f := interceptFull{head, tail}
		return f.result(), true
	}
	return Intercept{}, false
}

// Usage mirrors the host's UsageRecord fields the plugin reads. Field names match
// the host's untagged JSON; BaseURL and the other fields are left out on purpose.
type Usage struct {
	RequestID       string
	TraceID         string
	Provider        string
	Model           string
	Alias           string
	APIKey          string
	IsNativeKey     bool
	AccessProvider  string
	SessionID       string
	ParentSessionID string
	AuthIndex       string
	Stream          bool
	RequestedAt     time.Time
	Latency         time.Duration
	TTFT            time.Duration
	Failed          bool
	Failure         struct {
		StatusCode int
		Body       string
	}
	Detail struct {
		InputTokens         int64
		OutputTokens        int64
		ReasoningTokens     int64
		CachedTokens        int64
		CacheReadTokens     int64
		CacheCreationTokens int64
		TotalTokens         int64
	}
	ResponseHeaders struct {
		RetryAfter []string `json:"Retry-After"`
	}
}

// ParseUsage decodes a usage.handle payload.
func ParseUsage(data []byte) (*Usage, error) {
	u := new(Usage)
	if err := json.Unmarshal(data, u); err != nil {
		return nil, err
	}
	return u, nil
}

// Completion mirrors the host's RequestCompletion.
type Completion struct {
	RequestID      string
	TraceID        string
	Model          string
	RequestedModel string
	Stream         bool
	Outcome        string
	StatusCode     int
	Error          string
	StartedAt      time.Time
	CompletedAt    time.Time
	Metadata       sessionMeta
}

// AuthIndex, SessionID and ParentSession expose the completion metadata.
func (c *Completion) AuthIndex() string     { return string(c.Metadata.SelectedAuthIndex) }
func (c *Completion) SessionID() string     { return string(c.Metadata.CanonicalSession) }
func (c *Completion) ParentSession() string { return string(c.Metadata.ParentSession) }

// ParseCompletion decodes a request.complete payload.
func ParseCompletion(data []byte) (*Completion, error) {
	c := new(Completion)
	if err := json.Unmarshal(data, c); err != nil {
		return nil, err
	}
	return c, nil
}

// Lifecycle is the payload of plugin.register and plugin.reconfigure.
type Lifecycle struct {
	ConfigYAML    []byte `json:"config_yaml"`
	SchemaVersion uint32 `json:"schema_version"`
}

// ParseLifecycle decodes a plugin.register or plugin.reconfigure payload.
func ParseLifecycle(data []byte) (Lifecycle, error) {
	var l Lifecycle
	if len(bytes.TrimSpace(data)) == 0 {
		return l, nil
	}
	err := json.Unmarshal(data, &l)
	return l, err
}

// Management is a management.handle payload.
type Management struct {
	Method         string
	Path           string
	Headers        map[string][]string
	Query          map[string][]string
	HostCallbackID string `json:"host_callback_id"`
}

// ErrEmpty is returned for an empty management payload.
var ErrEmpty = errors.New("empty payload")

// ParseManagement decodes a management.handle payload; the request body is ignored.
func ParseManagement(data []byte) (Management, error) {
	var m Management
	if len(data) == 0 {
		return m, ErrEmpty
	}
	err := json.Unmarshal(data, &m)
	return m, err
}
