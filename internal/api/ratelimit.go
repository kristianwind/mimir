package api

// Counting attempts per address.
//
// Two endpoints need it and they need different numbers, so the window and
// the burst belong to the instance rather than to the package.
//
// In memory on purpose. It protects against a script hammering one instance,
// which is a thing that happens within one process's lifetime; persisting it
// would buy resistance to a restart nobody is going to time.

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// rateLimiter counts recent attempts per key and reports when there are too
// many in a window.
type rateLimiter struct {
	window time.Duration
	burst  int

	mu   sync.Mutex
	seen map[string][]time.Time
}

func newRateLimiter(window time.Duration, burst int) *rateLimiter {
	return &rateLimiter{window: window, burst: burst, seen: map[string][]time.Time{}}
}

// allow records an attempt and reports whether it is within the limit.
func (l *rateLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := now.Add(-l.window)
	kept := l.seen[key][:0]
	for _, t := range l.seen[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	// Keys that have gone quiet are dropped entirely, so this map does not
	// grow for the lifetime of the process.
	if len(kept) == 0 {
		delete(l.seen, key)
	} else {
		l.seen[key] = kept
	}
	if len(kept) >= l.burst {
		return false
	}
	l.seen[key] = append(kept, now)
	return true
}

// over reports whether a key is already at its limit, without recording an
// attempt.
//
// The login limiter counts failures rather than requests: somebody signing in
// correctly ten times in a morning is not an attack, and charging them for it
// would lock a working password out of a shared office. So the check and the
// count happen at different moments — this asks, and record() answers.
func (l *rateLimiter) over(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := now.Add(-l.window)
	n := 0
	for _, t := range l.seen[key] {
		if t.After(cutoff) {
			n++
		}
	}
	return n >= l.burst
}

// record adds an attempt without asking whether it was allowed.
func (l *rateLimiter) record(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := now.Add(-l.window)
	kept := l.seen[key][:0]
	for _, t := range l.seen[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.seen[key] = append(kept, now)
}

func clientAddr(r *http.Request) string {
	// RealIP middleware has already resolved the forwarded headers.
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
