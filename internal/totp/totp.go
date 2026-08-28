// Package totp implements the time-based one-time passwords of RFC 6238.
//
// Written out rather than imported. The algorithm is HMAC-SHA1 over a counter
// with a truncation rule, all of it in the standard library, and the whole of
// it fits on a page — a dependency here would be more code to audit than the
// thing it replaces, in a place where a subtle bug is a silent hole in
// somebody's second factor.
//
// Three properties this file is responsible for, each of which has been a real
// vulnerability in somebody else's implementation:
//
// Comparison is constant time. Comparing generated codes with == leaks how
// many leading digits were right, which turns a million-guess space into a
// few dozen.
//
// The acceptance window is small and asymmetric-free. One step either side of
// now, no more: every extra step multiplies the codes that are valid at any
// moment, and a window measured in minutes is a window an attacker can walk
// into.
//
// A code that has been used cannot be used again. Within its thirty seconds a
// code is otherwise replayable by anyone who saw it — over someone's
// shoulder, in a screenshot, in a log.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	// Period is the step, in seconds. Thirty is what every authenticator app
	// assumes; it is not configurable because a different value would simply
	// not work in the apps people have.
	Period = 30
	// Digits in a code.
	Digits = 6
	// Skew is how many steps either side of now are accepted. One step is
	// thirty seconds of tolerance in each direction, which covers an
	// unsynchronised phone and the time it takes to type six digits.
	Skew = 1
	// secretBytes is the size of a generated secret. Twenty is the RFC 4226
	// recommendation and the size every app expects to see.
	secretBytes = 20
)

// encoding is base32 without padding. Authenticator apps universally reject
// the '=' characters that standard base32 would add.
var encoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewSecret returns a fresh base32 secret.
func NewSecret() (string, error) {
	buf := make([]byte, secretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("totp: read random: %w", err)
	}
	return encoding.EncodeToString(buf), nil
}

// URI builds the otpauth:// address an authenticator app scans or opens.
//
// The issuer appears twice, in the label and as a parameter, because apps
// disagree about which one they read and an account listed as a bare username
// is unidentifiable next to five others.
func URI(secret, issuer, account string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(Digits))
	q.Set("period", fmt.Sprint(Period))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// Code returns the code for a counter value.
func Code(secret string, counter uint64) (string, error) {
	key, err := encoding.DecodeString(normalise(secret))
	if err != nil {
		return "", fmt.Errorf("totp: secret is not base32: %w", err)
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	// Dynamic truncation, RFC 4226 §5.3: the low nibble of the last byte
	// picks where to read four bytes from, and the top bit is cleared so the
	// result is positive on every platform.
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	mod := uint32(1)
	for i := 0; i < Digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", Digits, value%mod), nil
}

// Validate checks a code against a secret at time now, refusing anything at or
// below lastUsed.
//
// It returns the counter the code matched, which the caller must store: that
// is what makes a code single-use. Passing the stored value back in as
// lastUsed closes the replay window; passing zero opens it.
func Validate(secret, code string, now time.Time, lastUsed uint64) (counter uint64, ok bool) {
	code = strings.TrimSpace(code)
	if len(code) != Digits {
		return 0, false
	}
	current := uint64(now.Unix()) / Period

	// Every candidate is checked even after a match, so the time taken does
	// not reveal which step succeeded — an attacker who can measure that
	// learns the victim's clock offset.
	var (
		matched   uint64
		found     bool
		lowest    = current - Skew
		highest   = current + Skew
		replayed  bool
		candidate string
	)
	if current < Skew {
		lowest = 0
	}
	for c := lowest; c <= highest; c++ {
		var err error
		candidate, err = Code(secret, c)
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) == 1 {
			matched, found = c, true
			if c <= lastUsed {
				replayed = true
			}
		}
	}
	if !found || replayed {
		return 0, false
	}
	return matched, true
}

// Counter is the step a time falls in. Exposed so a caller can record the
// value a successful validation returned.
func Counter(now time.Time) uint64 { return uint64(now.Unix()) / Period }

// normalise makes a hand-typed secret usable: apps display them in spaced
// groups and in lower case, and somebody will paste one back.
func normalise(secret string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
}
