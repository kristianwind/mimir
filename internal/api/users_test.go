package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/auth"
	"github.com/kristianwind/mimir/internal/db"
)

// newServer returns a server with an admin and a plain user, plus a helper
// that performs requests as one of them.
func newServer(t *testing.T) (*Server, func(as, method, path, body string) *httptest.ResponseRecorder) {
	t.Helper()
	conn, err := db.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	store := &auth.Store{DB: conn}
	s := &Server{DB: conn, Auth: store, Log: slog.Default()}

	seed := func(name, role string) {
		hash, err := auth.HashPassword("correct-horse-battery")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(
			`INSERT INTO users (username, password_hash, role) VALUES (?, ?, ?)`,
			name, hash, role); err != nil {
			t.Fatal(err)
		}
	}
	seed("boss", "admin")
	seed("member", "user")

	do := func(as, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if body == "" {
			r = httptest.NewRequest(method, path, nil)
		} else {
			r = httptest.NewRequest(method, path, strings.NewReader(body))
		}

		// A real login and a real cookie: the middleware is part of what
		// these tests are checking, and injecting a user past it would
		// leave the actual authorisation path untested.
		token, _, err := store.Login(context.Background(), as, "correct-horse-battery", "", "test")
		if err != nil {
			t.Fatalf("could not log in as %q: %v", as, err)
		}
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})

		w := httptest.NewRecorder()
		s.Router().ServeHTTP(w, r)
		return w
	}
	return s, do
}

func TestOnlyAdminsManageUsers(t *testing.T) {
	_, do := newServer(t)

	for _, c := range []struct{ method, path, body string }{
		{"GET", "/api/users", ""},
		{"POST", "/api/users", `{"username":"ny","password":"correct-horse-battery"}`},
		{"PUT", "/api/users/2", `{"role":"admin"}`},
		{"DELETE", "/api/users/2", ""},
	} {
		w := do("member", c.method, c.path, c.body)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s as a plain user gave %d, want 403", c.method, c.path, w.Code)
		}
	}
}

func TestCreateAndListUsers(t *testing.T) {
	_, do := newServer(t)

	w := do("boss", "POST", "/api/users",
		`{"username":"tredje","password":"correct-horse-battery","role":"user"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create gave %d: %s", w.Code, w.Body)
	}

	w = do("boss", "GET", "/api/users", "")
	var users []userRecord
	if err := json.Unmarshal(w.Body.Bytes(), &users); err != nil {
		t.Fatal(err)
	}
	if len(users) != 3 {
		t.Fatalf("got %d users, want 3", len(users))
	}
	// No hash may leave the server, not even to an admin.
	if strings.Contains(w.Body.String(), "argon2") {
		t.Error("the response carries a password hash")
	}
}

func TestCreateRejectsAShortPasswordAndADuplicate(t *testing.T) {
	_, do := newServer(t)

	w := do("boss", "POST", "/api/users", `{"username":"kort","password":"kort"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("short password gave %d, want 400", w.Code)
	}

	w = do("boss", "POST", "/api/users",
		`{"username":"member","password":"correct-horse-battery"}`)
	if w.Code != http.StatusConflict {
		t.Errorf("duplicate username gave %d, want 409: %s", w.Code, w.Body)
	}
}

func TestCannotRemoveTheLastAdmin(t *testing.T) {
	s, do := newServer(t)

	var adminID int64
	s.DB.QueryRow(`SELECT id FROM users WHERE username = 'boss'`).Scan(&adminID)
	id := "/api/users/" + itoa(adminID)

	// Demoting, disabling and deleting are three routes to the same
	// unrecoverable state: an instance nobody can administer.
	for _, c := range []struct{ what, method, body string }{
		{"demote", "PUT", `{"role":"user"}`},
		{"disable", "PUT", `{"disabled":true}`},
		{"delete", "DELETE", ""},
	} {
		w := do("boss", c.method, id, c.body)
		if w.Code != http.StatusConflict {
			t.Errorf("%s of the last admin gave %d, want 409", c.what, w.Code)
		}
	}

	// With a second admin, the same operations are fine.
	do("boss", "PUT", "/api/users/2", `{"role":"admin"}`)
	if w := do("boss", "PUT", id, `{"role":"user"}`); w.Code != http.StatusOK {
		t.Errorf("demoting with a second admin present gave %d: %s", w.Code, w.Body)
	}
}

func TestDisablingRevokesSessions(t *testing.T) {
	s, do := newServer(t)

	token, _, err := s.Auth.Login(context.Background(), "member", "correct-horse-battery", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Auth.Resolve(context.Background(), token); err != nil {
		t.Fatal(err)
	}

	if w := do("boss", "PUT", "/api/users/2", `{"disabled":true}`); w.Code != http.StatusOK {
		t.Fatalf("disable gave %d: %s", w.Code, w.Body)
	}
	// A disabled account whose session still works is not disabled.
	if _, err := s.Auth.Resolve(context.Background(), token); err == nil {
		t.Error("the session of a disabled user still resolves")
	}
}

func TestAdminPasswordResetRevokesSessions(t *testing.T) {
	s, do := newServer(t)

	token, _, err := s.Auth.Login(context.Background(), "member", "correct-horse-battery", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if w := do("boss", "PUT", "/api/users/2", `{"password":"et-helt-nyt-kodeord"}`); w.Code != http.StatusOK {
		t.Fatalf("reset gave %d: %s", w.Code, w.Body)
	}
	if _, err := s.Auth.Resolve(context.Background(), token); err == nil {
		t.Error("a reset password left the old session alive")
	}
	if _, _, err := s.Auth.Login(context.Background(), "member", "et-helt-nyt-kodeord", "", "test"); err != nil {
		t.Errorf("the new password does not work: %v", err)
	}
}

func TestChangeOwnPasswordNeedsTheCurrentOne(t *testing.T) {
	s, do := newServer(t)

	w := do("member", "PUT", "/api/me/password",
		`{"current":"wrong","new":"et-helt-nyt-kodeord"}`)
	if w.Code != http.StatusForbidden {
		t.Errorf("a wrong current password gave %d, want 403", w.Code)
	}

	w = do("member", "PUT", "/api/me/password",
		`{"current":"correct-horse-battery","new":"et-helt-nyt-kodeord"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("change gave %d: %s", w.Code, w.Body)
	}
	if _, _, err := s.Auth.Login(context.Background(), "member", "et-helt-nyt-kodeord", "", "test"); err != nil {
		t.Errorf("the new password does not work: %v", err)
	}
	// The caller keeps a working session rather than being logged out of
	// the page they are standing on.
	if len(w.Result().Cookies()) == 0 {
		t.Error("no fresh session cookie was issued")
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
