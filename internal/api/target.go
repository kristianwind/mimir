package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kristianwind/mimir/internal/advisor"
	"github.com/kristianwind/mimir/internal/db"
	"github.com/kristianwind/mimir/internal/model"
)

// What to farm towards.
//
// The rest of Mimir answers questions about the bag: what to re-equip, what
// to level, who is worth building. This answers the one people currently take
// to a wiki — which set, which main stats, which weapon does this character
// want — and it answers it by computing, not by repeating somebody's opinion.
//
// That is the whole argument for having it. A wiki gives one recommendation
// to every player who looks the character up. This one runs against this
// character's constellation, talent levels and rotation, shows its numbers,
// and can therefore be checked and can disagree for a reason.

func (s *Server) handleTarget(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())
	key := chi.URLParam(r, "characterKey")

	snap, err := s.GameData.Current()
	if err != nil {
		writeDomainError(w, err)
		return
	}

	characters, err := s.loadCharacters(r.Context(), a.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	character := model.Character{Key: key, Level: 90, Ascension: 6,
		TalentAuto: 1, TalentSkill: 1, TalentBurst: 1}
	var owned bool
	for _, c := range characters {
		if c.Key == key {
			character, owned = c, true
			break
		}
	}
	if _, err := snap.Char(key); err != nil {
		writeDomainError(w, err)
		return
	}

	ownedSets, err := s.ownedSets(r.Context(), a.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	ownedWeapons, err := s.ownedWeapons(r.Context(), a.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	// A goal's declared conditions are reused when there is one, so the
	// target and the plan do not disagree about the same character.
	var conditions map[string]float64
	if goal, _, _, err := s.loadGoal(r.Context(), a.ID, key); err == nil {
		conditions = goal.Conditions
	}

	target, err := advisor.BuildTarget(r.Context(), advisor.TargetRequest{
		Snapshot:     snap,
		Character:    character,
		OwnedSets:    ownedSets,
		OwnedWeapons: ownedWeapons,
		Conditions:   conditions,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if !owned {
		target.Caveats = append(target.Caveats,
			"You do not have this character, so the target is computed at level 90 with "+
				"talents at 1 and no constellations. Import an account that has her and the "+
				"numbers change.")
	}

	writeJSON(w, http.StatusOK, target)
}

// ownedSets reports which artifact sets the account can already field four of
// in four distinct slots. Used for labelling only.
func (s *Server) ownedSets(ctx context.Context, accountID int64) (map[string]bool, error) {
	inventory, err := db.LoadArtifacts(s.DB, accountID)
	if err != nil {
		return nil, err
	}
	slots := map[string]map[model.Slot]bool{}
	for _, a := range inventory {
		if a.SetKey == "" {
			continue
		}
		if slots[a.SetKey] == nil {
			slots[a.SetKey] = map[model.Slot]bool{}
		}
		slots[a.SetKey][a.SlotKey] = true
	}
	// Four pieces in four different slots. Five gobles are five pieces and
	// no four-piece set, which is the mistake counting rows would make.
	out := map[string]bool{}
	for key, s := range slots {
		if len(s) >= 4 {
			out[key] = true
		}
	}
	return out, nil
}

// ownedWeapons maps each owned weapon to its refinement.
func (s *Server) ownedWeapons(ctx context.Context, accountID int64) (map[string]int, error) {
	weapons, err := s.loadWeapons(ctx, accountID)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, w := range weapons {
		if r := w.Refinement; r > out[w.Key] {
			out[w.Key] = r
		}
	}
	return out, nil
}
