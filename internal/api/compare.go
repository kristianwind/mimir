package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/kristianwind/mimir/internal/advisor"
	"github.com/kristianwind/mimir/internal/db"
	"github.com/kristianwind/mimir/internal/enka"
	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// Comparing one account against another.
//
// The request people actually make is "how do I compare to everyone else",
// and the honest answer is that Mimir cannot know. The public leaderboards
// are built from accounts whose owners chose to submit them — a population
// of the already-optimised — so a percentile against it says more about who
// bothers to publish than about whether a build is good. And reaching for one
// would mean scraping a service that answers 403 to anything but a browser.
//
// What Mimir can do is exact: fetch a showcase its owner has published, run
// the same yardstick over it that it runs over yours, and put the two numbers
// side by side. One ruler, both builds, and every figure produced by the same
// engine. That is a comparison somebody can act on, and it is checkable.
//
// Nothing about the other account is stored. It is fetched, measured and
// dropped: the showcase belongs to whoever published it, and Mimir has no
// business keeping a copy because somebody looked at it once.

// Comparison is one account measured against another.
type Comparison struct {
	UID      string `json:"uid"`
	Nickname string `json:"nickname,omitempty"`
	ARLevel  int    `json:"arLevel,omitempty"`
	// Stale says the showcase came from a cache because Enka could not be
	// reached, so it may be older than the other account's current build.
	Stale bool `json:"stale,omitempty"`

	Characters []CharacterComparison `json:"characters"`
	// OnlyTheirs and OnlyYours name the characters one side showed and the
	// other did not. A showcase holds at most a handful, chosen by its
	// owner, so an absence here is not evidence of anything.
	OnlyTheirs []string `json:"onlyTheirs,omitempty"`
	OnlyYours  []string `json:"onlyYours,omitempty"`
	Skipped    []string `json:"skipped,omitempty"`
	Caveats    []string `json:"caveats"`
}

// CharacterComparison is one character both accounts showed.
type CharacterComparison struct {
	Character string              `json:"character"`
	Yours     advisor.Measurement `json:"yours"`
	Theirs    advisor.Measurement `json:"theirs"`
	// Ratio is yours divided by theirs. One means the two builds do the same
	// damage on the yardstick.
	Ratio float64 `json:"ratio"`
}

func (s *Server) handleCompare(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())
	uid := chi.URLParam(r, "uid")

	// Validated here as well as inside Fetch. Fetch refuses it either way, so
	// this is not a security fix — it is a status code and a sentence. Left
	// to Fetch, a mistyped UID comes back as a 500 carrying "enka: uid ...
	// must be digits only", which reports the reader's typo as a server fault
	// and shows them a package name.
	if err := enka.ValidateUID(uid); err != nil {
		writeError(w, http.StatusBadRequest, "that is not a UID",
			"A UID is nine digits, at the bottom right in the game.")
		return
	}

	if uid == a.UID {
		writeError(w, http.StatusBadRequest,
			"that is this account's own UID",
			"Enter somebody else's — a friend's, or any UID whose owner has published a showcase.")
		return
	}
	if s.Enka == nil {
		writeError(w, http.StatusServiceUnavailable,
			"the Enka client is not configured", "")
		return
	}

	snap, err := s.GameData.Current()
	if err != nil {
		writeDomainError(w, err)
		return
	}

	fetched, err := s.Enka.Fetch(r.Context(), uid)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	// userID 0: this import is never written anywhere, so it belongs to
	// nobody. Passing the caller's id would make it look like it might be.
	theirs := fetched.Import(0, snap)
	if len(theirs.Characters) == 0 {
		writeError(w, http.StatusUnprocessableEntity,
			"that UID shows no characters",
			"A showcase has to be switched on in the game, under Profile → Edit, before anyone can read it.")
		return
	}

	out := Comparison{
		UID:      uid,
		Nickname: theirs.Account.Nickname,
		ARLevel:  theirs.Account.ARLevel,
		Stale:    fetched.Stale,
		Caveats: []string{
			"Both builds are measured the same way: one cast of the elemental skill and one of the burst, at each character's own talent levels, against a level 90 enemy with 10% resistance. No teams, no rotations, no reactions.",
			"A showcase holds a handful of characters, chosen by its owner and only as current as the last time they logged in. A character missing from one side is not evidence about that account.",
			"Constellations and weapon refinements are part of the score and are shown next to it, because a build that wins on a constellation is not a build you can copy.",
		},
		Characters: []CharacterComparison{},
	}

	mine, err := s.compareLoadouts(r.Context(), a.ID, snap)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	both := loadoutsOf(theirs, snap)

	for key, theirLoadout := range both {
		myLoadout, ok := mine[key]
		if !ok {
			out.OnlyTheirs = append(out.OnlyTheirs, key)
			continue
		}
		yours, err := advisor.Measure(r.Context(), snap, myLoadout, nil)
		if err != nil {
			out.Skipped = append(out.Skipped, fmt.Sprintf("%s on this account: %v", key, err))
			continue
		}
		mirror, err := advisor.Measure(r.Context(), snap, theirLoadout, nil)
		if err != nil {
			out.Skipped = append(out.Skipped, fmt.Sprintf("%s on %s: %v", key, uid, err))
			continue
		}
		c := CharacterComparison{Character: key, Yours: yours, Theirs: mirror}
		if mirror.Score > 0 {
			c.Ratio = yours.Score / mirror.Score
		}
		out.Characters = append(out.Characters, c)
	}
	for key := range mine {
		if _, ok := both[key]; !ok {
			out.OnlyYours = append(out.OnlyYours, key)
		}
	}

	// Weakest first: the point of the page is what to work on.
	sort.SliceStable(out.Characters, func(i, j int) bool {
		return out.Characters[i].Ratio < out.Characters[j].Ratio
	})
	sort.Strings(out.OnlyTheirs)
	sort.Strings(out.OnlyYours)

	if len(out.Characters) == 0 {
		writeError(w, http.StatusUnprocessableEntity,
			"the two accounts have no character in common",
			"A comparison needs the same character on both sides. Their showcase holds "+
				joinKeys(out.OnlyTheirs)+".")
		return
	}

	s.audit(r, "account.compare", uid, map[string]any{"characters": len(out.Characters)})
	writeJSON(w, http.StatusOK, out)
}

