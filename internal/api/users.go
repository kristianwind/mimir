package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/kristianwind/mimir/internal/auth"
)

// MinPasswordLength is the floor for a new password.
const MinPasswordLength = 12

// userRecord is a user as the API returns it. There is no password field in
// either direction beyond the write-only one on create and change — a hash
// has no business leaving the server, not even to an admin.
type userRecord struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email,omitempty"`
	Role      string `json:"role"`
	Disabled  bool   `json:"disabled"`
	CreatedAt string `json:"createdAt"`
	// Accounts is how many game accounts they have attached, so an admin can
	// see what deleting them would take with it.
	Accounts int `json:"accounts"`
	// Sessions is how many active logins they have.
	Sessions int `json:"sessions"`
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT u.id, u.username, COALESCE(u.email, ''), u.role, u.disabled, u.created_at,
		       (SELECT COUNT(*) FROM accounts a WHERE a.user_id = u.id),
		       (SELECT COUNT(*) FROM sessions se WHERE se.user_id = u.id
		          AND se.expires_at > datetime('now'))
		FROM users u ORDER BY u.created_at`)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	defer rows.Close()

	out := []userRecord{}
	for rows.Next() {
		var u userRecord
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Role, &u.Disabled,
			&u.CreatedAt, &u.Accounts, &u.Sessions); err != nil {
			writeDomainError(w, err)
			return
		}
		out = append(out, u)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var body struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "ugyldig forespørgsel", "")
		return
	}

	body.Username = strings.TrimSpace(body.Username)
	if body.Username == "" {
		writeError(w, http.StatusBadRequest, "brugernavn mangler", "")
		return
	}
	if err := checkPassword(body.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "")
		return
	}
	if body.Role != "admin" && body.Role != "user" {
		body.Role = "user"
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	res, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO users (username, email, password_hash, role) VALUES (?, ?, ?, ?)`,
		body.Username, nullIfEmpty(body.Email), hash, body.Role)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "brugernavnet er taget", "")
			return
		}
		writeDomainError(w, err)
		return
	}
	id, _ := res.LastInsertId()

	s.audit(r, "user.create", body.Username, map[string]any{"role": body.Role})
	writeJSON(w, http.StatusCreated, userRecord{
		ID: id, Username: body.Username, Email: body.Email, Role: body.Role,
	})
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ugyldigt bruger-id", "")
		return
	}

	var body struct {
		Role     *string `json:"role,omitempty"`
		Disabled *bool   `json:"disabled,omitempty"`
		Password *string `json:"password,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "ugyldig forespørgsel", "")
		return
	}

	// Anything that could remove the last administrator is checked against
	// the state *after* the change, not before. Locking everybody out of
	// their own instance is not a mistake you get to undo from the web.
	if (body.Role != nil && *body.Role != "admin") || (body.Disabled != nil && *body.Disabled) {
		ok, err := s.wouldKeepAnAdmin(r.Context(), id)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		if !ok {
			writeError(w, http.StatusConflict,
				"det ville efterlade instansen uden administratorer",
				"Gør en anden til administrator først.")
			return
		}
	}

	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	defer tx.Rollback()

	if body.Role != nil {
		if *body.Role != "admin" && *body.Role != "user" {
			writeError(w, http.StatusBadRequest, "ukendt rolle", "")
			return
		}
		if _, err := tx.ExecContext(r.Context(),
			`UPDATE users SET role = ? WHERE id = ?`, *body.Role, id); err != nil {
			writeDomainError(w, err)
			return
		}
	}
	if body.Disabled != nil {
		if _, err := tx.ExecContext(r.Context(),
			`UPDATE users SET disabled = ? WHERE id = ?`, *body.Disabled, id); err != nil {
			writeDomainError(w, err)
			return
		}
		if *body.Disabled {
			// A disabled account whose session still works is not disabled.
			if _, err := tx.ExecContext(r.Context(),
				`DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
				writeDomainError(w, err)
				return
			}
		}
	}
	if body.Password != nil {
		if err := checkPassword(*body.Password); err != nil {
			writeError(w, http.StatusBadRequest, err.Error(), "")
			return
		}
		hash, err := auth.HashPassword(*body.Password)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		if _, err := tx.ExecContext(r.Context(),
			`UPDATE users SET password_hash = ? WHERE id = ?`, hash, id); err != nil {
			writeDomainError(w, err)
			return
		}
		// A reset password with the old sessions still live protects nobody.
		if _, err := tx.ExecContext(r.Context(),
			`DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
			writeDomainError(w, err)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		writeDomainError(w, err)
		return
	}
	s.audit(r, "user.update", strconv.FormatInt(id, 10), map[string]any{
		"role": body.Role, "disabled": body.Disabled, "passwordReset": body.Password != nil,
	})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ugyldigt bruger-id", "")
		return
	}

	ok, err := s.wouldKeepAnAdmin(r.Context(), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusConflict,
			"det ville efterlade instansen uden administratorer",
			"Gør en anden til administrator først.")
		return
	}

	// Accounts, characters, artifacts and goals cascade. Say so before
	// doing it rather than after.
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM users WHERE id = ?`, id); err != nil {
		writeDomainError(w, err)
		return
	}
	s.audit(r, "user.delete", strconv.FormatInt(id, 10), nil)
	writeJSON(w, http.StatusNoContent, nil)
}

// handleChangeOwnPassword lets any user change their own, proving they know
// the current one first.
func (s *Server) handleChangeOwnPassword(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())

	var body struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "ugyldig forespørgsel", "")
		return
	}
	if err := checkPassword(body.New); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "")
		return
	}

	// Requiring the current password is what stops a borrowed session from
	// becoming a permanent one.
	var hash string
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT password_hash FROM users WHERE id = ?`, me.ID).Scan(&hash); err != nil {
		writeDomainError(w, err)
		return
	}
	ok, err := auth.VerifyPassword(body.Current, hash)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusForbidden, "den nuværende adgangskode er forkert", "")
		return
	}

	newHash, err := auth.HashPassword(body.New)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE users SET password_hash = ? WHERE id = ?`, newHash, me.ID); err != nil {
		writeDomainError(w, err)
		return
	}

	// Every other session is dropped; this one is re-issued so changing a
	// password does not log you out of the page you are standing on.
	if _, err := s.DB.ExecContext(r.Context(),
		`DELETE FROM sessions WHERE user_id = ?`, me.ID); err != nil {
		writeDomainError(w, err)
		return
	}
	token, _, err := s.Auth.Login(r.Context(), me.Username, body.New, r.UserAgent())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	s.Auth.SetCookie(w, token)

	s.audit(r, "user.password", me.Username, nil)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// wouldKeepAnAdmin reports whether at least one enabled admin remains once
// the given user stops being one.
func (s *Server) wouldKeepAnAdmin(ctx context.Context, excluding int64) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE role = 'admin' AND disabled = 0 AND id != ?`,
		excluding).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func checkPassword(p string) error {
	if len([]rune(p)) < MinPasswordLength {
		return errors.New("adgangskoden skal være mindst " +
			strconv.Itoa(MinPasswordLength) + " tegn")
	}
	return nil
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique")
}
