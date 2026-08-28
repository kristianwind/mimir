package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/db"
)

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("hash = %q, want an argon2id encoding", hash)
	}

	ok, err := VerifyPassword("correct horse battery staple", hash)
	if err != nil || !ok {
		t.Errorf("correct password did not verify: ok=%v err=%v", ok, err)
	}
	ok, err = VerifyPassword("wrong", hash)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("wrong password verified")
	}
}

func TestPasswordHashesAreSalted(t *testing.T) {
	a, _ := HashPassword("same")
	b, _ := HashPassword("same")
	if a == b {
		t.Error("two hashes of the same password are identical; the salt is not random")
	}
}

func TestVerifyRejectsMalformedHash(t *testing.T) {
	for _, bad := range []string{"", "plaintext", "$argon2i$v=19$m=1,t=1,p=1$AA$AA"} {
		if _, err := VerifyPassword("x", bad); err == nil {
			t.Errorf("VerifyPassword accepted %q", bad)
		}
	}
}

func newStore(t *testing.T) *Store {
	t.Helper()
	conn, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return &Store{DB: conn}
}

func seedUser(t *testing.T, s *Store, username, password string) int64 {
	t.Helper()
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.DB.Exec(`INSERT INTO users (username, password_hash, role) VALUES (?, ?, 'user')`, username, hash)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestLoginIssuesAResolvableSession(t *testing.T) {
	s := newStore(t)
	id := seedUser(t, s, "sabrina", "hunter2hunter2")

	token, user, err := s.Login(context.Background(), "sabrina", "hunter2hunter2", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != id {
		t.Errorf("logged in as %d, want %d", user.ID, id)
	}

	resolved, err := s.Resolve(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Username != "sabrina" {
		t.Errorf("resolved %q", resolved.Username)
	}

	// The raw token must not be recoverable from the database.
	var stored string
	if err := s.DB.QueryRow(`SELECT token_hash FROM sessions`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == token {
		t.Error("the session token is stored in the clear")
	}
}

func TestLoginFailuresAreIndistinguishable(t *testing.T) {
	s := newStore(t)
	seedUser(t, s, "sabrina", "hunter2hunter2")

	_, _, wrongPass := s.Login(context.Background(), "sabrina", "nope", "", "test")
	_, _, noUser := s.Login(context.Background(), "ukendt", "nope", "", "test")
	if wrongPass == nil || noUser == nil {
		t.Fatal("both cases must fail")
	}
	if wrongPass.Error() != noUser.Error() {
		t.Errorf("error messages differ (%v vs %v); this enumerates usernames", wrongPass, noUser)
	}
}

func TestDisabledUserCannotLogIn(t *testing.T) {
	s := newStore(t)
	seedUser(t, s, "sabrina", "hunter2hunter2")
	if _, err := s.DB.Exec(`UPDATE users SET disabled = 1`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Login(context.Background(), "sabrina", "hunter2hunter2", "", "test"); err == nil {
		t.Error("a disabled user logged in")
	}
}

func TestLogoutRevokes(t *testing.T) {
	s := newStore(t)
	seedUser(t, s, "sabrina", "hunter2hunter2")
	token, _, err := s.Login(context.Background(), "sabrina", "hunter2hunter2", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Logout(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(context.Background(), token); err == nil {
		t.Error("a revoked token still resolves")
	}
}

func TestResolveRejectsExpiredSession(t *testing.T) {
	s := newStore(t)
	seedUser(t, s, "sabrina", "hunter2hunter2")
	token, _, err := s.Login(context.Background(), "sabrina", "hunter2hunter2", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE sessions SET expires_at = '2020-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(context.Background(), token); err != ErrNoSession {
		t.Errorf("expired session resolved with %v", err)
	}
}
