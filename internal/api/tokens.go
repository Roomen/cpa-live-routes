package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"sync"
	"time"
)

const (
	tokenTTL  = 12 * time.Hour
	maxTokens = 32
)

type token struct {
	sum [32]byte
	exp time.Time
}

// TokenStore issues and checks view tokens. Only hashes are kept, in memory, in
// least-recently-used order.
type TokenStore struct {
	mu    sync.Mutex
	now   func() time.Time
	items []token
}

// NewTokenStore returns an empty token store. now may be nil for the wall clock.
func NewTokenStore(now func() time.Time) *TokenStore {
	if now == nil {
		now = time.Now
	}
	return &TokenStore{now: now}
}

// Issue creates a token valid for 12 hours.
func (t *TokenStore) Issue() (string, time.Time) {
	var b [32]byte
	_, _ = rand.Read(b[:])
	raw := base64.RawURLEncoding.EncodeToString(b[:])
	now := t.now()
	exp := now.Add(tokenTTL)

	t.mu.Lock()
	defer t.mu.Unlock()
	t.purge(now)
	if len(t.items) >= maxTokens {
		t.items = append(t.items[:0], t.items[1:]...)
	}
	t.items = append(t.items, token{sum: sha256.Sum256([]byte(raw)), exp: exp})
	return raw, exp
}

// Valid reports whether raw is a live token and marks it as recently used.
func (t *TokenStore) Valid(raw string) bool {
	if raw == "" {
		return false
	}
	sum := sha256.Sum256([]byte(raw))
	t.mu.Lock()
	defer t.mu.Unlock()
	t.purge(t.now())
	found := -1
	for i := range t.items {
		if subtle.ConstantTimeCompare(sum[:], t.items[i].sum[:]) == 1 {
			found = i
		}
	}
	if found < 0 {
		return false
	}
	tok := t.items[found]
	t.items = append(append(t.items[:found], t.items[found+1:]...), tok)
	return true
}

func (t *TokenStore) purge(now time.Time) {
	kept := t.items[:0]
	for _, it := range t.items {
		if now.Before(it.exp) {
			kept = append(kept, it)
		}
	}
	t.items = kept
}
