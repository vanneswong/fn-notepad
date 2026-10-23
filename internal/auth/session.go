package auth

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// Session lifetimes: "记住我" keeps the login for a month, a plain login dies
// with the browser and after half a day of inactivity anyway.
const (
	DefaultTTL  = 12 * time.Hour
	RememberTTL = 30 * 24 * time.Hour
)

type session struct {
	user     string
	expires  time.Time
	remember bool
}

// Sessions is an in-memory, cookie-backed login session table. Restarting the
// service invalidates every session, which is the safe default; clients that
// ticked "记住账号密码" simply log in again automatically.
type Sessions struct {
	rememberTTL time.Duration
	defaultTTL  time.Duration
	now         func() time.Time

	mu     sync.Mutex
	tokens map[string]session
}

// NewSessions returns an empty session table.
func NewSessions() *Sessions {
	return &Sessions{
		rememberTTL: RememberTTL,
		defaultTTL:  DefaultTTL,
		now:         time.Now,
		tokens:      make(map[string]session),
	}
}

// Start creates a session for username. The second return value is the expiry
// the client should store when "remember" was requested.
func (s *Sessions) Start(username string, remember bool) (token string, expires time.Time) {
	ttl := s.defaultTTL
	if remember {
		ttl = s.rememberTTL
	}
	token = newToken()
	expires = s.now().Add(ttl)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	s.tokens[token] = session{user: username, expires: expires, remember: remember}
	return token, expires
}

// Lookup returns the login name behind a token, if it is still valid.
func (s *Sessions) Lookup(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.tokens[token]
	if !ok {
		return "", false
	}
	if !s.now().Before(entry.expires) {
		delete(s.tokens, token)
		return "", false
	}
	// Sliding expiry: activity keeps a session alive.
	ttl := s.defaultTTL
	if entry.remember {
		ttl = s.rememberTTL
	}
	entry.expires = s.now().Add(ttl)
	s.tokens[token] = entry
	return entry.user, true
}

// Stop drops one session (logout).
func (s *Sessions) Stop(token string) {
	if token == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, token)
}

// StopUser drops every session of a user, optionally keeping one token alive
// (the caller's own session after a password change). It reports how many
// sessions were dropped.
func (s *Sessions) StopUser(username, keep string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	dropped := 0
	for token, entry := range s.tokens {
		if token == keep || entry.user != username {
			continue
		}
		delete(s.tokens, token)
		dropped++
	}
	return dropped
}

// Count reports how many sessions are currently held.
func (s *Sessions) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tokens)
}

func (s *Sessions) pruneLocked() {
	now := s.now()
	for token, entry := range s.tokens {
		if !now.Before(entry.expires) {
			delete(s.tokens, token)
		}
	}
}

func newToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failing is fatal for a login system; there is no sane
		// fallback, so surface it as an empty token which never authenticates.
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
