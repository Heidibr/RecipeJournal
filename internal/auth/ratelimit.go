package auth

import (
	"sync"
	"time"
)

// limiter is a simple in-memory sliding-window throttle. Each use picks its
// own key: login uses "<ip>|<lowercased-email>" so an attacker can't lock
// other people out from a single IP by spamming their email — they'd also
// need to share IP with the victim. Persistence across restarts is
// intentionally skipped: it's a single-binary app, restarts are rare, and an
// attacker who can trigger a restart has bigger problems.
type limiter struct {
	mu          sync.Mutex
	hits        map[string][]time.Time
	window      time.Duration
	maxInWindow int
}

// sweepAbove bounds memory: keys are only pruned when they are touched, so a
// flood of one-off keys (a new IP per request) would otherwise grow the map
// forever. Past this size every record sweeps the whole map once.
const sweepAbove = 10000

func newLimiter(window time.Duration, maxInWindow int) *limiter {
	return &limiter{
		hits:        make(map[string][]time.Time),
		window:      window,
		maxInWindow: maxInWindow,
	}
}

// Allow returns false if this key has had too many recent failures. It does
// NOT record an attempt — call RecordFailure on a failed login afterward.
// Splitting check from record lets a successful login skip recording entirely.
func (l *limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(key, time.Now())
	return len(l.hits[key]) < l.maxInWindow
}

// RecordFailure adds a failed-attempt timestamp for the key.
func (l *limiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.record(key, time.Now())
}

// Take checks and records in one step, for actions where every attempt
// counts (signup, password-reset emails), not just failed ones. Doing both
// under one lock means two concurrent requests can't both slip into the last
// free slot.
func (l *limiter) Take(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.prune(key, now)
	if len(l.hits[key]) >= l.maxInWindow {
		return false
	}
	l.record(key, now)
	return true
}

// Reset clears the failure history for a key — call after a successful login
// so a user who eventually types their password right isn't still throttled.
func (l *limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}

// record appends a timestamp. Caller holds the lock.
func (l *limiter) record(key string, now time.Time) {
	l.prune(key, now)
	l.hits[key] = append(l.hits[key], now)
	if len(l.hits) > sweepAbove {
		for k := range l.hits {
			l.prune(k, now)
		}
	}
}

// prune drops timestamps outside the sliding window. Caller holds the lock.
func (l *limiter) prune(key string, now time.Time) {
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
