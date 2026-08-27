package mine

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/kristianwind/mimir/internal/gamedata"
)

// The material catalogue: what every upgrade bill is written in.
//
// The bills themselves are numbers, so they come from the datamine, keyed by
// item id. What an id *is* — its name, and above all where you get it — is
// not in the datamine in any readable form: the names live in TextMap, which
// does not resolve (docs/GAMEDATA.md), and the drop sources live in a table
// whose keys are obfuscated. genshin-db has both, keyed by the same ids.
//
// That split is the same one the rest of the miner uses, and it is what makes
// this safe: a stale name source can mislabel a material, but it cannot
// change how many of them an ascension costs.

// MoraID is the item id of mora. Mora is separated from the rest of a bill
// because it is the one cost that is never resin-gated: folding it into a
// list of farmables would put "you need 420,000 mora" next to "you need six
// Everflame Seeds" as though they were the same kind of problem.
const MoraID = 202

type gdbMaterial struct {
	ID             int      `json:"id"`
	Name           string   `json:"name"`
	Rarity         int      `json:"rarity"`
	Category       string   `json:"category"`
	TypeText       string   `json:"typeText"`
	DropDomainID   int      `json:"dropDomainId"`
	DropDomainName string   `json:"dropDomainName"`
	DaysOfWeek     []string `json:"daysOfWeek"`
	Sources        []string `json:"sources"`
}

func (m *Miner) mineMaterials(ctx context.Context, snap *gamedata.Snapshot) error {
	listURL := fmt.Sprintf("https://api.github.com/repos/%s/contents/src/data/English/materials?ref=%s",
		m.Cfg.GenshinDBRepo, m.Cfg.GenshinDBRef)

	var listing []struct {
		Name        string `json:"name"`
		DownloadURL string `json:"download_url"`
	}
	if err := m.Src.GetJSON(ctx, listURL, &listing); err != nil {
		return fmt.Errorf("mine: list materials: %w", err)
	}

	urls := make([]string, 0, len(listing))
	for _, f := range listing {
		if strings.HasSuffix(f.Name, ".json") && f.DownloadURL != "" {
			urls = append(urls, f.DownloadURL)
		}
	}

	if snap.Materials == nil {
		snap.Materials = map[int]gamedata.Material{}
	}

	err := m.Src.GetManyJSON(ctx, urls, func(url string, raw []byte) error {
		var g gdbMaterial
		if err := json.Unmarshal(raw, &g); err != nil {
			return fmt.Errorf("mine: decode material %s: %w", url, err)
		}
		if g.ID == 0 || g.Name == "" {
			return nil
		}
		snap.Materials[g.ID] = gamedata.Material{
			ID:     g.ID,
			Name:   g.Name,
			Rarity: g.Rarity,
			Source: materialSource(g),
			Days:   weekdays(g.DaysOfWeek),
		}
		return nil
	})
	if err != nil {
		return err
	}

	m.Log("materials: %d", len(snap.Materials))
	if err := linkMaterialDomains(snap); err != nil {
		return err
	}
	return m.checkRotationGroups(ctx, snap)
}

// checkRotationGroups verifies the weekdays against the datamine.
//
// The days come from genshin-db, which is a second source and therefore the
// one that can go stale. The datamine cannot be read for weekday *names* —
// its rotation table is keyed by obfuscated identifiers — but it can be read
// for the grouping: DungeonEntryExcelConfigData lists, per talent domain,
// which materials share each rotation slot. Two books in one slot are
// available on exactly the same days, and two books in different slots are
// not.
//
// So the partition is checkable even though the labels are not, and that is
// what this checks. A disagreement means the day labels have drifted, which
// would send a player to a domain on the wrong day — quietly, and only on the
// days they are not playing.
func (m *Miner) checkRotationGroups(ctx context.Context, snap *gamedata.Snapshot) error {
	var entries []dungeonEntryRow
	if err := m.Src.GetJSON(ctx, m.excel("DungeonEntryExcelConfigData"), &entries); err != nil {
		return fmt.Errorf("mine: dungeon entries: %w", err)
	}

	var checked, disagreed int
	for _, e := range entries {
		if e.Type != "DUNGEN_ENTRY_TYPE_AVATAR_TALENT" && e.Type != "DUNGEN_ENTRY_TYPE_WEAPON_PROMOTE" {
			continue
		}
		for _, group := range e.DescriptionCycleRewardList {
			// The last slot of the cycle is Sunday, when everything is
			// open, so it says nothing about which days a material has.
			if len(group) < 2 || len(group) > 3 {
				continue
			}
			var want []int
			for _, id := range group {
				mat, ok := snap.Materials[id]
				if !ok || len(mat.Days) == 0 {
					want = nil
					break
				}
				if want == nil {
					want = mat.Days
					continue
				}
				checked++
				if !sameDays(want, mat.Days) {
					disagreed++
				}
			}
		}
	}

	if checked == 0 {
		m.Log("rotation check: no groups to check; the weekdays are unverified")
		return nil
	}
	if disagreed > 0 {
		return fmt.Errorf("mine: %d of %d materials disagree with the datamine about "+
			"which days share a domain rotation; the weekday source has drifted",
			disagreed, checked)
	}
	m.Log("rotation check: %d materials agree with the datamine's grouping", checked)
	return nil
}

