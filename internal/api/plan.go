package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/kristianwind/mimir/internal/advisor"
	"github.com/kristianwind/mimir/internal/calc"
	"github.com/kristianwind/mimir/internal/db"
	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
	"github.com/kristianwind/mimir/internal/optimizer"
)

// goalPayload is a goal as the frontend sends and receives it.
type goalPayload struct {
	CharacterKey string             `json:"characterKey"`
	Priority     int                `json:"priority"`
	Team         []string           `json:"team"`
	Spec         advisor.Spec       `json:"rotation"`
	Target       calc.Target        `json:"target"`
	Buffs        model.StatBlock    `json:"buffs,omitempty"`
	Conditions   map[string]float64 `json:"conditions,omitempty"`
	Constraints  []constraint       `json:"constraints,omitempty"`
	Notes        string             `json:"notes,omitempty"`
	// Source is "manual" when the player wrote the rotation and "derived"
	// when Mimir did. Read-only from the client's side: saving a goal makes
	// it the player's, which is the whole point of opening it.
	Source string `json:"source,omitempty"`
}

type constraint struct {
	Stat model.Stat `json:"stat"`
	Min  float64    `json:"min"`
}

// defaultTarget is what a gain figure is measured against when a goal does
// not say. It is stated in the API response rather than hidden, because a DPS
// number without a named enemy is not comparable to anything.
func defaultTarget() calc.Target {
	return calc.Target{
		Level: 100,
		Resistance: map[model.Element]float64{
			model.Pyro: 0.10, model.Hydro: 0.10, model.Anemo: 0.10,
			model.Electro: 0.10, model.Dendro: 0.10, model.Cryo: 0.10,
			model.Geo: 0.10, model.Physical: 0.10,
		},
	}
}

