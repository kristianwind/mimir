package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/kristianwind/mimir/internal/auth"
)

// First-run bootstrap.
//
// Mimir ships with no default account, which is the right call — a
// self-hosted app with a known default login is a break-in waiting to happen
// — but it left the first user reachable only through a shell on the host.
// That is a poor first five minutes and, on a container, means SSH.
//
// So the login page offers to create the first administrator, and only then.
// The window is not guarded by a flag or a timer that could be forgotten: it
// is the absence of any user at all, and it closes in the same instant the
// first one exists. There is nothing to switch off afterwards, because there
// is nothing left to switch off.

// handleBootstrapStatus reports whether the instance has no users yet.
//
// Public by necessity: nobody can authenticate before the first account
// exists. It reveals only that fact, which an attacker learns anyway by
// trying the endpoint below.
func (s *Server) handleBootstrapStatus(w http.ResponseWriter, r *http.Request) {
	needed, err := s.needsBootstrap(r.Context())
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"needed": needed})
}

// handleBootstrap creates the first administrator.
func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, r, http.StatusBadRequest, "malformed request", "")
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if body.Username == "" {
		writeError(w, r, http.StatusBadRequest, "username is missing", "")
		return
	}
	if err := checkPassword(body.Password); err != nil {
		writeError(w, r, http.StatusBadRequest, passwordError(r, err), "")
		return
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}

	// The guard is in the statement, not around it. Checking "are there
	// users?" and then inserting leaves a window where two requests both see
	// none and both succeed — and the second one would be an administrator
	// nobody asked for.
	res, err := s.DB.ExecContext(r.Context(), `
		INSERT INTO users (username, password_hash, role)
		SELECT ?, ?, 'admin' WHERE NOT EXISTS (SELECT 1 FROM users)`,
		body.Username, hash)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	n, err := res.RowsAffected()
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	if n == 0 {
		writeError(w, r, http.StatusConflict,
			"the instance already has a user",
			"Log in instead. If you have lost access, create a new user with `mimir useradd` on the host.")
		return
	}

	// Straight in rather than back to a login form: they typed the
	// credentials a second ago, and asking for them again is friction with
	// no security in it.
	token, user, err := s.Auth.Login(r.Context(), body.Username, body.Password, r.UserAgent())
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	s.Auth.SetCookie(w, token)

	if _, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO audit_log (user_id, action, resource) VALUES (?, 'user.bootstrap', ?)`,
		user.ID, body.Username); err != nil && s.Log != nil {
		s.Log.Warn("could not record the bootstrap", "error", err)
	}
	if s.Log != nil {
		s.Log.Info("first administrator created", "username", body.Username)
	}

	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) needsBootstrap(ctx context.Context) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	return n == 0, nil
}
