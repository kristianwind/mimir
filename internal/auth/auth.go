// Package auth provides password hashing, session issuing and the middleware
// that puts a user on a request context.
//
// Parameters match Yggdrasil so an operator running both runes reasons about
// one security model, not two: argon2id at 64 MB / 3 iterations / 4 lanes,
// opaque random session tokens stored only as SHA-256 hashes.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/kristianwind/mimir/internal/i18n"
	"time"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. Changing these is safe: the cost is encoded in every
// stored hash, so old hashes keep verifying and are rehashed on next login.
const (
	argonMemory  = 64 * 1024 // KiB
	argonTime    = 3
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16
)

// SessionLifetime is how long a session cookie stays valid.
const SessionLifetime = 30 * 24 * time.Hour

// CookieName is the session cookie's name.
const CookieName = "mimir_session"

// ErrInvalidCredentials is deliberately returned for both an unknown user and
// a wrong password, so the API cannot be used to enumerate accounts.
var ErrInvalidCredentials = errors.New("auth: invalid credentials")

// ErrNoSession is returned when a request carries no valid session.
var ErrNoSession = errors.New("auth: no session")

// HashPassword returns an encoded argon2id hash.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword checks a password against an encoded hash.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, fmt.Errorf("auth: unrecognised hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, fmt.Errorf("auth: bad hash version: %w", err)
	}
	var memory uint32
	var iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return false, fmt.Errorf("auth: bad hash parameters: %w", err)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("auth: bad salt: %w", err)
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("auth: bad key: %w", err)
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// User is the authenticated principal carried on a request context.
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// IsAdmin reports whether the user may manage other users.
func (u User) IsAdmin() bool { return u.Role == "admin" }

// Store issues and validates sessions.
type Store struct {
	DB *sql.DB
	// Secure controls the cookie's Secure flag. It is off only for local
	// http development; the rune deployment always terminates TLS.
	Secure bool
}

// Login verifies credentials and issues a session, returning the raw token.
func (s *Store) Login(ctx context.Context, username, password, userAgent string) (string, User, error) {
	var (
		u        User
		hash     string
		disabled bool
	)
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, username, role, password_hash, disabled FROM users WHERE username = ?`,
		username,
	).Scan(&u.ID, &u.Username, &u.Role, &hash, &disabled)
	if errors.Is(err, sql.ErrNoRows) {
		// Spend comparable time on a miss so response timing does not leak
		// whether the username exists.
		_, _ = HashPassword(password)
		return "", User{}, ErrInvalidCredentials
	}
	if err != nil {
		return "", User{}, err
	}
	if disabled {
		return "", User{}, ErrInvalidCredentials
	}

	ok, err := VerifyPassword(password, hash)
	if err != nil {
		return "", User{}, err
	}
	if !ok {
		return "", User{}, ErrInvalidCredentials
	}

	token, err := s.issue(ctx, u.ID, userAgent)
	if err != nil {
		return "", User{}, err
	}
	return token, u, nil
}

func (s *Store) issue(ctx context.Context, userID int64, userAgent string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("auth: read token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO sessions (user_id, token_hash, user_agent, expires_at) VALUES (?, ?, ?, ?)`,
		userID, hashToken(token), userAgent, time.Now().Add(SessionLifetime).UTC().Format(time.RFC3339),
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

// Logout revokes a session token.
func (s *Store) Logout(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}

// Resolve looks up the user behind a session token.
func (s *Store) Resolve(ctx context.Context, token string) (User, error) {
	var (
		u       User
		expires string
	)
	err := s.DB.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.role, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND u.disabled = 0`,
		hashToken(token),
	).Scan(&u.ID, &u.Username, &u.Role, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNoSession
	}
	if err != nil {
		return User{}, err
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil || time.Now().After(exp) {
		return User{}, ErrNoSession
	}
	return u, nil
}

// SetCookie writes the session cookie.
func (s *Store) SetCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Now().Add(SessionLifetime),
	})
}

// ClearCookie expires the session cookie.
func (s *Store) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

type ctxKey struct{}

// Middleware resolves the session cookie and rejects unauthenticated requests.
//
// The rejection is JSON, like every other error the API returns. A plain-text
// body here reaches a client that parses every response as JSON and surfaces
// as a syntax error rather than "your session expired" — which is a confusing
// way to say something entirely routine.
func (s *Store) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(CookieName)
		if err != nil {
			unauthorized(w, r)
			return
		}
		u, err := s.Resolve(r.Context(), c.Value)
		if err != nil {
			unauthorized(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	})
}

// unauthorized answers in JSON rather than plain text: the client parses every
// error body, and a bare string here used to surface as a parse failure instead
// of the real reason. It takes the request so the sentence can be translated
// like every other error.
func unauthorized(w http.ResponseWriter, r *http.Request) {
	lang := i18n.FromRequest(r)
	body, err := json.Marshal(map[string]string{
		"error": i18n.T(lang, "your session has expired"),
		"hint":  i18n.T(lang, "Log in again."),
	})
	if err != nil {
		body = []byte(`{"error":"your session has expired"}`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write(body)
}

// FromContext returns the authenticated user, if any.
func FromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxKey{}).(User)
	return u, ok
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