func (s *Server) handleListGoals(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT char_key, priority, team, rotation, target, conditions, notes, source FROM goals
		 WHERE account_id = ? ORDER BY priority DESC, char_key`, a.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	defer rows.Close()

	out := []goalPayload{}
	for rows.Next() {
		var (
			g                                  goalPayload
			team, rotation, target, conditions string
		)
		if err := rows.Scan(&g.CharacterKey, &g.Priority, &team, &rotation, &target,
			&conditions, &g.Notes, &g.Source); err != nil {
			writeDomainError(w, err)
			return
		}
		_ = json.Unmarshal([]byte(team), &g.Team)
		_ = json.Unmarshal([]byte(rotation), &g.Spec)
		_ = json.Unmarshal([]byte(target), &g.Target)
		_ = json.Unmarshal([]byte(conditions), &g.Conditions)
		out = append(out, g)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSaveGoal(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())

	var g goalPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&g); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request", "")
		return
	}
	if g.CharacterKey == "" {
		writeError(w, http.StatusBadRequest, "the goal is missing a character", "")
		return
	}
	if len(g.Spec.Steps) == 0 {
		writeError(w, http.StatusBadRequest, "the goal is missing a rotation",
			"A ranking without a rotation is meaningless: a gain on a burst you never press is no gain.")
		return
	}

	snap, err := s.GameData.Current()
	if err != nil {
		writeDomainError(w, err)
		return
	}
	// Validate the rotation against the character's real talent tables now,
	// so a typo in a step label is caught while the user is still looking at
	// the form rather than when the plan silently comes back empty.
	if _, err := advisor.BuildRotation(snap, model.Character{
		Key: g.CharacterKey, Level: 90, TalentAuto: 1, TalentSkill: 1, TalentBurst: 1,
	}, g.Spec); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(),
			"Use one of the labels from the character's talent table.")
		return
	}

	team, _ := json.Marshal(g.Team)
	rotation, _ := json.Marshal(g.Spec)
	if g.Target.Level == 0 {
		g.Target = defaultTarget()
	}
	target, _ := json.Marshal(g.Target)
	if g.Conditions == nil {
		g.Conditions = map[string]float64{}
	}
	conditions, _ := json.Marshal(g.Conditions)

	if _, err := s.DB.ExecContext(r.Context(), `
		INSERT INTO goals (account_id, char_key, priority, team, rotation, target, conditions, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id, char_key) DO UPDATE SET
			priority = excluded.priority,
			team = excluded.team,
			rotation = excluded.rotation,
			target = excluded.target,
			conditions = excluded.conditions,
			notes = excluded.notes,
			-- Saving is the player taking ownership: a rotation they have
			-- looked at and kept is theirs, derived or not.
			source = 'manual'`,
		a.ID, g.CharacterKey, g.Priority, string(team), string(rotation),
		string(target), string(conditions), g.Notes,
	); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleDeleteGoal(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())
	key := chi.URLParam(r, "characterKey")
	if _, err := s.DB.ExecContext(r.Context(),
		`DELETE FROM goals WHERE account_id = ? AND char_key = ?`, a.ID, key); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

// handlePlanForGoal ranks every upgrade for one goal.
func (s *Server) handlePlanForGoal(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())
	key := chi.URLParam(r, "characterKey")

	snap, err := s.GameData.Current()
	if err != nil {
		writeDomainError(w, err)
		return
	}
	inventory, err := db.LoadArtifacts(s.DB, a.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	weapons, err := s.loadWeapons(r.Context(), a.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	req, err := s.planRequest(r.Context(), a.ID, key, snap, inventory, weapons, farmSim(snap, inventory))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound,
				fmt.Sprintf("no goal has been set up for %s", key),
				"Create a goal with a rotation, and the plan can be calculated.")
			return
		}
		writeError(w, http.StatusUnprocessableEntity, key+": "+err.Error(),
			"Import from Enka or upload a .good file, and equip the character in the game.")
		return
	}

	plan, err := advisor.BuildPlan(r.Context(), req)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"plan":   plan,
		"target": req.Goal.Target,
	})
}

// handleAccountPlan ranks every goal on the account in one list.
//
// This is the question the player actually has — "what should I do tomorrow" —
// and the only place the contention between goals gets resolved rather than
// presented twice from opposite sides.
func (s *Server) handleAccountPlan(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())

	snap, err := s.GameData.Current()
	if err != nil {
		writeDomainError(w, err)
		return
	}

	keys, err := s.goalKeys(r.Context(), a.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if len(keys) == 0 {
		writeError(w, http.StatusNotFound, "no goals have been set up",
			"Create at least one goal with a rotation, and the plan can be calculated.")
		return
	}

	inventory, err := db.LoadArtifacts(s.DB, a.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	weapons, err := s.loadWeapons(r.Context(), a.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	sim := farmSim(snap, inventory)

	var reqs []advisor.Request
	skipped := []string{}
	for _, key := range keys {
		req, err := s.planRequest(r.Context(), a.ID, key, snap, inventory, weapons, sim)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %v", key, err))
			continue
		}
		reqs = append(reqs, req)
	}
	if len(reqs) == 0 {
		writeError(w, http.StatusUnprocessableEntity,
			"none of the goals could be calculated", joinLines(skipped))
		return
	}

	plan, err := advisor.BuildAccountPlan(r.Context(), reqs)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if derived, err := s.derivedGoals(r.Context(), a.ID); err == nil && len(derived) > 0 {
		plan.Caveats = append(plan.Caveats, fmt.Sprintf(
			"%s: the rotation was derived by Mimir, not written by you — one cast of the skill and one of the burst. "+
				"Every gain below for those goals is measured against that, so open them and say what you actually press.",
			strings.Join(derived, ", ")))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"plan":    plan,
		"skipped": skipped,
	})
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "; "
		}
		out += l
	}
	return out
}

func (s *Server) goalKeys(ctx context.Context, accountID int64) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT char_key FROM goals WHERE account_id = ? ORDER BY priority DESC, char_key`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

// planRequest assembles everything one goal needs to be ranked.
func (s *Server) planRequest(
	ctx context.Context,
	accountID int64,
	key string,
	snap *gamedata.Snapshot,
	inventory []model.Artifact,
	weapons []model.Weapon,
	sim *advisor.FarmSim,
) (advisor.Request, error) {
	goal, buffs, constraints, err := s.loadGoal(ctx, accountID, key)
	if err != nil {
		return advisor.Request{}, fmt.Errorf("the goal could not be read: %w", err)
	}
	character, err := s.loadCharacter(ctx, accountID, key)
	if err != nil {
		return advisor.Request{}, fmt.Errorf("the character is not on the account")
	}
	weapon, err := s.loadEquippedWeapon(ctx, accountID, key)
	if err != nil {
		return advisor.Request{}, err
	}

	var equipped []model.Artifact
	for _, art := range inventory {
		if art.Location == key {
			equipped = append(equipped, art)
		}
	}
	if len(equipped) == 0 {
		return advisor.Request{}, fmt.Errorf("has no artifacts equipped")
	}

	return advisor.Request{
		Snapshot: snap,
		Goal:     goal,
		Loadout: advisor.Loadout{
			Character: character,
			Weapon:    weapon,
			Artifacts: equipped,
			Buffs:     buffs,
		},
		Inventory:     inventory,
		Weapons:       weapons,
		Constraints:   constraints,
		MaxSetConfigs: 8,
		FarmDays:      7,
		ResinPerDay:   180,
		Sim:           sim,
	}, nil
}

// handleBuildSheet resolves one character's current build and shows where
// every effect-derived number came from.
func (s *Server) handleBuildSheet(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())
	key := chi.URLParam(r, "characterKey")

	snap, err := s.GameData.Current()
	if err != nil {
		writeDomainError(w, err)
		return
	}
	character, err := s.loadCharacter(r.Context(), a.ID, key)
	if err != nil {
		writeError(w, http.StatusNotFound,
			fmt.Sprintf("%s is not on the account", key), "")
		return
	}
	weapon, err := s.loadEquippedWeapon(r.Context(), a.ID, key)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	inventory, err := db.LoadArtifacts(s.DB, a.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	var equipped []model.Artifact
	for _, art := range inventory {
		if art.Location == key {
			equipped = append(equipped, art)
		}
	}

	var conditions map[string]float64
	if goal, _, _, err := s.loadGoal(r.Context(), a.ID, key); err == nil {
		conditions = goal.Conditions
	}

	state, err := advisor.Assemble(snap, advisor.Loadout{
		Character: character, Weapon: weapon, Artifacts: equipped,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	sheet, err := advisor.BuildSheet(snap, state, conditions)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	weaponKey := ""
	if weapon != nil {
		weaponKey = weapon.Key
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"character": character,
		"weapon":    weaponKey,
		"sheet":     sheet,
	})
}

func (s *Server) handleListWeapons(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())
	weapons, err := s.loadWeapons(r.Context(), a.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if weapons == nil {
		weapons = []model.Weapon{}
	}
	writeJSON(w, http.StatusOK, weapons)
}

// handleTalentTable exposes a character's mined talent rows.
//
// This is what makes rotations authorable instead of guessable: the editor
// offers the real labels and their multipliers at the character's actual
// talent levels, so a step names something that exists.
func (s *Server) handleTalentTable(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())
	key := chi.URLParam(r, "characterKey")

	snap, err := s.GameData.Current()
	if err != nil {
		writeDomainError(w, err)
		return
	}
	def, err := snap.Char(key)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	// Talent levels come from the account when the character is owned, so
	// the multipliers shown are the ones that will actually be used —
	// including the three levels a constellation adds, which is the
	// difference between showing a level 9 skill and the level 12 that
	// actually fires.
	levels := map[string]int{"auto": 1, "skill": 1, "burst": 1}
	base := map[string]int{"auto": 1, "skill": 1, "burst": 1}
	if c, err := s.loadCharacter(r.Context(), a.ID, key); err == nil {
		base["auto"], base["skill"], base["burst"] = c.TalentAuto, c.TalentSkill, c.TalentBurst
		for slot := range levels {
			levels[slot] = advisor.EffectiveTalentLevel(def, c, slot)
		}
	}

	type entry struct {
		Label    string     `json:"label"`
		Unit     string     `json:"unit"`
		Scaling  model.Stat `json:"scaling"`
		IsDamage bool       `json:"isDamage"`
		AtLevel  float64    `json:"atLevel"`
		MaxLevel int        `json:"maxLevel"`
	}
	out := map[string]any{
		"character":  key,
		"element":    def.Element,
		"levels":     levels,
		"baseLevels": base,
	}
	talents := map[string]any{}
	for slot, talent := range def.Talents {
		entries := make([]entry, 0, len(talent.Entries))
		for _, e := range talent.Entries {
			value, err := e.Multiplier(levels[slot])
			if err != nil {
				continue
			}
			entries = append(entries, entry{
				Label:    e.Label,
				Unit:     e.Unit,
				Scaling:  e.Scaling,
				IsDamage: e.IsDamage(),
				AtLevel:  value,
				MaxLevel: len(e.Multipliers),
			})
		}
		talents[slot] = map[string]any{"name": talent.Name, "entries": entries}
	}
	out["talents"] = talents
	writeJSON(w, http.StatusOK, out)
}

// farmSim returns a simulator driven by the account's own inventory.
//
// The drop distributions are measured per account rather than shipped as game
// data, so this returns nil — and the plan says why — until the inventory is
// large enough to measure from. A showcase of forty artifacts is not.
func farmSim(snap *gamedata.Snapshot, inventory []model.Artifact) *advisor.FarmSim {
	est, err := advisor.EstimateDropModel(inventory)
	if err != nil {
		if snap.DropModel.PiecesPerRun > 0 {
			// A properly sourced model in the snapshot still wins.
			return &advisor.FarmSim{Snapshot: snap, Trials: 200, Seed: 1}
		}
		return nil
	}
	dm := est.Model
	// A supplied per-run yield lets the estimate be priced in resin; without
	// it the plan ranks farming in artifacts examined.
	if snap.DropModel.PiecesPerRun > 0 {
		dm.PiecesPerRun = snap.DropModel.PiecesPerRun
		dm.FiveStarChance = snap.DropModel.FiveStarChance
	}
	return &advisor.FarmSim{Snapshot: snap, DropModel: &dm, Trials: 200, Seed: 1}
}

// handleDropModel exposes the measured drop model and its caveats, so the
// numbers behind a farming recommendation are inspectable rather than
// something the user has to take on faith.
func (s *Server) handleDropModel(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())
	inventory, err := db.LoadArtifacts(s.DB, a.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	est, err := advisor.EstimateDropModel(inventory)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(),
			"Upload a .good file from Inventory Kamera, and the model is measured on your whole inventory.")
		return
	}
	writeJSON(w, http.StatusOK, est)
}

