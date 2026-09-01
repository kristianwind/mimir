package api

// The grid.
//
// Sabrina, after using the chat: "Tror du den kan lave en visuel guide,
// ligesom mit excel ark, så jeg ikke behøver at spørge den hvilke karakter den
// skal sammenligne, men hvor jeg kan vælge til og fra?"
//
// She already maintains this by hand — characters down the side, the five
// slots across, each cell coloured. What she is asking for is that sheet with
// the colours computed instead of typed.
//
// Choosing who is in it is not only a convenience. Scoring one piece is a
// damage calculation, and a full account is around two hundred artifacts
// against seventy characters — some fourteen thousand evaluations, which is
// not a page load. So the selection is what makes the view possible as well as
// what she asked for, and the endpoint refuses a request that does not make
// one rather than quietly measuring everybody.

import (
	"net/http"
	"sort"
	"strings"

	"github.com/kristianwind/mimir/internal/advisor"
	"github.com/kristianwind/mimir/internal/db"
	"github.com/kristianwind/mimir/internal/model"
)

// gridLimit caps how many characters one request may ask for.
//
// Not a guess: five characters is about a thousand damage evaluations, which
// is the same order as the potential page already does on every visit. Past
// that the wait stops looking like a page and starts looking like a hang, and
// a grid nobody waits for is not a grid.
const gridLimit = 8

func (s *Server) handleArtifactGrid(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())

	var keys []string
	for _, k := range strings.Split(r.URL.Query().Get("characters"), ",") {
		if k = strings.TrimSpace(k); k != "" {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		writeError(w, http.StatusBadRequest, "no characters were chosen",
			"Pick who the grid should cover. Measuring a whole roster piece by piece is thousands of damage calculations, so this asks rather than guesses.")
		return
	}
	if len(keys) > gridLimit {
		writeError(w, http.StatusUnprocessableEntity,
			"that is more characters than one grid can measure at once",
			"Choose up to eight. Each one is scored against every artifact in the bag, so the wait grows with the list.")
		return
	}

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
	inventory, err := db.LoadArtifacts(s.DB, a.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	owned := map[string]model.Character{}
	for _, c := range characters {
		owned[c.Key] = c
	}
	equipped := map[string][]model.Artifact{}
	for _, art := range inventory {
		if art.Location != "" {
			equipped[art.Location] = append(equipped[art.Location], art)
		}
	}

	out := struct {
		Rows    []advisor.ArtifactRanking `json:"rows"`
		Missing []string                  `json:"missing,omitempty"`
	}{Rows: []advisor.ArtifactRanking{}}

	for _, key := range keys {
		c, ok := owned[key]
		if !ok {
			// Named but not on the account. Reported rather than dropped:
			// a row silently missing from a grid reads as "nothing to say
			// about her", which is a different claim.
			out.Missing = append(out.Missing, key+": not on this account")
			continue
		}
		weapon, err := s.loadEquippedWeapon(r.Context(), a.ID, key)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		var conditions map[string]float64
		if goal, _, _, err := s.loadGoal(r.Context(), a.ID, key); err == nil {
			conditions = goal.Conditions
		}

		ranking, err := advisor.RankArtifacts(r.Context(), advisor.ArtifactRankingRequest{
			Snapshot: snap,
			Loadout: advisor.Loadout{
				Character: c,
				Weapon:    weapon,
				Artifacts: equipped[key],
			},
			Inventory:  inventory,
			Conditions: conditions,
		})
		if err != nil {
			// One character that cannot be measured must not take the grid
			// down with it — the same rule the potential ranking follows.
			out.Missing = append(out.Missing, key+": "+err.Error())
			continue
		}
		out.Rows = append(out.Rows, ranking)
	}

	// The order the caller asked for is the order they see. Sorting by score
	// here would fight the selection: she picks who to compare, and a grid
	// that reshuffles itself is harder to read twice.
	pos := map[string]int{}
	for i, k := range keys {
		pos[k] = i
	}
	sort.SliceStable(out.Rows, func(i, j int) bool {
		return pos[out.Rows[i].Character] < pos[out.Rows[j].Character]
	})

	writeJSON(w, http.StatusOK, out)
}
