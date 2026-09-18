package api

// One character's own page.
//
// Sabrina, testing: "Jeg vil gerne have en side til hver karakter, som viser
// mig hvilke stykker jeg får mest ud af at opgradere. Og på karakter siden der
// er status på hvor godt karakteren er bygget."
//
// Everything it reports already existed, in views that are about something
// else: Potential ranks the whole roster, the grid compares up to eight
// characters slot by slot, the target says what to farm towards. None of them
// is the page you open when you have picked somebody and want to know what to
// do about them.
//
// It is one endpoint rather than three calls assembled in the browser because
// the expensive half — every artifact in the bag scored against this character
// — would otherwise be done twice, and because two round trips can disagree
// with each other about the same build while one cannot.

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kristianwind/mimir/internal/advisor"
	"github.com/kristianwind/mimir/internal/db"
	"github.com/kristianwind/mimir/internal/model"
)

func (s *Server) handleCharacterPage(w http.ResponseWriter, r *http.Request) {
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
	var character model.Character
	var owned bool
	for _, c := range characters {
		if c.Key == key {
			character, owned = c, true
			break
		}
	}
	// A character the account does not have is a 404 and not an empty page.
	// The target view answers for anybody, owned or not, because "what should
	// I farm towards" is a fair question about a character you are saving
	// for; this one is about the pieces you have on, and there are none.
	if !owned {
		writeError(w, http.StatusNotFound, "that character is not on this account",
			"Import your showcase or a .good file first, or open the target view, which answers for characters you have not pulled yet.")
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

	weapon, err := s.loadEquippedWeapon(r.Context(), a.ID, key)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	// What the account can already field four of, and which weapons it has.
	// Used to label the "aim" half, never to bias it: the recommendation is
	// what to farm towards, so preferring what is already owned would answer
	// a different question.
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

	// A goal's declared conditions are reused where there is one, so this
	// page and the plan do not disagree about the same character.
	var conditions map[string]float64
	if goal, _, _, err := s.loadGoal(r.Context(), a.ID, key); err == nil {
		conditions = goal.Conditions
	}

	status, err := advisor.CharacterStatus(r.Context(), advisor.BuildStatusRequest{
		Snapshot: snap,
		Loadout: advisor.Loadout{
			Character: character,
			Weapon:    weapon,
			Artifacts: equipped,
		},
		Inventory:    inventory,
		OwnedSets:    ownedSets,
		OwnedWeapons: ownedWeapons,
		Conditions:   conditions,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, status)
}
