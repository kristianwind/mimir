package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kristianwind/mimir/internal/secrets"
	"github.com/kristianwind/mimir/internal/totp"
)

// The alphabet has to be exactly 32 characters or base32.NewEncoding panics —
// at package initialisation, which means the binary starts and dies rather
// than failing to build. Generating one code is what proves it.
func TestARecoveryCodeCanActuallyBeGenerated(t *testing.T) {
	code, err := newRecoveryCode()
	if err != nil {
		t.Fatal(err)
	}
	if want := 19; len(code) != want {
		t.Errorf("code %q is %d characters, want %d", code, len(code), want)
	}
	if strings.Count(code, "-") != 3 {
		t.Errorf("code %q is not grouped for reading aloud", code)
	}
	// The four characters people misread must not be in it at all.
	for _, bad := range []string{"i", "l", "o", "u"} {
		if strings.Contains(code, bad) {
			t.Errorf("code %q contains %q, which is misread off a screen", code, bad)
		}
	}
}

// A code has to survive being typed back without its dashes, or in capitals,
// because that is how people will type it.
func TestARecoveryCodeSurvivesBeingTypedBack(t *testing.T) {
	code, err := newRecoveryCode()
	if err != nil {
		t.Fatal(err)
	}
	want := hashRecoveryCode(code)
	for _, variant := range []string{
		strings.ReplaceAll(code, "-", ""),
		strings.ToUpper(code),
		"  " + code + "  ",
	} {
		if got := hashRecoveryCode(variant); got != want {
			t.Errorf("%q hashed differently from %q", variant, code)
		}
	}
}

// Two codes must never collide, which is the property the whole recovery path
// rests on.
func TestRecoveryCodesAreNotReused(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		c, err := newRecoveryCode()
		if err != nil {
			t.Fatal(err)
		}
		if seen[c] {
			t.Fatal("the same recovery code was generated twice")
		}
		seen[c] = true
	}
}

// --------------------------------------------------------------- the gate

