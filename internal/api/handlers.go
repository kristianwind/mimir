package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kristianwind/mimir/internal/auth"
	"github.com/kristianwind/mimir/internal/db"
	"github.com/kristianwind/mimir/internal/enka"
	"github.com/kristianwind/mimir/internal/good"
	"github.com/kristianwind/mimir/internal/model"
)

// maxGOODUpload caps a .good upload. A full inventory with 2,000 artifacts is
// well under 5 MB; anything larger is a mistake or an attack.
const maxGOODUpload = 16 << 20

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// loginWindow and loginBurst bound how often one address may guess.
//
// Failures only. Signing in correctly ten times in a morning is not an
// attack, and charging for it would lock a working password out of a shared
// office.
//
// Keyed on the address and not on the username, deliberately. A per-username
// counter stops a botnet grinding one account, but it also hands anybody a
// way to lock a named person out by failing on their behalf — the protection
// and the denial of service are the same mechanism. Ten failures in a quarter
// of an hour is generous for a household with a typo and useless for a
// script.
const (
	loginWindow = 15 * time.Minute
	loginBurst  = 10
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	addr := clientAddr(r)
	if s.logins.over(addr, time.Now()) {
		// Says nothing about whether the account exists, or whether any of
		// the attempts were close.
		writeError(w, http.StatusTooManyRequests,
			"too many sign-in attempts from here — wait a few minutes", "")
		return
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		// Code is a TOTP code or a recovery code. Absent on the first
		// attempt: the client does not know whether one is wanted until it
		// is told, because asking every account would say which ones have a
		// factor to anyone who can type a username.
		Code string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request", "")
		return
	}

	token, user, err := s.Auth.Login(r.Context(), body.Username, body.Password, body.Code, r.UserAgent())
	switch {
	case err == nil:
	case errors.Is(err, auth.ErrInvalidCredentials):
		s.failedLogin(r, addr, body.Username, "credentials")
		writeError(w, http.StatusUnauthorized, "wrong username or password", "")
		return
	case errors.Is(err, auth.ErrSecondFactorRequired):
		// Not an error the reader caused. The password was right; the form
		// grows a field.
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error":        "a code from your authenticator app is needed",
			"secondFactor": true,
		})
		return
	case errors.Is(err, auth.ErrSecondFactorInvalid):
		// A wrong code is a guess and counts. A code being *required* does
		// not: the password was right and the form is simply growing a
		// field, which is the normal path and not an attempt at anything.
		s.failedLogin(r, addr, body.Username, "second factor")
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error":        "that code is not right, or has already been used",
			"secondFactor": true,
		})
		return
	default:
		s.Log.Error("login failed", "error", err)
		writeError(w, http.StatusInternalServerError, "login failed", "")
		return
	}

	s.Auth.SetCookie(w, token)
	// Recorded like the passkey path already was. Without this the audit
	// trail showed passkey sign-ins and nothing else, which reads as though
	// nobody uses a password.
	s.audit(r, "user.login", user.Username, nil)
	writeJSON(w, http.StatusOK, user)
}