func sameDays(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// materialSource classifies where a material comes from.
//
// The order matters. A structured field is trusted before a written one:
// a material that names a drop domain came from that domain, whatever its
// prose says. Only when nothing structured is present does this fall back to
// the item's type text — and when that says nothing either, the material is
// left explicitly unclassified rather than guessed at, because a wrong source
// would put an unfarmable material in a farm plan.
func materialSource(g gdbMaterial) gamedata.MaterialSource {
	if g.DropDomainID != 0 {
		return gamedata.SourceDomain
	}

	switch {
	case g.TypeText == "Character Ascension Material":
		// The elemental gems, and nothing else: checked against the whole
		// catalogue, this type text covers exactly the 32 gems — eight
		// elements in four tiers — and no other material.
		return gamedata.SourceGem
	case strings.HasPrefix(g.TypeText, "Local Specialty"):
		return gamedata.SourceOverworld
	case g.TypeText == "Character and Weapon Enhancement Material",
		g.TypeText == "Weapon Enhancement Material":
		// Common mob drops: slime condensate, insignia, handguards.
		return gamedata.SourceOverworld
	}

	// Boss materials are the remaining ascension and level-up materials, and
	// which boss decides the price: a world boss can be run every day, a
	// trounce domain is capped at three discounted runs a week. The two are
	// told apart by the prose, so the match is anchored on the phrases the
	// game itself uses rather than on a rarity that happens to correlate.
	joined := strings.Join(g.Sources, " ")
	switch {
	case weeklyBoss.MatchString(joined):
		return gamedata.SourceWeekly
	case worldBoss.MatchString(joined):
		return gamedata.SourceBoss
	case eventOnly.MatchString(joined):
		return gamedata.SourceEvent
	case questOnly.MatchString(joined):
		return gamedata.SourceQuest
	}
	return gamedata.SourceUnknown
}

var (
	// "Lv. 70+ Signora Challenge Reward", "Trounce Domain ... Challenge Reward"
	weeklyBoss = regexp.MustCompile(`(?i)(trounce domain|challenge reward)`)
	// "Dropped by Lv. 30+ Pyro Regisvines"
	worldBoss = regexp.MustCompile(`(?i)dropped by lv\.`)
	eventOnly = regexp.MustCompile(`(?i)(limited-duration event|event reward)`)
	// "Obtained from completed regional quests in Natlan"
	questOnly = regexp.MustCompile(`(?i)\bquests?\b`)
)

// weekdays turns genshin-db's day names into Go's numbering, 0 = Sunday.
//
// A day that cannot be read is dropped and the rest kept, because a domain
// listed as open on two of its three days is a smaller error than one listed
// as open every day — which is what an empty list means.
func weekdays(names []string) []int {
	var out []int
	for _, n := range names {
		if d, ok := weekdayNumbers[strings.ToLower(strings.TrimSpace(n))]; ok {
			out = append(out, d)
		}
	}
	sort.Ints(out)
	return out
}

var weekdayNumbers = map[string]int{
	"sunday":    0,
	"monday":    1,
	"tuesday":   2,
	"wednesday": 3,
	"thursday":  4,
	"friday":    5,
	"saturday":  6,
}

// linkMaterialDomains records which domain drops each material.
//
// The link is made from the domain's side, through the item ids in its reward
// preview, rather than by matching the domain name a material carries. Those
// two names are not the same string — a material says "Domain of Mastery:
// Realm of Slumber" where the domain calls itself by its entrance, "Forsaken
// Rift" — and a match that works today because two strings happen to agree is
// a match that breaks on the next domain that is named differently.
//
// It also checks the days. A material and the domain that drops it come from
// two different files, so they can disagree; if they do, one of them would
// send a player to a closed door.
func linkMaterialDomains(snap *gamedata.Snapshot) error {
	for key, d := range snap.Domains {
		if d.Kind == "artifact" {
			continue
		}
		for _, id := range d.Rewards {
			mat, ok := snap.Materials[id]
			if !ok {
				return fmt.Errorf("mine: domain %q drops item %d, which is not in the material catalogue", key, id)
			}
			if len(mat.Days) > 0 && len(d.Days) > 0 && !sameDays(mat.Days, d.Days) {
				return fmt.Errorf("mine: %s is open %v but %s is listed for %v; "+
					"the two name sources disagree about the rotation",
					d.Name, d.Days, mat.Name, mat.Days)
			}
			mat.Domain = key
			if len(mat.Days) == 0 {
				mat.Days = d.Days
			}
			snap.Materials[id] = mat
		}
	}
	return nil
}