func twoFactorStore(t *testing.T) (*Store, *TwoFactor, int64) {
	t.Helper()
	s := newStore(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	vault, err := secrets.NewVault(key)
	if err != nil {
		t.Fatal(err)
	}
	tf := &TwoFactor{DB: s.DB, Vault: vault, Issuer: "Mimir"}
	s.TwoFactor = tf
	id := seedUser(t, s, "sabrina", "hunter2hunter2")
	return s, tf, id
}

func enrol(t *testing.T, tf *TwoFactor, id int64) (secret string, recovery []string) {
	t.Helper()
	secret, _, err := tf.Begin(context.Background(), id, "sabrina")
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.Code(secret, totp.Counter(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	recovery, err = tf.Confirm(context.Background(), id, code)
	if err != nil {
		t.Fatal(err)
	}
	return secret, recovery
}

// The whole point. A correct password on an enrolled account must produce no
// session at all — not a limited one, not one that needs upgrading. If a token
// comes back here, the second factor is decoration.
func TestACorrectPasswordAloneIssuesNoSession(t *testing.T) {
	s, tf, id := twoFactorStore(t)
	enrol(t, tf, id)

	token, _, err := s.Login(context.Background(), "sabrina", "hunter2hunter2", "", "test")
	if !errors.Is(err, ErrSecondFactorRequired) {
		t.Fatalf("err = %v, want ErrSecondFactorRequired", err)
	}
	if token != "" {
		t.Fatal("a session was issued to a password with no second factor")
	}
	// And the empty token must not resolve to anything either.
	if _, err := s.Resolve(context.Background(), token); err == nil {
		t.Error("the empty token resolved to a session")
	}
}

// An unconfirmed enrolment protects nothing and must not lock anybody out.
// Someone whose authenticator failed to scan has to still be able to sign in.
func TestAnUnconfirmedEnrolmentDoesNotLockTheAccount(t *testing.T) {
	s, tf, id := twoFactorStore(t)
	if _, _, err := tf.Begin(context.Background(), id, "sabrina"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Login(context.Background(), "sabrina", "hunter2hunter2", "", "test"); err != nil {
		t.Fatalf("a half-finished enrolment locked the account out: %v", err)
	}
}

// The full path: password plus a real code gets in.
func TestAPasswordAndACodeSignIn(t *testing.T) {
	s, tf, id := twoFactorStore(t)
	secret, _ := enrol(t, tf, id)

	// A step ahead of the one confirmation consumed, since a code cannot be
	// reused — which is itself the property being relied on here.
	code, err := totp.Code(secret, totp.Counter(time.Now())+1)
	if err != nil {
		t.Fatal(err)
	}
	token, user, err := s.Login(context.Background(), "sabrina", "hunter2hunter2", code, "test")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || user.ID != id {
		t.Fatalf("token %q user %d", token, user.ID)
	}
}

// A wrong code is refused, and refused without a session.
func TestAWrongCodeIsRefused(t *testing.T) {
	s, tf, id := twoFactorStore(t)
	enrol(t, tf, id)

	token, _, err := s.Login(context.Background(), "sabrina", "hunter2hunter2", "000000", "test")
	if !errors.Is(err, ErrSecondFactorInvalid) {
		t.Errorf("err = %v, want ErrSecondFactorInvalid", err)
	}
	if token != "" {
		t.Error("a session was issued against a wrong code")
	}
}

// The wrong password stays wrong whatever the code says, and must not report
// anything about the factor — that would turn the login form into a way of
// asking which accounts have one.
func TestAWrongPasswordIsStillJustAWrongPassword(t *testing.T) {
	s, tf, id := twoFactorStore(t)
	secret, _ := enrol(t, tf, id)
	code, err := totp.Code(secret, totp.Counter(time.Now())+1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Login(context.Background(), "sabrina", "wrong", code, "test"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("err = %v, want ErrInvalidCredentials", err)
	}
}

// A recovery code gets in exactly once. Being single-use is what makes it
// safe to write on a piece of paper.
func TestARecoveryCodeWorksOnceAndThenDoesNot(t *testing.T) {
	s, tf, id := twoFactorStore(t)
	_, recovery := enrol(t, tf, id)
	if len(recovery) != RecoveryCodeCount {
		t.Fatalf("got %d recovery codes, want %d", len(recovery), RecoveryCodeCount)
	}

	if _, _, err := s.Login(context.Background(), "sabrina", "hunter2hunter2", recovery[0], "test"); err != nil {
		t.Fatalf("a recovery code was refused: %v", err)
	}
	if _, _, err := s.Login(context.Background(), "sabrina", "hunter2hunter2", recovery[0], "test"); err == nil {
		t.Error("the same recovery code was accepted twice")
	}
	// The others are untouched.
	if _, _, err := s.Login(context.Background(), "sabrina", "hunter2hunter2", recovery[1], "test"); err != nil {
		t.Errorf("using one recovery code invalidated the rest: %v", err)
	}

	status, err := tf.Status(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if want := RecoveryCodeCount - 2; status.RecoveryRemaining != want {
		t.Errorf("%d codes left, want %d", status.RecoveryRemaining, want)
	}
}

// Enrolling over a confirmed factor would let anyone with a live session
// silently swap the thing protecting the account for one of their own.
func TestAConfirmedFactorCannotBeSilentlyReplaced(t *testing.T) {
	_, tf, id := twoFactorStore(t)
	enrol(t, tf, id)

	if _, _, err := tf.Begin(context.Background(), id, "sabrina"); err == nil {
		t.Error("a confirmed second factor was replaced without removing it first")
	}
}

// The secret must not be readable from the database by itself. A stolen
// database without the machine key has to be inert.
func TestTheSecretIsNotStoredInTheClear(t *testing.T) {
	_, tf, id := twoFactorStore(t)
	secret, _ := enrol(t, tf, id)

	var stored []byte
	if err := tf.DB.QueryRow(`SELECT secret FROM user_totp WHERE user_id = ?`, id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), secret) {
		t.Fatal("the TOTP secret is sitting in the database in plain text")
	}
}

// Removing the factor takes the recovery codes with it. Leaving them behind
// would mean a re-enrolment inherited codes printed against the old secret.
func TestDisablingTakesTheRecoveryCodesToo(t *testing.T) {
	s, tf, id := twoFactorStore(t)
	_, recovery := enrol(t, tf, id)

	if err := tf.Disable(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	status, err := tf.Status(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if status.Enrolled || status.Pending || status.RecoveryRemaining != 0 {
		t.Errorf("after disabling: %+v", status)
	}
	// The password alone is enough again, and an old recovery code is not a
	// way in.
	if _, _, err := s.Login(context.Background(), "sabrina", "hunter2hunter2", "", "test"); err != nil {
		t.Errorf("the password alone did not work after disabling: %v", err)
	}
	if err := tf.Check(context.Background(), id, recovery[0]); !errors.Is(err, ErrNotEnrolled) {
		t.Errorf("an old recovery code was still considered: %v", err)
	}
}
