// Package redact turns raw request material into values that are safe to keep and show.
package redact

import (
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Client kinds, matching the page contract.
const (
	KindClaudeCode  = "claude-code"
	KindCodex       = "codex"
	KindOpenCode    = "opencode"
	KindGeminiCLI   = "gemini-cli"
	KindAntigravity = "antigravity"
	KindSDK         = "sdk"
	KindOther       = "other"
)

const (
	maxUA        = 160
	maxErr       = 200
	maxErrInput  = 2048
	maxPrincipal = 32
)

// Key is a redacted API key. Sum is kept so labels can be re-resolved after a
// reconfigure without the raw key.
type Key struct {
	FP   string
	Mask string
	Sum  [32]byte
}

// NewKey redacts a raw client API key.
func NewKey(raw string) Key {
	sum := sha256.Sum256([]byte(raw))
	fp := hex.EncodeToString(sum[:4])
	mask := "key#" + fp
	if len(raw) >= 12 && printableASCII(raw) {
		mask = raw[:3] + "…" + raw[len(raw)-4:]
	}
	return Key{FP: fp, Mask: mask, Sum: sum}
}

// Principal redacts the identity of a non-key access provider.
func Principal(provider, principal string) Key {
	name := principal
	if provider != "" {
		name = provider + ":" + principal
	}
	sum := sha256.Sum256([]byte(name))
	return Key{FP: hex.EncodeToString(sum[:4]), Mask: truncRunes(name, maxPrincipal), Sum: sum}
}

// KeyFromHeaders extracts the raw API key from the usual client headers.
func KeyFromHeaders(authorization, xAPIKey, googAPIKey string) string {
	if len(authorization) > 7 && strings.EqualFold(authorization[:7], "bearer ") {
		if k := strings.TrimSpace(authorization[7:]); k != "" {
			return k
		}
	}
	if xAPIKey != "" {
		return strings.TrimSpace(xAPIKey)
	}
	return strings.TrimSpace(googAPIKey)
}

// LabelFor resolves a configured label by sha256 hex prefix.
func LabelFor(sum [32]byte, labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	full := hex.EncodeToString(sum[:])
	for prefix, label := range labels {
		if len(prefix) >= 8 && strings.HasPrefix(full, strings.ToLower(prefix)) {
			return label
		}
	}
	return ""
}

var secretRe = regexp.MustCompile(`(sk-|sk_|Bearer\s+|AIza)[A-Za-z0-9._\-]{6,}`)

const (
	minSecret  = 6 // shortest exact secret or sum-matched token that is masked
	maxToken   = 256
	genericMin = 20 // shortest token-like run the generic rule considers
)

func tokenByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.'
}

// keyByte is the wider alphabet used to cut candidate tokens for the sum match.
func keyByte(c byte) bool {
	return tokenByte(c) || c == '~' || c == '+' || c == '/' || c == '=' || c == ':'
}

