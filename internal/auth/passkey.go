package auth

// Passkeys.
//
// A passkey is not a second password. It is a key pair whose private half
// never leaves the phone or the laptop's secure enclave, and which will only
// sign for the site it was made for — so a convincing copy of the sign-in page
// cannot collect anything, because the authenticator simply refuses to answer
// a domain it does not recognise. That is the property worth having, and it is
// the one a typed code cannot offer however careful the person typing is.
//
// Because of it, a passkey here signs in on its own rather than acting as a
// second step after a password. An authenticator that verified the user —
// a fingerprint, a face, a device PIN — has already provided two factors:
// something you have, and something you are or know. Demanding a password as
// well would add a phishable step to an unphishable one and call the result
// stronger.
//
// The domain is load-bearing. A credential is bound to the RP ID it was made
// under, so one enrolled at a different address is not merely inconvenient
// afterwards — it is unusable, and the browser will offer to overwrite a
// credential the server no longer knows about while the registration quietly
// does nothing.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// ErrNoPasskeys is returned when an account has none enrolled.
var ErrNoPasskeys = errors.New("auth: no passkeys enrolled")

// ErrCloned is a credential presenting a signature counter that has gone
// backwards, which is what a copied authenticator looks like.
var ErrCloned = errors.New("auth: the authenticator's counter went backwards; the credential may have been cloned")

// challengeTTL is how long a half-finished ceremony is remembered. Long
// enough to find your phone, short enough that an abandoned one is not left
// lying around.
const challengeTTL = 5 * time.Minute

// Passkeys stores and verifies WebAuthn credentials.
type Passkeys struct {
	DB *sql.DB
	// Web is nil when no origin is configured, and then every method here
	// refuses rather than guessing a domain. Guessing is how a credential
	// gets bound to the wrong one.
	Web *webauthn.WebAuthn
}

// NewPasskeys builds the WebAuthn configuration from the site's own address.
//
// The RP ID is the host of that address and nothing else. It is derived
// rather than configured separately so it cannot drift from where the site
// actually is — a mismatch does not fail loudly, it fails as a browser
// silently declining to produce a credential.
func NewPasskeys(db *sql.DB, baseURL, displayName string) (*Passkeys, error) {
	if baseURL == "" {
		return &Passkeys{DB: db}, nil
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("auth: passkeys need a valid base URL, got %q", baseURL)
	}
	w, err := webauthn.New(&webauthn.Config{
		RPID:          u.Hostname(),
		RPDisplayName: displayName,
		RPOrigins:     []string{strings.TrimSuffix(baseURL, "/")},
	})
	if err != nil {
		return nil, fmt.Errorf("auth: passkeys: %w", err)
	}
	return &Passkeys{DB: db, Web: w}, nil
}

// Available reports whether passkeys can be used on this instance.
func (p *Passkeys) Available() bool { return p != nil && p.Web != nil }

// RelyingParty is the hostname credentials are bound to.
//
// Reported publicly, because it is the public domain and every visitor
// already knows it — and because an instance quietly bound to "localhost"
// behind a real domain is the commonest way this is misconfigured, and
// otherwise the only symptom is a browser silently declining.
func (p *Passkeys) RelyingParty() string {
	if !p.Available() {
		return ""
	}
	return p.Web.Config.RPID
}

// Credential is one enrolled passkey, as the interface shows it.
type Credential struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	CreatedAt  string  `json:"createdAt"`
	LastUsedAt *string `json:"lastUsedAt,omitempty"`
}

// List returns a user's passkeys.
func (p *Passkeys) List(ctx context.Context, userID int64) ([]Credential, error) {
	rows, err := p.DB.QueryContext(ctx,
		`SELECT id, name, created_at, last_used_at FROM user_passkeys
		  WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Credential{}
	for rows.Next() {
		var c Credential
		var last sql.NullString
		if err := rows.Scan(&c.ID, &c.Name, &c.CreatedAt, &last); err != nil {
			return nil, err
		}
		if last.Valid {
			c.LastUsedAt = &last.String
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Count is how many a user has, which decides whether the sign-in page should
// offer the option at all.
func (p *Passkeys) Count(ctx context.Context, userID int64) (int, error) {
	var n int
	err := p.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM user_passkeys WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

// Delete removes one.
func (p *Passkeys) Delete(ctx context.Context, userID, id int64) error {
	_, err := p.DB.ExecContext(ctx,
		`DELETE FROM user_passkeys WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// ------------------------------------------------------------- registration

// BeginRegistration starts enrolling a new passkey for a signed-in user.
func (p *Passkeys) BeginRegistration(ctx context.Context, u User) (options any, challengeID string, err error) {
	if !p.Available() {
		return nil, "", errors.New("auth: passkeys are not configured on this instance")
	}
	wu, err := p.user(ctx, u)
	if err != nil {
		return nil, "", err
	}
	opts, session, err := p.Web.BeginRegistration(wu,
		// Ask the authenticator to verify the person, because that is what
		// makes a passkey two factors on its own rather than merely a key
		// somebody found.
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationRequired,
		}),
		// Never enrol a second credential from an authenticator that already
		// holds one for this account: it would silently replace it, and the
		// person would end up with a list of keys where one no longer works.
		webauthn.WithExclusions(wu.CredentialDescriptors()),
	)
	if err != nil {
		return nil, "", fmt.Errorf("auth: begin registration: %w", err)
	}
	id, err := p.storeChallenge(ctx, &u.ID, session)
	if err != nil {
		return nil, "", err
	}
	return opts, id, nil
}