func (s *Server) loadGoal(ctx context.Context, accountID int64, key string) (
	advisor.Goal, model.StatBlock, []optimizer.Constraint, error,
) {
	var rotation, target, conditions string
	var priority int
	err := s.DB.QueryRowContext(ctx,
		`SELECT priority, rotation, target, conditions FROM goals WHERE account_id = ? AND char_key = ?`,
		accountID, key).Scan(&priority, &rotation, &target, &conditions)
	if err != nil {
		return advisor.Goal{}, nil, nil, err
	}

	goal := advisor.Goal{CharacterKey: key, Priority: priority, Target: defaultTarget()}
	if err := json.Unmarshal([]byte(rotation), &goal.Spec); err != nil {
		return advisor.Goal{}, nil, nil, fmt.Errorf("goal %s: %w", key, err)
	}
	if target != "" && target != "{}" {
		if err := json.Unmarshal([]byte(target), &goal.Target); err != nil {
			return advisor.Goal{}, nil, nil, fmt.Errorf("goal %s target: %w", key, err)
		}
	}
	if conditions != "" {
		if err := json.Unmarshal([]byte(conditions), &goal.Conditions); err != nil {
			return advisor.Goal{}, nil, nil, fmt.Errorf("goal %s conditions: %w", key, err)
		}
	}
	return goal, nil, nil, nil
}

