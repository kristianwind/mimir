package auth

// The second factor.
//
// Two rules shape everything here, and both exist because getting them wrong
// is worse than having no second factor at all.
//
// Enrolment is not complete until a code has been proved. Between generating
// a secret and the user typing a code from it, the row exists and does not
// protect anything — because if it did, a user whose authenticator failed to
// scan would have locked themselves out of their own account with no way
// back. Nothing may treat an unconfirmed row as enabled.
//
// There is always a way in that does not need the phone. Recovery codes are
// issued at enrolment and shown exactly once; the break-glass CLI on the box
// clears the factor entirely. A second factor with no recovery path is a
// timer counting down to a support incident.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kristianwind/mimir/internal/secrets"
	"github.com/kristianwind/mimir/internal/totp"
)

// ErrSecondFactorRequired says the password was right and is not enough.
//
// Distinct from ErrInvalidCredentials on purpose: the caller has already
// proved the password, so telling them a factor is needed reveals nothing
// they did not just demonstrate they knew.
var ErrSecondFactorRequired = errors.New("auth: second factor required")

// ErrSecondFactorInvalid is a wrong or reused code.
var ErrSecondFactorInvalid = errors.New("auth: second factor is not valid")

// ErrNotEnrolled is returned when there is no factor to act on.
var ErrNotEnrolled = errors.New("auth: no second factor is enrolled")

// RecoveryCodeCount is how many are issued at enrolment.
const RecoveryCodeCount = 10

// recoveryCodeBytes gives 80 bits of entropy per code, which is why they can
// be stored under a fast hash.
const recoveryCodeBytes = 10

// TwoFactor stores and checks second factors.
type TwoFactor struct {
	DB    *sql.DB
	Vault *secrets.Vault
	// Issuer is the name an authenticator app lists the account under.
	Issuer string
	// Now is swappable so tests do not depend on the wall clock.
	Now func() time.Time
}

func (t *TwoFactor) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

// Status reports what protection an account actually has.
//
// Enrolled means confirmed. A pending row is reported separately so the UI can
// offer to finish or abandon it, and never as protection.
type Status struct {
	Enrolled bool `json:"enrolled"`
	Pending  bool `json:"pending"`
	// RecoveryRemaining is how many unused codes are left. Users burn
	// through these without noticing, and discovering there are none left at
	// the moment the phone is lost is discovering it too late.
	RecoveryRemaining int `json:"recoveryRemaining"`
}

// Status returns the state of a user's second factor.
func (t *TwoFactor) Status(ctx context.Context, userID int64) (Status, error) {
	var confirmed sql.NullString
	err := t.DB.QueryRowContext(ctx,
		`SELECT confirmed_at FROM user_totp WHERE user_id = ?`, userID).Scan(&confirmed)
	if errors.Is(err, sql.ErrNoRows) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	out := Status{Enrolled: confirmed.Valid, Pending: !confirmed.Valid}
	if !out.Enrolled {
		return out, nil
	}
	if err := t.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM user_recovery_codes WHERE user_id = ? AND used_at IS NULL`,
		userID).Scan(&out.RecoveryRemaining); err != nil {
		return Status{}, err
	}
	return out, nil
}

// Enrolled reports whether a confirmed factor exists. This is the only
// question the login path may ask.
func (t *TwoFactor) Enrolled(ctx context.Context, userID int64) (bool, error) {
	s, err := t.Status(ctx, userID)
	return s.Enrolled, err
}

// Begin starts an enrolment, returning the secret and the otpauth URI.
//
// Replaces any unconfirmed attempt, so a user who closed the page and came
// back gets a fresh secret rather than one they no longer have. It refuses to
// touch a confirmed one: overwriting that would let anyone with a live
// session silently replace the factor protecting the account.
func (t *TwoFactor) Begin(ctx context.Context, userID int64, username string) (secret, uri string, err error) {
	enrolled, err := t.Enrolled(ctx, userID)
	if err != nil {
		return "", "", err
	}
	if enrolled {
		return "", "", errors.New("auth: a second factor is already enrolled; remove it first")
	}

	secret, err = totp.NewSecret()
	if err != nil {
		return "", "", err
	}
	nonce, sealed, err := t.Vault.Seal([]byte(secret))
	if err != nil {
		return "", "", err
	}
	if _, err := t.DB.ExecContext(ctx,
		`INSERT INTO user_totp (user_id, nonce, secret, last_counter, confirmed_at)
		 VALUES (?, ?, ?, 0, NULL)
		 ON CONFLICT(user_id) DO UPDATE SET
		   nonce = excluded.nonce, secret = excluded.secret,
		   last_counter = 0, confirmed_at = NULL`,
		userID, nonce, sealed); err != nil {
		return "", "", err
	}
	return secret, totp.URI(secret, t.issuer(), username), nil
}

// Confirm finishes an enrolment by proving a code, and returns the recovery
// codes. They are shown once and never again: they are stored hashed, so
// nobody — including whoever runs the server — can produce them a second
// time.
func (t *TwoFactor) Confirm(ctx context.Context, userID int64, code string) ([]string, error) {
	secret, _, confirmed, err := t.load(ctx, userID)
	if err != nil {
		return nil, err
	}
	if confirmed {
		return nil, errors.New("auth: already confirmed")
	}
	counter, ok := totp.Validate(secret, code, t.now(), 0)
	if !ok {
		return nil, ErrSecondFactorInvalid
	}

	tx, err := t.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`UPDATE user_totp SET confirmed_at = datetime('now'), last_counter = ? WHERE user_id = ?`,
		counter, userID); err != nil {
		return nil, err
	}
	// Any codes from a previous enrolment are void; they were printed
	// against a factor that no longer exists.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM user_recovery_codes WHERE user_id = ?`, userID); err != nil {
		return nil, err
	}
	codes, err := t.issueRecoveryCodes(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return codes, nil
}