// mapRuns replaces every maximal run of bytes accepted by in with f(run) when f
// reports a replacement.
func mapRuns(s string, in func(byte) bool, f func(string) (string, bool)) string {
	var b strings.Builder
	last := 0
	for i := 0; i < len(s); {
		if !in(s[i]) {
			i++
			continue
		}
		j := i
		for j < len(s) && in(s[j]) {
			j++
		}
		if rep, ok := f(s[i:j]); ok {
			b.WriteString(s[last:i])
			b.WriteString(rep)
			last = j
		}
		i = j
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

var (
	// lower-case words and short numbers joined by - _ ., e.g. "claude-3-5-sonnet-20241022", "gpt-4o-mini"
	plainJoinedRe = regexp.MustCompile(`^(?:[a-z]+|[0-9]{1,8}[a-z]{0,2})(?:[-_.](?:[a-z]+|[0-9]{1,8}[a-z]{0,2}))*$`)
	// CamelCase identifiers, e.g. "ResourceExhaustedException"
	plainCamelRe = regexp.MustCompile(`^(?:[A-Z][a-z]{2,})+$`)
)

func matchesSum(tok string, sums [][32]byte) bool {
	if len(tok) < minSecret || len(tok) > maxToken {
		return false
	}
	h := sha256.Sum256([]byte(tok))
	for i := range sums {
		if h == sums[i] {
			return true
		}
	}
	return false
}

const fieldPunct = "\"'`,;:()[]{}<>."

// maskSums masks the tokens whose sha256 is in sums: each space-separated field
// (with or without the punctuation around it) and each run of key characters.
func maskSums(s string, sums [][32]byte) string {
	fields := strings.Split(s, " ")
	for i, f := range fields {
		for lead := 0; lead <= len(f) && lead < 4; lead++ {
			if lead > 0 && !strings.ContainsRune(fieldPunct, rune(f[lead-1])) {
				break
			}
			hit := false
			for end := len(f); end >= lead && len(f)-end < 4; end-- {
				if end < len(f) && !strings.ContainsRune(fieldPunct, rune(f[end])) {
					break
				}
				if matchesSum(f[lead:end], sums) {
					fields[i] = f[:lead] + "…" + f[end:]
					hit = true
					break
				}
			}
			if hit {
				break
			}
		}
	}
	s = strings.Join(fields, " ")
	return mapRuns(s, keyByte, func(run string) (string, bool) {
		if matchesSum(run, sums) {
			return "…", true
		}
		return "", false
	})
}

// plainWord reports whether a long run is readable text rather than a secret.
func plainWord(run string) bool {
	return plainJoinedRe.MatchString(run) || plainCamelRe.MatchString(run)
}

// ErrorText sanitizes an upstream error body for display. It removes, in order:
// every exact occurrence of the given raw secrets; any token whose sha256 equals
// one of sums (for callers that must not hold the raw key, only its digest);
// well-known key shapes (sk-…, Bearer …, AIza…); and finally any token-like run
// of at least 20 characters of [A-Za-z0-9_.-] that is not a plain word, because
// a key format unknown to the regexes can still be echoed by an upstream.
// A key containing spaces or characters outside the token alphabet is only caught
// by the exact-secret rule.
func ErrorText(s string, secrets ...string) string {
	return ErrorTextSums(s, nil, secrets...)
}

// ErrorTextSums is ErrorText plus the sha256 sums of raw keys to mask.
func ErrorTextSums(s string, sums [][32]byte, secrets ...string) string {
	if len(s) > maxErrInput {
		s = strings.ToValidUTF8(s[:maxErrInput], "")
	}
	s = strings.Join(strings.Fields(s), " ")
	for _, sec := range secrets {
		if len(sec) >= minSecret {
			s = strings.ReplaceAll(s, sec, "…")
		}
	}
	if len(sums) > 0 {
		s = maskSums(s, sums)
	}
	if strings.Contains(s, "sk") || strings.Contains(s, "Bearer") || strings.Contains(s, "AIza") {
		s = secretRe.ReplaceAllString(s, "${1}…")
	}
	s = mapRuns(s, tokenByte, func(run string) (string, bool) {
		if len(run) < genericMin || plainWord(run) {
			return "", false
		}
		return "…", true
	})
	return truncRunes(s, maxErr)
}

// UA is a classified User-Agent.
type UA struct {
	Kind  string
	Label string
	Raw   string
}

// ParseUA truncates and classifies a User-Agent header.
func ParseUA(ua string) UA {
	ua = truncRunes(strings.TrimSpace(ua), maxUA)
	if ua == "" {
		return UA{}
	}
	tok := ua
	if i := strings.IndexAny(tok, " \t"); i >= 0 {
		tok = tok[:i]
	}
	name, ver, _ := strings.Cut(tok, "/")
	if i := strings.IndexByte(ver, '/'); i >= 0 {
		ver = ver[:i]
	}
	ver = truncRunes(ver, 24)
	lname := strings.ToLower(name)
	withVer := func(base string) string {
		if ver == "" {
			return base
		}
		return base + " " + ver
	}
	switch {
	case strings.Contains(lname, "claude-cli"), strings.Contains(lname, "claude-code"):
		return UA{KindClaudeCode, withVer("Claude Code"), ua}
	case strings.HasPrefix(lname, "codex"):
		return UA{KindCodex, withVer("Codex"), ua}
	case strings.HasPrefix(lname, "opencode"):
		return UA{KindOpenCode, withVer("OpenCode"), ua}
	case lname == "geminicli", strings.HasPrefix(lname, "gemini-cli"):
		return UA{KindGeminiCLI, withVer("Gemini CLI"), ua}
	case strings.HasPrefix(lname, "antigravity"), lname == "agy":
		return UA{KindAntigravity, withVer("Antigravity"), ua}
	}
	label := truncRunes(tok, 40)
	for _, p := range []string{"openai", "anthropic", "python", "node", "axios", "undici"} {
		if strings.HasPrefix(lname, p) {
			return UA{KindSDK, label, ua}
		}
	}
	return UA{KindOther, label, ua}
}

// ClientFromSession guesses the client family from a canonical session id.
func ClientFromSession(sid string) (kind, label string) {
	prefix, _, ok := strings.Cut(sid, ":")
	if !ok {
		return "", ""
	}
	switch strings.ToLower(prefix) {
	case "claude":
		return KindClaudeCode, "Claude Code"
	case "codex":
		return KindCodex, "Codex"
	case "opencode":
		return KindOpenCode, "OpenCode"
	case "gemini":
		return KindGeminiCLI, "Gemini CLI"
	case "agy", "antigravity":
		return KindAntigravity, "Antigravity"
	}
	return "", ""
}

// ClientIP picks the client address from proxy headers, in priority order.
func ClientIP(xForwardedFor, xRealIP, cfConnectingIP string) string {
	first, _, _ := strings.Cut(xForwardedFor, ",")
	for _, c := range []string{first, xRealIP, cfConnectingIP} {
		if ip := parseIP(strings.TrimSpace(c)); ip != "" {
			return ip
		}
	}
	return ""
}

func parseIP(s string) string {
	if s == "" {
		return ""
	}
	if a, err := netip.ParseAddr(s); err == nil {
		return a.String()
	}
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().String()
	}
	return ""
}

// SessionShort is the 8-character display id of a session.
func SessionShort(sid string) string {
	rest := sid
	if i := strings.Index(sid, ":agent:"); i >= 0 {
		rest = sid[i+len(":agent:"):]
	} else if i := strings.IndexByte(sid, ':'); i >= 0 {
		rest = sid[i+1:]
	}
	return truncRunes(rest, 8)
}

// MaskEmail renders "a***@domain"; it returns "" when s is not an address.
func MaskEmail(s string) string {
	local, domain, ok := strings.Cut(strings.TrimSpace(s), "@")
	if !ok || local == "" || domain == "" {
		return ""
	}
	r, _ := utf8.DecodeRuneInString(local)
	return string(r) + "***@" + domain
}

func printableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] <= 0x20 || s[i] >= 0x7f {
			return false
		}
	}
	return true
}

func truncRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := 0
	for j := range s {
		if i == n {
			return s[:j]
		}
		i++
	}
	return s
}