func (s *Server) loadCharacter(ctx context.Context, accountID int64, key string) (model.Character, error) {
	c := model.Character{AccountID: accountID, Key: key}
	err := s.DB.QueryRowContext(ctx, `
		SELECT level, ascension, constellation, talent_auto, talent_skill, talent_burst
		FROM characters WHERE account_id = ? AND char_key = ?`, accountID, key).
		Scan(&c.Level, &c.Ascension, &c.Constellation, &c.TalentAuto, &c.TalentSkill, &c.TalentBurst)
	return c, err
}

func (s *Server) loadEquippedWeapon(ctx context.Context, accountID int64, key string) (*model.Weapon, error) {
	var wp model.Weapon
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, weapon_key, level, ascension, refinement, location
		FROM weapons WHERE account_id = ? AND location = ? LIMIT 1`, accountID, key).
		Scan(&wp.ID, &wp.Key, &wp.Level, &wp.Ascension, &wp.Refinement, &wp.Location)
	if errors.Is(err, sql.ErrNoRows) {
		// A character with no weapon is a valid state to plan for; the
		// baseline simply has no weapon ATK in it.
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	wp.AccountID = accountID
	return &wp, nil
}

func (s *Server) loadWeapons(ctx context.Context, accountID int64) ([]model.Weapon, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, weapon_key, level, ascension, refinement, location
		FROM weapons WHERE account_id = ?`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Weapon
	for rows.Next() {
		wp := model.Weapon{AccountID: accountID}
		if err := rows.Scan(&wp.ID, &wp.Key, &wp.Level, &wp.Ascension, &wp.Refinement, &wp.Location); err != nil {
			return nil, err
		}
		out = append(out, wp)
	}
	return out, rows.Err()
}

// derivedGoals names the goals whose rotation Mimir guessed.
func (s *Server) derivedGoals(ctx context.Context, accountID int64) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT char_key FROM goals WHERE account_id = ? AND source = 'derived' ORDER BY char_key`,
		accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}
