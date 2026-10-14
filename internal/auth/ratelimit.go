package auth

import (
	"sync"
	"time"
)

// loginLimiter is a simple in-memory sliding-window throttle for failed login
// attempts. Keyed by an opaque identifier (we use "<ip>|<lowercased-email>")
// so an attacker can't lock other people out from a single IP by spamming
// their email — they'd also need to share IP with the victim. Persistence
// across restarts is intentionally skipped: it's a single-binary app, restarts
// are rare, and an attacker who can trigger a restart has bigger problems.
type loginLimiter struct {
	mu         sync.Mutex
	hits       map[string][]time.Time
	window     time.Duration
	maxInWindow int
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		hits:        make(map[string][]time.Time),
		window:      15 * time.Minute,
		maxInWindow: 5,
	}
}

// Allow returns false if this key has had too many recent failures. It does
// NOT record an attempt — call RecordFailure on a failed login afterward.
// Splitting check from record lets a successful login skip recording entirely.
func (l *loginLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(key, time.Now())
	return len(l.hits[key]) < l.maxInWindow
}

// RecordFailure adds a failed-attempt timestamp for the key.
func (l *loginLimiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.prune(key, now)
	l.hits[key] = append(l.hits[key], now)
}

// Reset clears the failure history for a key — call after a successful login
// so a user who eventually types their password right isn't still throttled.
func (l *loginLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}

// prune drops timestamps outside the sliding window. Caller holds the lock.
func (l *loginLimiter) prune(key string, now time.Time) {
	cutoff := now.Add(-l.window)
	ts := l.hits[key]
	kept := ts[:0]
	for _, t := range ts {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.hits, key)
		return
	}
	l.hits[key] = kept
}