// compareLoadouts assembles this account's characters as they are equipped.
func (s *Server) compareLoadouts(
	ctx context.Context, accountID int64, snap *gamedata.Snapshot,
) (map[string]advisor.Loadout, error) {
	characters, err := s.loadCharacters(ctx, accountID)
	if err != nil {
		return nil, err
	}
	inventory, err := db.LoadArtifacts(s.DB, accountID)
	if err != nil {
		return nil, err
	}

	equipped := map[string][]model.Artifact{}
	for _, art := range inventory {
		if art.Location != "" {
			equipped[art.Location] = append(equipped[art.Location], art)
		}
	}

	out := map[string]advisor.Loadout{}
	for _, c := range characters {
		pieces := equipped[c.Key]
		if len(pieces) == 0 {
			continue
		}
		weapon, err := s.loadEquippedWeapon(ctx, accountID, c.Key)
		if err != nil {
			return nil, err
		}
		out[c.Key] = advisor.Loadout{Character: c, Weapon: weapon, Artifacts: pieces}
	}
	return out, nil
}

// loadoutsOf turns a fetched showcase into loadouts without persisting it.
func loadoutsOf(res enka.ImportResult, snap *gamedata.Snapshot) map[string]advisor.Loadout {
	weapons := map[string]*model.Weapon{}
	for i := range res.Weapons {
		w := res.Weapons[i]
		if w.Location != "" {
			weapons[w.Location] = &w
		}
	}
	pieces := map[string][]model.Artifact{}
	for _, a := range res.Artifacts {
		if a.Location != "" {
			pieces[a.Location] = append(pieces[a.Location], a)
		}
	}

	out := map[string]advisor.Loadout{}
	for _, c := range res.Characters {
		if len(pieces[c.Key]) == 0 {
			continue
		}
		if _, err := snap.Char(c.Key); err != nil {
			continue
		}
		out[c.Key] = advisor.Loadout{
			Character: c, Weapon: weapons[c.Key], Artifacts: pieces[c.Key],
		}
	}
	return out
}

func joinKeys(keys []string) string {
	if len(keys) == 0 {
		return "nothing Mimir could read"
	}
	if len(keys) > 6 {
		keys = keys[:6]
	}
	return joinWords(keys)
}

func joinWords(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	out := ""
	for i, n := range names[:len(names)-1] {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out + " and " + names[len(names)-1]
}
