package api

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/kristianwind/mimir/internal/auth"
	"github.com/kristianwind/mimir/internal/db"
)

// emptyServer has no users at all, which is the only state bootstrap works in.
func emptyServer(t *testing.T) *Server {
	t.Helper()
	conn, err := db.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return &Server{DB: conn, Auth: &auth.Store{DB: conn}, Log: slog.Default()}
}

func post(s *Server, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, r)
	return w
}

func get(s *Server, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, r)
	return w
}

func TestBootstrapCreatesTheFirstAdminAndLogsThemIn(t *testing.T) {
	s := emptyServer(t)

	if w := get(s, "/api/auth/bootstrap"); !strings.Contains(w.Body.String(), "true") {
		t.Fatalf("an empty instance did not offer bootstrap: %s", w.Body)
	}

	w := post(s, "/api/auth/bootstrap",
		`{"username":"sabrina","password":"correct-horse-battery"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("bootstrap gave %d: %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"role":"admin"`) {
		t.Errorf("the first user is not an admin: %s", w.Body)
	}
	// Straight in: they typed the credentials a second ago.
	if len(w.Result().Cookies()) == 0 {
		t.Error("no session cookie was issued")
	}
}

func TestBootstrapClosesItselfImmediately(t *testing.T) {
	s := emptyServer(t)
	// A non-ASCII username on purpose: it has to survive the round trip
	// through SQLite and back out of the session lookup.
	post(s, "/api/auth/bootstrap", `{"username":"zoë","password":"correct-horse-battery"}`)

	if w := get(s, "/api/auth/bootstrap"); !strings.Contains(w.Body.String(), "false") {
		t.Errorf("bootstrap still offered after a user exists: %s", w.Body)
	}
	w := post(s, "/api/auth/bootstrap", `{"username":"chancer","password":"correct-horse-battery"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("a second bootstrap gave %d, want 409: %s", w.Code, w.Body)
	}

	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	if n != 1 {
		t.Errorf("instance has %d users, want 1", n)
	}
}

func TestBootstrapIsAtomicUnderConcurrency(t *testing.T) {
	// Checking "are there users?" and then inserting leaves a window where
	// two requests both see none and both succeed — and the second would be
	// an administrator nobody asked for.
	s := emptyServer(t)

	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := post(s, "/api/auth/bootstrap",
				`{"username":"candidate`+string(rune('a'+i))+`","password":"correct-horse-battery"}`)
			codes[i] = w.Code
		}(i)
	}
	wg.Wait()

	created := 0
	for _, c := range codes {
		if c == http.StatusCreated {
			created++
		}
	}
	if created != 1 {
		t.Errorf("%d of 8 concurrent bootstraps succeeded, want exactly 1: %v", created, codes)
	}

	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	if n != 1 {
		t.Errorf("instance has %d users after the race, want 1", n)
	}
}

func TestBootstrapEnforcesThePasswordFloor(t *testing.T) {
	s := emptyServer(t)
	w := post(s, "/api/auth/bootstrap", `{"username":"sabrina","password":"kort"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("a short password gave %d, want 400", w.Code)
	}
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	if n != 0 {
		t.Error("a rejected bootstrap still created a user")
	}
}

func TestBootstrapNeedsAUsername(t *testing.T) {
	s := emptyServer(t)
	if w := post(s, "/api/auth/bootstrap", `{"password":"correct-horse-battery"}`); w.Code != http.StatusBadRequest {
		t.Errorf("an empty username gave %d, want 400", w.Code)
	}
}