// Check verifies a code at login, accepting either a TOTP code or an unused
// recovery code.
//
// A recovery code is consumed by using it, which is the whole point: it is a
// way back in that cannot be replayed by whoever else has seen the piece of
// paper.
func (t *TwoFactor) Check(ctx context.Context, userID int64, code string) error {
	secret, lastCounter, confirmed, err := t.load(ctx, userID)
	if errors.Is(err, ErrNotEnrolled) {
		return ErrNotEnrolled
	}
	if err != nil {
		return err
	}
	if !confirmed {
		return ErrNotEnrolled
	}

	if counter, ok := totp.Validate(secret, code, t.now(), lastCounter); ok {
		_, err := t.DB.ExecContext(ctx,
			`UPDATE user_totp SET last_counter = ? WHERE user_id = ?`, counter, userID)
		return err
	}
	return t.consumeRecoveryCode(ctx, userID, code)
}

// Disable removes the factor and every recovery code with it.
func (t *TwoFactor) Disable(ctx context.Context, userID int64) error {
	tx, err := t.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_totp WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM user_recovery_codes WHERE user_id = ?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// RegenerateRecoveryCodes issues a fresh set and voids the old one.
func (t *TwoFactor) RegenerateRecoveryCodes(ctx context.Context, userID int64) ([]string, error) {
	enrolled, err := t.Enrolled(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !enrolled {
		return nil, ErrNotEnrolled
	}
	tx, err := t.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM user_recovery_codes WHERE user_id = ?`, userID); err != nil {
		return nil, err
	}
	codes, err := t.issueRecoveryCodes(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return codes, nil
}

// load returns the decrypted secret and the row's state.
func (t *TwoFactor) load(ctx context.Context, userID int64) (secret string, lastCounter uint64, confirmed bool, err error) {
	var (
		nonce, sealed []byte
		confirmedAt   sql.NullString
		counter       int64
	)
	err = t.DB.QueryRowContext(ctx,
		`SELECT nonce, secret, last_counter, confirmed_at FROM user_totp WHERE user_id = ?`,
		userID).Scan(&nonce, &sealed, &counter, &confirmedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, false, ErrNotEnrolled
	}
	if err != nil {
		return "", 0, false, err
	}
	plain, err := t.Vault.Open(nonce, sealed)
	if err != nil {
		return "", 0, false, err
	}
	return string(plain), uint64(counter), confirmedAt.Valid, nil
}

func (t *TwoFactor) issueRecoveryCodes(ctx context.Context, tx *sql.Tx, userID int64) ([]string, error) {
	out := make([]string, 0, RecoveryCodeCount)
	for i := 0; i < RecoveryCodeCount; i++ {
		code, err := newRecoveryCode()
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO user_recovery_codes (user_id, code_hash) VALUES (?, ?)`,
			userID, hashRecoveryCode(code)); err != nil {
			return nil, err
		}
		out = append(out, code)
	}
	return out, nil
}

func (t *TwoFactor) consumeRecoveryCode(ctx context.Context, userID int64, code string) error {
	res, err := t.DB.ExecContext(ctx,
		`UPDATE user_recovery_codes SET used_at = datetime('now')
		 WHERE user_id = ? AND code_hash = ? AND used_at IS NULL`,
		userID, hashRecoveryCode(code))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrSecondFactorInvalid
	}
	return nil
}

func (t *TwoFactor) issuer() string {
	if t.Issuer != "" {
		return t.Issuer
	}
	return "Mimir"
}

// recoveryEncoding is Crockford's alphabet: the digits and letters with i, l,
// o and u removed, because these codes get read off a screen and typed back
// by hand and those four are the ones people misread.
//
// It must be exactly 32 characters — base32.NewEncoding panics otherwise, and
// it would do so at package initialisation, which is to say when the binary
// starts rather than when it is built.
var recoveryEncoding = base32.NewEncoding("0123456789abcdefghjkmnpqrstvwxyz").WithPadding(base32.NoPadding)

func newRecoveryCode() (string, error) {
	buf := make([]byte, recoveryCodeBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: recovery code: %w", err)
	}
	s := recoveryEncoding.EncodeToString(buf)
	// Grouped, because these get copied out by hand.
	return s[:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:], nil
}

// hashRecoveryCode normalises before hashing, so a code typed back with the
// dashes left out, or in the wrong case, still works.
func hashRecoveryCode(code string) string {
	norm := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])
}
