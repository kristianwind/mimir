package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

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

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "ugyldig forespørgsel", "")
		return
	}

	token, user, err := s.Auth.Login(r.Context(), body.Username, body.Password, r.UserAgent())
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "forkert brugernavn eller adgangskode", "")
			return
		}
		s.Log.Error("login failed", "error", err)
		writeError(w, http.StatusInternalServerError, "login mislykkedes", "")
		return
	}

	s.Auth.SetCookie(w, token)
	writeJSON(w, http.StatusOK, user)
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

func (s *Server) handleSetTheme(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var body struct {
		Theme string `json:"theme"`
		Mode  string `json:"themeMode"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "ugyldig forespørgsel", "")
		return
	}
	if !validTheme(body.Theme) {
		writeError(w, http.StatusBadRequest, "ukendt tema", "Vælg et af de syv elementer.")
		return
	}
	if body.Mode != "light" && body.Mode != "dark" && body.Mode != "system" {
		body.Mode = "system"
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE users SET theme = ?, theme_mode = ? WHERE id = ?`, body.Theme, body.Mode, u.ID); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"theme": body.Theme, "themeMode": body.Mode})
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
			writeError(w, http.StatusBadRequest, "ugyldigt account-id", "")
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
			writeError(w, http.StatusNotFound, "kontoen findes ikke", "")
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
		writeError(w, http.StatusBadRequest, "ugyldig forespørgsel", "")
		return
	}
	if err := enka.ValidateUID(body.UID); err != nil {
		writeError(w, http.StatusBadRequest, "ugyldigt UID",
			"UID'et står nederst til højre i spillet og er ni cifre.")
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
		writeError(w, http.StatusBadRequest, err.Error(),
			"Eksportér en .good-fil fra Inventory Kamera eller Genshin Optimizer.")
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
