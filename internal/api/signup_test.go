package api

import (
	"testing"
	"time"
)

// Every account gets fourteen free days, so an unmetered signup form is a
// machine for issuing free trials to whoever asks fastest.
func TestSignupsAreMeteredPerAddress(t *testing.T) {
	l := newSignupLimiter()
	now := time.Now()

	for i := 0; i < signupBurst; i++ {
		if !l.allow("1.2.3.4", now) {
			t.Fatalf("attempt %d was refused inside the burst", i+1)
		}
	}
	if l.allow("1.2.3.4", now) {
		t.Error("the burst was exceeded and still allowed")
	}
	// Somebody else is unaffected, which is the point of counting per
	// address rather than globally.
	if !l.allow("5.6.7.8", now) {
		t.Error("one address's limit blocked another")
	}
}

// The window rolls. Somebody rate limited an hour ago is not rate limited
// forever — it is there to stop a script, not to punish a household.
func TestTheLimitExpires(t *testing.T) {
	l := newSignupLimiter()
	now := time.Now()
	for i := 0; i < signupBurst; i++ {
		l.allow("1.2.3.4", now)
	}
	if l.allow("1.2.3.4", now) {
		t.Fatal("precondition: should be limited")
	}
	if !l.allow("1.2.3.4", now.Add(signupWindow+time.Second)) {
		t.Error("still refused after the window had passed")
	}
}

// An address that has gone quiet must not be remembered for the life of the
// process, or this map is a slow leak on a public endpoint.
func TestQuietAddressesAreForgotten(t *testing.T) {
	l := newSignupLimiter()
	now := time.Now()
	l.allow("1.2.3.4", now)
	if len(l.seen) != 1 {
		t.Fatalf("expected one address tracked, got %d", len(l.seen))
	}
	// A later attempt from somebody else sweeps nothing on its own, but the
	// original address's own next call must drop its stale entries.
	l.allow("1.2.3.4", now.Add(2*signupWindow))
	if got := len(l.seen["1.2.3.4"]); got != 1 {
		t.Errorf("stale attempts were kept: %d entries", got)
	}
}
