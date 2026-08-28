package totp

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"
)

// The RFC 6238 appendix B vectors, which is the only way to know this is
// interoperable rather than merely self-consistent. The published table is
// for eight digits, so these are the low six of each.
//
// The RFC's SHA1 key is the ASCII "12345678901234567890"; the document gives
// it as hex, and every authenticator app wants base32, so it is converted
// here rather than pasted as a magic string.
func TestAgainstTheRFCVectors(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).
		EncodeToString([]byte("12345678901234567890"))

	for _, tc := range []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	} {
		got, err := Code(secret, uint64(tc.unix)/Period)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("at unix %d got %s, want %s", tc.unix, got, tc.want)
		}
	}
}

// A code is good for its own step and one either side, and nothing else. The
// window is the whole security margin: every extra step accepted is another
// code valid at this instant.
func TestTheWindowIsOneStepEitherSide(t *testing.T) {
	secret, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	current := Counter(now)

	for _, tc := range []struct {
		offset int64
		want   bool
	}{
		{-2, false},
		{-1, true},
		{0, true},
		{1, true},
		{2, false},
	} {
		code, err := Code(secret, uint64(int64(current)+tc.offset))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := Validate(secret, code, now, 0); ok != tc.want {
			t.Errorf("step %+d accepted = %v, want %v", tc.offset, ok, tc.want)
		}
	}
}

// Within its thirty seconds a code is otherwise replayable by anyone who saw
// it over a shoulder or in a screenshot. Recording the counter is what stops
// that, so a caller who stores it must find the code refused the second time.
func TestACodeCannotBeUsedTwice(t *testing.T) {
	secret, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	code, err := Code(secret, Counter(now))
	if err != nil {
		t.Fatal(err)
	}

	used, ok := Validate(secret, code, now, 0)
	if !ok {
		t.Fatal("a fresh code was refused")
	}
	if _, ok := Validate(secret, code, now, used); ok {
		t.Error("the same code was accepted a second time")
	}
	// And so is an older one, which is the same attack a step earlier.
	older, err := Code(secret, Counter(now)-1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := Validate(secret, older, now, used); ok {
		t.Error("a code from before the last one used was accepted")
	}
}

// Authenticator apps show secrets in spaced groups and some paste them back
// in lower case. Refusing those is a support problem, not a security one.
func TestASecretSurvivesBeingTypedBack(t *testing.T) {
	secret, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	code, err := Code(secret, Counter(now))
	if err != nil {
		t.Fatal(err)
	}

	spaced := strings.ToLower(strings.Join(chunk(secret, 4), " "))
	if _, ok := Validate(spaced, code, now, 0); !ok {
		t.Errorf("the secret as an app displays it was rejected: %q", spaced)
	}
}

// The URI is what an app actually consumes, so the parts it reads have to be
// there. An account listed as a bare username is unidentifiable next to five
// others, which is why the issuer appears in both places.
func TestTheEnrolmentURICarriesWhatAppsRead(t *testing.T) {
	uri := URI("ABCDEFGHIJKLMNOP", "Mimir", "sabrina@example.com")
	for _, want := range []string{
		"otpauth://totp/",
		"secret=ABCDEFGHIJKLMNOP",
		"issuer=Mimir",
		"digits=6",
		"period=30",
		"algorithm=SHA1",
	} {
		if !strings.Contains(uri, want) {
			t.Errorf("the URI is missing %q: %s", want, uri)
		}
	}
	// The label is "issuer:account". The colon is the separator and the key
	// URI format allows it literally or as %3A; apps read both.
	if !strings.Contains(uri, "Mimir:sabrina") && !strings.Contains(uri, "Mimir%3Asabrina") {
		t.Errorf("the label does not carry the issuer: %s", uri)
	}
}

// Anything that is not six digits is not a code, and must not reach the
// comparison at all.
func TestRubbishIsRefusedWithoutBeingCompared(t *testing.T) {
	secret, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	for _, code := range []string{"", "12345", "1234567", "abcdef", "  "} {
		if _, ok := Validate(secret, code, now, 0); ok {
			t.Errorf("%q was accepted as a code", code)
		}
	}
}

// Two enrolments must not share a secret, which is the one property that
// makes the whole thing worth having.
func TestSecretsAreNotReused(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		s, err := NewSecret()
		if err != nil {
			t.Fatal(err)
		}
		if seen[s] {
			t.Fatal("the same secret was generated twice")
		}
		seen[s] = true
	}
}

func chunk(s string, n int) []string {
	var out []string
	for len(s) > n {
		out = append(out, s[:n])
		s = s[n:]
	}
	return append(out, s)
}