// failedLogin counts the attempt and leaves a trace of it.
//
// Both halves matter and neither is enough alone: a limit with no record
// stops the attack you are having and tells you nothing about it afterwards,
// and a record with no limit is an audit trail of a door being kicked in.
//
// The attempted username is stored because a failure without one is
// unreadable — "someone failed to sign in" is not an investigation. The known
// hazard is somebody typing a password into the username field; the audit log
// is administrator-only, which bounds that rather than solving it.
func (s *Server) failedLogin(r *http.Request, addr, username, why string) {
	s.logins.record(addr, time.Now())
	s.audit(r, "user.login.failed", username, map[string]any{"reason": why})
	if s.Log != nil {
		s.Log.Warn("sign-in refused", "username", username, "reason", why, "from", addr)
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.CookieName); err == nil {
		_ = s.Auth.Logout(r.Context(), c.Value)
	}
	s.Auth.ClearCookie(w)
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var theme, mode string
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT theme, theme_mode FROM users WHERE id = ?`, u.ID).Scan(&theme, &mode)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": u.ID, "username": u.Username, "role": u.Role,
		"theme": theme, "themeMode": mode,
	})
}

// handlePrefs stores the presentation preferences that follow the account
// rather than the browser: element theme and light/dark mode.
//
// One endpoint for both because they are one decision from the user's side —
// "how should this look" — and splitting them would mean two round trips to
// render the same page correctly on a new device.
func (s *Server) handleSetPrefs(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var body struct {
		Theme string `json:"theme"`
		Mode  string `json:"themeMode"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request", "")
		return
	}
	if !validTheme(body.Theme) {
		writeError(w, http.StatusBadRequest, "unknown theme", "Pick one of the seven elements.")
		return
	}
	if body.Mode != "light" && body.Mode != "dark" && body.Mode != "system" {
		body.Mode = "system"
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE users SET theme = ?, theme_mode = ? WHERE id = ?`,
		body.Theme, body.Mode, u.ID); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"theme": body.Theme, "themeMode": body.Mode,
	})
}

func validTheme(theme string) bool {
	for _, e := range model.Elements {
		if string(e) == theme {
			return true
		}
	}
	return false
}

func (s *Server) handleGameDataStatus(w http.ResponseWriter, r *http.Request) {
	versions, err := s.GameData.Versions()
	if err != nil {
		writeDomainError(w, err)
		return
	}
	snap, err := s.GameData.Current()
	resp := map[string]any{"versions": versions, "synced": err == nil}
	if err == nil {
		resp["active"] = snap.Version
		resp["characters"] = len(snap.Characters)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------- accounts

type accountKey struct{}

// requireAccount loads the account named in the URL and checks it belongs to
// the caller. Ownership is enforced here rather than in each handler so a new
// endpoint cannot forget it.
func (s *Server) requireAccount(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := auth.FromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "accountID"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid account id", "")
			return
		}

		var a model.Account
		err = s.DB.QueryRowContext(r.Context(),
			`SELECT id, user_id, uid, nickname, region, ar_level, wl_level
			 FROM accounts WHERE id = ? AND user_id = ?`, id, u.ID,
		).Scan(&a.ID, &a.UserID, &a.UID, &a.Nickname, &a.Region, &a.ARLevel, &a.WLLevel)
		if errors.Is(err, sql.ErrNoRows) {
			// Not "forbidden": a user must not learn that an account id
			// exists on somebody else's profile.
			writeError(w, http.StatusNotFound, "the account does not exist", "")
			return
		}
		if err != nil {
			writeDomainError(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), accountKey{}, a)))
	})
}

func accountFrom(ctx context.Context) model.Account {
	a, _ := ctx.Value(accountKey{}).(model.Account)
	return a
}

func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT id, user_id, uid, nickname, region, ar_level, wl_level
		 FROM accounts WHERE user_id = ? ORDER BY created_at`, u.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	defer rows.Close()

	out := []model.Account{}
	for rows.Next() {
		var a model.Account
		if err := rows.Scan(&a.ID, &a.UserID, &a.UID, &a.Nickname, &a.Region, &a.ARLevel, &a.WLLevel); err != nil {
			writeDomainError(w, err)
			return
		}
		out = append(out, a)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var body struct {
		UID string `json:"uid"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request", "")
		return
	}
	if err := enka.ValidateUID(body.UID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid UID",
			"The UID is at the bottom right in the game and is nine digits.")
		return
	}

	res, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO accounts (user_id, uid, region) VALUES (?, ?, ?)
		 ON CONFLICT(user_id, uid) DO UPDATE SET updated_at = datetime('now')`,
		u.ID, body.UID, enka.Region(body.UID))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusCreated, model.Account{
		ID: id, UserID: u.ID, UID: body.UID, Region: enka.Region(body.UID),
	})
}