// FinishRegistration verifies the authenticator's answer and stores the
// credential.
func (p *Passkeys) FinishRegistration(ctx context.Context, u User, challengeID, name string, body []byte) error {
	if !p.Available() {
		return errors.New("auth: passkeys are not configured on this instance")
	}
	session, owner, err := p.takeChallenge(ctx, challengeID)
	if err != nil {
		return err
	}
	// The challenge has to belong to the person answering it, or one user
	// could finish another's ceremony.
	if owner == nil || *owner != u.ID {
		return errors.New("auth: that registration was not started by this account")
	}

	parsed, err := protocol.ParseCredentialCreationResponseBytes(body)
	if err != nil {
		return fmt.Errorf("auth: malformed registration: %w", err)
	}
	wu, err := p.user(ctx, u)
	if err != nil {
		return err
	}
	cred, err := p.Web.CreateCredential(wu, *session, parsed)
	if err != nil {
		return fmt.Errorf("auth: registration rejected: %w", err)
	}

	if strings.TrimSpace(name) == "" {
		name = "Passkey"
	}
	transports := make([]string, 0, len(cred.Transport))
	for _, t := range cred.Transport {
		transports = append(transports, string(t))
	}
	_, err = p.DB.ExecContext(ctx,
		`INSERT INTO user_passkeys
		   (user_id, credential_id, public_key, attestation, transports, sign_count,
		    backup_eligible, backed_up, name)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, cred.ID, cred.PublicKey, cred.AttestationType,
		strings.Join(transports, ","), cred.Authenticator.SignCount,
		boolToInt(cred.Flags.BackupEligible), boolToInt(cred.Flags.BackupState),
		strings.TrimSpace(name))
	return err
}

// ------------------------------------------------------------------ sign-in

// BeginLogin starts a passwordless sign-in.
//
// No username is asked for. The credential itself carries who it belongs to,
// which is both friendlier and quieter: a form that asks for a name first
// tells anybody who types one whether that account exists.
func (p *Passkeys) BeginLogin(ctx context.Context) (options any, challengeID string, err error) {
	if !p.Available() {
		return nil, "", errors.New("auth: passkeys are not configured on this instance")
	}
	opts, session, err := p.Web.BeginDiscoverableLogin(
		webauthn.WithUserVerification(protocol.VerificationRequired),
	)
	if err != nil {
		return nil, "", fmt.Errorf("auth: begin login: %w", err)
	}
	id, err := p.storeChallenge(ctx, nil, session)
	if err != nil {
		return nil, "", err
	}
	return opts, id, nil
}

// FinishLogin verifies a signature and returns who signed it.
func (p *Passkeys) FinishLogin(ctx context.Context, challengeID string, body []byte) (User, error) {
	if !p.Available() {
		return User{}, errors.New("auth: passkeys are not configured on this instance")
	}
	session, _, err := p.takeChallenge(ctx, challengeID)
	if err != nil {
		return User{}, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(body)
	if err != nil {
		return User{}, fmt.Errorf("auth: malformed assertion: %w", err)
	}

	var signedIn User
	_, err = p.Web.ValidateDiscoverableLogin(
		func(rawID, userHandle []byte) (webauthn.User, error) {
			u, err := p.userByHandle(ctx, userHandle)
			if err != nil {
				return nil, err
			}
			signedIn = u.user
			return u, nil
		}, *session, parsed)
	if err != nil {
		return User{}, fmt.Errorf("auth: sign-in rejected: %w", err)
	}

	// The counter is the only signal a credential has been copied. Not every
	// authenticator keeps one — a zero on both sides means it does not, and
	// that is normal rather than suspicious.
	newCount := parsed.Response.AuthenticatorData.Counter
	var stored int64
	if err := p.DB.QueryRowContext(ctx,
		`SELECT sign_count FROM user_passkeys WHERE credential_id = ?`,
		parsed.RawID).Scan(&stored); err != nil {
		return User{}, err
	}
	if newCount != 0 && int64(newCount) <= stored {
		return User{}, ErrCloned
	}
	if _, err := p.DB.ExecContext(ctx,
		`UPDATE user_passkeys SET sign_count = ?, last_used_at = datetime('now')
		  WHERE credential_id = ?`, newCount, parsed.RawID); err != nil {
		return User{}, err
	}
	return signedIn, nil
}

// ----------------------------------------------------------------- internals

// webUser adapts a Mimir user to what the WebAuthn library expects.
type webUser struct {
	user  User
	creds []webauthn.Credential
}

func (w *webUser) WebAuthnID() []byte                         { return handleOf(w.user.ID) }
func (w *webUser) WebAuthnName() string                       { return w.user.Username }
func (w *webUser) WebAuthnDisplayName() string                { return w.user.Username }
func (w *webUser) WebAuthnCredentials() []webauthn.Credential { return w.creds }

// CredentialDescriptors lists what this user already has, so an authenticator
// holding one of them declines to make another.
func (w *webUser) CredentialDescriptors() []protocol.CredentialDescriptor {
	out := make([]protocol.CredentialDescriptor, 0, len(w.creds))
	for _, c := range w.creds {
		out = append(out, c.Descriptor())
	}
	return out
}

func (p *Passkeys) user(ctx context.Context, u User) (*webUser, error) {
	creds, err := p.credentials(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	return &webUser{user: u, creds: creds}, nil
}

func (p *Passkeys) userByHandle(ctx context.Context, handle []byte) (*webUser, error) {
	id, err := idFromHandle(handle)
	if err != nil {
		return nil, err
	}
	var u User
	if err := p.DB.QueryRowContext(ctx,
		`SELECT id, username, role FROM users WHERE id = ? AND disabled = 0`, id).
		Scan(&u.ID, &u.Username, &u.Role); err != nil {
		return nil, fmt.Errorf("auth: no such user for that passkey: %w", err)
	}
	return p.user(ctx, u)
}

func (p *Passkeys) credentials(ctx context.Context, userID int64) ([]webauthn.Credential, error) {
	rows, err := p.DB.QueryContext(ctx,
		`SELECT credential_id, public_key, attestation, transports, sign_count,
		        backup_eligible, backed_up
		   FROM user_passkeys WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []webauthn.Credential
	for rows.Next() {
		var (
			c                  webauthn.Credential
			transports         string
			count              int64
			eligible, backedUp int
		)
		if err := rows.Scan(&c.ID, &c.PublicKey, &c.AttestationType, &transports,
			&count, &eligible, &backedUp); err != nil {
			return nil, err
		}
		c.Authenticator.SignCount = uint32(count)
		c.Flags.BackupEligible = eligible == 1
		c.Flags.BackupState = backedUp == 1
		for _, t := range strings.Split(transports, ",") {
			if t != "" {
				c.Transport = append(c.Transport, protocol.AuthenticatorTransport(t))
			}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (p *Passkeys) storeChallenge(ctx context.Context, userID *int64, session *webauthn.SessionData) (string, error) {
	blob, err := json.Marshal(session)
	if err != nil {
		return "", err
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(raw)

	// Expired ceremonies are swept here rather than by a background job:
	// this is the only place that cares, and it runs often enough.
	if _, err := p.DB.ExecContext(ctx,
		`DELETE FROM webauthn_challenges WHERE expires_at < datetime('now')`); err != nil {
		return "", err
	}
	_, err = p.DB.ExecContext(ctx,
		`INSERT INTO webauthn_challenges (id, user_id, session, expires_at) VALUES (?, ?, ?, ?)`,
		id, userID, blob, time.Now().Add(challengeTTL).UTC().Format("2006-01-02 15:04:05"))
	return id, err
}

// takeChallenge reads a challenge and deletes it in the same breath, so one
// can never be answered twice.
func (p *Passkeys) takeChallenge(ctx context.Context, id string) (*webauthn.SessionData, *int64, error) {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	var (
		blob    []byte
		owner   sql.NullInt64
		expires string
	)
	err = tx.QueryRowContext(ctx,
		`SELECT session, user_id, expires_at FROM webauthn_challenges WHERE id = ?`, id).
		Scan(&blob, &owner, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, errors.New("auth: that request has expired; start again")
	}
	if err != nil {
		return nil, nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM webauthn_challenges WHERE id = ?`, id); err != nil {
		return nil, nil, err
	}
	if t, perr := time.Parse("2006-01-02 15:04:05", expires); perr == nil && time.Now().UTC().After(t) {
		_ = tx.Commit()
		return nil, nil, errors.New("auth: that request has expired; start again")
	}

	var session webauthn.SessionData
	if err := json.Unmarshal(blob, &session); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	var ownerID *int64
	if owner.Valid {
		v := owner.Int64
		ownerID = &v
	}
	return &session, ownerID, nil
}

// handleOf and idFromHandle turn a user id into the opaque handle WebAuthn
// stores on the authenticator and back.
//
// It is the id and not the username on purpose: the handle is written into
// the device and cannot be changed afterwards, so putting a name in it would
// mean a rename broke every passkey.
func handleOf(id int64) []byte {
	return []byte(fmt.Sprintf("mimir:%d", id))
}

func idFromHandle(handle []byte) (int64, error) {
	var id int64
	if _, err := fmt.Sscanf(string(handle), "mimir:%d", &id); err != nil {
		return 0, fmt.Errorf("auth: unrecognised passkey handle: %w", err)
	}
	return id, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
