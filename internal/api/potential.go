package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/kristianwind/mimir/internal/advisor"
	"github.com/kristianwind/mimir/internal/db"
	"github.com/kristianwind/mimir/internal/model"
)

// The potential view.
//
// The plan answers "what should I spend tomorrow's resin on" and needs a goal
// with a rotation to answer it. This answers the question before that one —
// "which of these characters is worth investing in at all" — for the whole
// roster, including everybody who has no goal and is therefore invisible to
// the plan.
//
// See internal/advisor/potential.go for what the yardstick measures and, more
// importantly, what it does not.

// handlePotential ranks every owned character.
func (s *Server) handlePotential(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())

	reqs, err := s.potentialRequests(r.Context(), a.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if len(reqs) == 0 {
		writeError(w, http.StatusUnprocessableEntity,
			"none of the characters have anything equipped",
			"Import from Enka or upload a .good file. A character with no artifacts has nothing to measure.")
		return
	}

	ranking, err := advisor.AccountPotential(r.Context(), reqs)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ranking)
}

// potentialRequests assembles one measurement input per owned character.
//
// A character with nothing equipped is left out rather than scored at zero: an
// empty build is not a weak build, it is an absence of data, and ranking it
// last would read as advice.
func (s *Server) potentialRequests(ctx context.Context, accountID int64) ([]advisor.PotentialRequest, error) {
	snap, err := s.GameData.Current()
	if err != nil {
		return nil, err
	}
	characters, err := s.loadCharacters(ctx, accountID)
	if err != nil {
		return nil, err
	}
	inventory, err := db.LoadArtifacts(s.DB, accountID)
	if err != nil {
		return nil, err
	}
	weapons, err := s.loadWeapons(ctx, accountID)
	if err != nil {
		return nil, err
	}

	equipped := map[string][]model.Artifact{}
	for _, art := range inventory {
		if art.Location != "" {
			equipped[art.Location] = append(equipped[art.Location], art)
		}
	}

	var out []advisor.PotentialRequest
	for _, c := range characters {
		pieces := equipped[c.Key]
		if len(pieces) == 0 {
			continue
		}
		weapon, err := s.loadEquippedWeapon(ctx, accountID, c.Key)
		if err != nil {
			return nil, err
		}

		// A goal's declared conditions are reused when there is one, so the
		// two views do not disagree about the same build. Without a goal the
		// conditions are simply unanswered, which the assessment reports.
		var conditions map[string]float64
		if goal, _, _, err := s.loadGoal(ctx, accountID, c.Key); err == nil {
			conditions = goal.Conditions
		}

		out = append(out, advisor.PotentialRequest{
			Snapshot: snap,
			Loadout: advisor.Loadout{
				Character: c,
				Weapon:    weapon,
				Artifacts: pieces,
			},
			Inventory:     inventory,
			Weapons:       weapons,
			Conditions:    conditions,
			MaxSetConfigs: 8,
		})
	}
	return out, nil
}

type deriveRequest struct {
	// Characters names who to create goals for. Empty means everybody the
	// ranking could measure.
	Characters []string `json:"characters,omitempty"`
	// Limit caps how many goals are created, counting from the top of the
	// ranking. Zero means no cap.
	Limit int `json:"limit,omitempty"`
}

type derivedGoal struct {
	Character string `json:"character"`
	Priority  int    `json:"priority"`
	Rotation  string `json:"rotation"`
	Created   bool   `json:"created"`
	Reason    string `json:"reason,omitempty"`
}

// handleDeriveGoals turns the ranking into goals.
//
// Two rules, both about not lying:
//
//   - A goal the player wrote is never touched. Their rotation is the one
//     thing here that is not a guess.
//   - What is created is marked derived, and stays marked. Every gain in the
//     plan is measured against the rotation, so a guessed one is wrong all the
//     way down — and a guess that cannot be told apart from an authored one is
//     the version of that nobody catches.
func (s *Server) handleDeriveGoals(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())

	var body deriveRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "malformed request", "")
			return
		}
	}

	snap, err := s.GameData.Current()
	if err != nil {
		writeDomainError(w, err)
		return
	}
	reqs, err := s.potentialRequests(r.Context(), a.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	ranking, err := advisor.AccountPotential(r.Context(), reqs)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	wanted := map[string]bool{}
	for _, key := range body.Characters {
		wanted[key] = true
	}

	existing, err := s.goalSources(r.Context(), a.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	out := []derivedGoal{}
	created := 0
	// Priority descends with the ranking, so the account plan resolves gear
	// contention in the same order the ranking put them in.
	priority := len(ranking.Characters)

	for _, c := range ranking.Characters {
		priority--
		if len(wanted) > 0 && !wanted[c.Character] {
			continue
		}
		if body.Limit > 0 && created >= body.Limit {
			break
		}
		if source, ok := existing[c.Character]; ok && source != "derived" {
			out = append(out, derivedGoal{
				Character: c.Character,
				Reason:    "you wrote this goal yourself; it was left alone",
			})
			continue
		}

		spec, err := advisor.DeriveSpec(snap, characterOf(reqs, c.Character))
		if err != nil {
			out = append(out, derivedGoal{Character: c.Character, Reason: err.Error()})
			continue
		}
		if err := s.saveDerivedGoal(r.Context(), a.ID, c.Character, priority, spec); err != nil {
			writeDomainError(w, err)
			return
		}
		created++
		out = append(out, derivedGoal{
			Character: c.Character,
			Priority:  priority,
			Rotation:  specSummary(spec),
			Created:   true,
		})
	}

	s.audit(r, "goals.derive", fmt.Sprintf("account %d", a.ID),
		map[string]any{"created": created})

	writeJSON(w, http.StatusOK, map[string]any{
		"goals":   out,
		"created": created,
		"note": "These rotations are Mimir's, not yours: one cast of the elemental skill and one of the burst. " +
			"Every number the plan reports is measured against them, so open each goal and say what you actually press.",
	})
}

func characterOf(reqs []advisor.PotentialRequest, key string) model.Character {
	for _, r := range reqs {
		if r.Loadout.Character.Key == key {
			return r.Loadout.Character
		}
	}
	return model.Character{Key: key}
}

func specSummary(spec advisor.Spec) string {
	out := ""
	for i, s := range spec.Steps {
		if i > 0 {
			out += " + "
		}
		out += s.Talent + " " + s.Entry
	}
	return out
}

// goalSources reports which characters already have a goal, and whether the
// player wrote it.
func (s *Server) goalSources(ctx context.Context, accountID int64) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT char_key, source FROM goals WHERE account_id = ?`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var key, source string
		if err := rows.Scan(&key, &source); err != nil {
			return nil, err
		}
		out[key] = source
	}
	return out, rows.Err()
}

func (s *Server) saveDerivedGoal(
	ctx context.Context, accountID int64, key string, priority int, spec advisor.Spec,
) error {
	rotation, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	target, err := json.Marshal(defaultTarget())
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO goals (account_id, char_key, priority, team, rotation, target, conditions, source, notes)
		VALUES (?, ?, ?, '[]', ?, ?, '{}', 'derived', ?)
		ON CONFLICT(account_id, char_key) DO UPDATE SET
			priority = excluded.priority,
			rotation = excluded.rotation,
			source   = 'derived'`,
		accountID, key, priority, string(rotation), string(target),
		"Derived by Mimir from the potential ranking. Replace the rotation with how you actually play.")
	return err
}