func (s *Server) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, accountFrom(r.Context()))
}

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM accounts WHERE id = ?`, a.ID); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

// ---------------------------------------------------------------- import

func (s *Server) handleImportEnka(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	a := accountFrom(r.Context())

	snap, err := s.GameData.Current()
	if err != nil {
		writeDomainError(w, err)
		return
	}

	fetched, err := s.Enka.Fetch(r.Context(), a.UID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	res := fetched.Import(u.ID, snap)

	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(r.Context(),
		`UPDATE accounts SET nickname = ?, region = ?, ar_level = ?, wl_level = ?, updated_at = datetime('now')
		 WHERE id = ?`,
		res.Account.Nickname, res.Account.Region, res.Account.ARLevel, res.Account.WLLevel, a.ID); err != nil {
		writeDomainError(w, err)
		return
	}
	if err := upsertCharacters(r.Context(), tx, a.ID, res.Characters); err != nil {
		writeDomainError(w, err)
		return
	}
	if err := upsertEquippedWeapons(r.Context(), tx, a.ID, res.Weapons); err != nil {
		writeDomainError(w, err)
		return
	}
	stats, err := db.UpsertArtifacts(tx, a.ID, res.Artifacts)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"source":        "enka",
		"weapons":       len(res.Weapons),
		"characters":    len(res.Characters),
		"artifacts":     stats,
		"warnings":      res.Warnings,
		"fromCache":     fetched.FromCache,
		"stale":         fetched.Stale,
		"refreshableAt": fetched.RefreshableAt,
	})
}

func (s *Server) handleImportGOOD(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())

	f, err := good.Parse(http.MaxBytesReader(w, r.Body, maxGOODUpload))
	if err != nil {
		// The remedy differs by cause, and "export a .good file" is unhelpful
		// advice to somebody who just did.
		hint := "Export a .good file from Inventory Kamera or Genshin Optimizer."
		switch {
		case errors.Is(err, good.ErrTooNew):
			hint = "Your exporter is ahead of Mimir. Nothing is wrong with the file — Mimir will not import a format it has not been checked against, because a renamed stat key would arrive as a silently wrong inventory. Update Mimir, or say so and it gets checked."
		case errors.Is(err, good.ErrTooOld):
			hint = "That file predates the current slot and stat names. Re-export it from a current Inventory Kamera or Genshin Optimizer."
		}
		writeError(w, http.StatusBadRequest, err.Error(), hint)
		return
	}
	chars, weapons, arts := f.Import(a.ID)

	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	defer tx.Rollback()

	if err := upsertCharacters(r.Context(), tx, a.ID, chars); err != nil {
		writeDomainError(w, err)
		return
	}
	if err := upsertWeapons(r.Context(), tx, a.ID, weapons); err != nil {
		writeDomainError(w, err)
		return
	}
	stats, err := db.UpsertArtifacts(tx, a.ID, arts)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"source":     "good",
		"scanner":    f.Source,
		"characters": len(chars),
		"weapons":    len(weapons),
		"artifacts":  stats,
	})
}

func upsertCharacters(ctx context.Context, tx *sql.Tx, accountID int64, chars []model.Character) error {
	for _, c := range chars {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO characters
				(account_id, char_key, level, ascension, constellation,
				 talent_auto, talent_skill, talent_burst, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(account_id, char_key) DO UPDATE SET
				level = excluded.level,
				ascension = excluded.ascension,
				constellation = excluded.constellation,
				talent_auto = excluded.talent_auto,
				talent_skill = excluded.talent_skill,
				talent_burst = excluded.talent_burst,
				source = excluded.source,
				updated_at = datetime('now')`,
			accountID, c.Key, c.Level, c.Ascension, c.Constellation,
			c.TalentAuto, c.TalentSkill, c.TalentBurst, c.Source,
		); err != nil {
			return fmt.Errorf("upsert character %s: %w", c.Key, err)
		}
	}
	return nil
}

// upsertEquippedWeapons merges the handful of weapons a showcase carries.
//
// It cannot use the wholesale replace below: a showcase holds eight equipped
// weapons, and replacing on that basis would delete a full inventory imported
// from a .good file minutes earlier. Instead it replaces only what is
// equipped on the characters it actually saw.
func upsertEquippedWeapons(ctx context.Context, tx *sql.Tx, accountID int64, weapons []model.Weapon) error {
	for _, wp := range weapons {
		if wp.Location == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM weapons WHERE account_id = ? AND location = ?`, accountID, wp.Location); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO weapons (account_id, weapon_key, level, ascension, refinement, location, locked, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			accountID, wp.Key, wp.Level, wp.Ascension, wp.Refinement, wp.Location, wp.Lock, wp.Source,
		); err != nil {
			return fmt.Errorf("insert weapon %s: %w", wp.Key, err)
		}
	}
	return nil
}

// upsertWeapons replaces the account's weapons wholesale. Weapons carry no
// stable identity across a scan — two R1 Favonius Swords at level 90 are
// indistinguishable — so incremental matching would invent distinctions that
// do not exist.
func upsertWeapons(ctx context.Context, tx *sql.Tx, accountID int64, weapons []model.Weapon) error {
	if len(weapons) == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM weapons WHERE account_id = ?`, accountID); err != nil {
		return err
	}
	for _, wp := range weapons {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO weapons (account_id, weapon_key, level, ascension, refinement, location, locked, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			accountID, wp.Key, wp.Level, wp.Ascension, wp.Refinement, wp.Location, wp.Lock, wp.Source,
		); err != nil {
			return fmt.Errorf("insert weapon %s: %w", wp.Key, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------- inventory

func (s *Server) handleListCharacters(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT id, char_key, level, ascension, constellation,
		       talent_auto, talent_skill, talent_burst, source
		FROM characters WHERE account_id = ? ORDER BY char_key`, a.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	defer rows.Close()

	out := []model.Character{}
	for rows.Next() {
		c := model.Character{AccountID: a.ID}
		if err := rows.Scan(&c.ID, &c.Key, &c.Level, &c.Ascension, &c.Constellation,
			&c.TalentAuto, &c.TalentSkill, &c.TalentBurst, &c.Source); err != nil {
			writeDomainError(w, err)
			return
		}
		out = append(out, c)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListArtifacts(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())
	arts, err := db.LoadArtifacts(s.DB, a.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if arts == nil {
		arts = []model.Artifact{}
	}
	writeJSON(w, http.StatusOK, arts)
}
