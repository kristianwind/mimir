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

// Domains are the farmable half of the resin plan.
//
// All three kinds come from genshin-db's domain files, which carry what the
// datamine cannot be read for: the entrance name, the five-star sets or
// materials in the reward preview, and — for the weekday-gated talent and
// weapon domains — the days each one is open. The datamine's own daily
// rotation table is keyed by names that are obfuscated and rotate between
// versions, so it cannot be read directly; what it does carry, in
// DungeonEntryExcelConfigData, is which materials share a rotation slot, and
// that grouping is what the weekdays here are checked against.

type gdbDomain struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	DomainType    string `json:"domainType"`
	EntranceName  string `json:"entranceName"`
	RegionName    string `json:"regionName"`
	RewardPreview []struct {
		ID     int    `json:"id"`
		Name   string `json:"name"`
		Rarity int    `json:"rarity"`
		Count  int    `json:"count"`
	} `json:"rewardPreview"`
	// genshin-db writes the key as daysOfWeek; the lowercase spelling was
	// what an older layout used. Both are accepted so a rename upstream
	// shows up as domains with no days rather than as a silent every-day.
	DaysOfWeek    []string `json:"daysOfWeek"`
	DaysOfWeekAlt []string `json:"daysofweek"`
}

// withoutTier strips the tier numeral from a domain name, so that the four
// difficulties of one domain are one entry: they drop the same materials on
// the same days, and a plan that named a tier would be telling the player
// something they cannot act on.
func withoutTier(name string) string {
	fields := strings.Fields(strings.TrimSpace(name))
	if n := len(fields); n > 1 && romanNumeral.MatchString(fields[n-1]) {
		fields = fields[:n-1]
	}
	return strings.Join(fields, " ")
}

var romanNumeral = regexp.MustCompile(`^[IVX]+$`)

func (d gdbDomain) days() []string {
	if len(d.DaysOfWeek) > 0 {
		return d.DaysOfWeek
	}
	return d.DaysOfWeekAlt
}

// domainKinds maps genshin-db's domain type onto the kind the resin costs are
// keyed by. A type that is not here is not a farmable upgrade domain — the
// blessing domains that give adventure rank, the one-off story domains.
var domainKinds = map[string]string{
	"UI_ABYSSUS_RELIC":          "artifact",
	"UI_ABYSSUS_AVATAR_PROUD":   "talent",
	"UI_ABYSSUS_WEAPON_PROMOTE": "weapon",
}

func (m *Miner) mineDomains(ctx context.Context, snap *gamedata.Snapshot) error {
	listURL := fmt.Sprintf("https://api.github.com/repos/%s/contents/src/data/English/domains?ref=%s",
		m.Cfg.GenshinDBRepo, m.Cfg.GenshinDBRef)

	var listing []struct {
		Name        string `json:"name"`
		DownloadURL string `json:"download_url"`
	}
	if err := m.Src.GetJSON(ctx, listURL, &listing); err != nil {
		return fmt.Errorf("mine: list domains: %w", err)
	}

	urls := make([]string, 0, len(listing))
	for _, f := range listing {
		if strings.HasSuffix(f.Name, ".json") && f.DownloadURL != "" {
			urls = append(urls, f.DownloadURL)
		}
	}

	// A set key -> the domain that drops it, so the plan can go from "you
	// want more Emblem" to "here is where to get it".
	if snap.Domains == nil {
		snap.Domains = map[string]gamedata.Domain{}
	}

	err := m.Src.GetManyJSON(ctx, urls, func(url string, raw []byte) error {
		var d gdbDomain
		if err := json.Unmarshal(raw, &d); err != nil {
			return fmt.Errorf("mine: decode domain %s: %w", url, err)
		}
		kind, ok := domainKinds[d.DomainType]
		if !ok {
			return nil
		}

		// Artifact domains are identified by their entrance, because that is
		// the unit a player farms: every tier behind one door drops the same
		// two sets, every day.
		//
		// Talent and weapon domains are not. One entrance holds three named
		// domains on three different rotations, so keying them by the door
		// would merge three day-sets into one and make every book look
		// available every day. They are keyed by the named domain instead,
		// with the tier numeral dropped.
		key, name := GOODKey(d.EntranceName), strings.TrimSpace(d.EntranceName)
		if kind != "artifact" {
			name = withoutTier(d.Name)
			key = GOODKey(name)
		}
		if key == "" {
			return nil
		}

		// Every tier of a domain drops the same things on the same days, so
		// they collapse into one entry. The reward previews are merged
		// rather than the highest tier winning outright: the lower tiers of
		// a talent domain list the two- and three-star books the top tier
		// leaves out, and a bill is usually paid in all three.
		entry, seen := snap.Domains[key]
		if !seen {
			entry = gamedata.Domain{
				Key:      key,
				Name:     name,
				Kind:     kind,
				Entrance: strings.TrimSpace(d.EntranceName),
				Days:     weekdays(d.days()),
			}
		}

		if kind == "artifact" {
			// Only the five-star sets. The reward preview also lists the
			// three- and four-star filler every artifact domain drops, and
			// offering to farm a domain for Resolution of Sojourner would
			// be noise.
			have := map[string]bool{}
			for _, s := range entry.Sets {
				have[s] = true
			}
			for _, r := range d.RewardPreview {
				if r.Rarity != 5 || r.Name == "" {
					continue
				}
				set := GOODKey(r.Name)
				if set == "" || have[set] {
					continue
				}
				if _, ok := snap.ArtifactSets[set]; !ok {
					continue
				}
				have[set] = true
				entry.Sets = append(entry.Sets, set)
			}
			sort.Strings(entry.Sets)
			if len(entry.Sets) == 0 {
				return nil
			}
		} else {
			// Talent and weapon domains are described by what they drop, and
			// the reward is recorded by id so the bill can be matched to the
			// domain without going through a name a second time.
			have := map[int]bool{}
			for _, r := range entry.Rewards {
				have[r] = true
			}
			for _, r := range d.RewardPreview {
				// A preview entry with a fixed count is the guaranteed
				// payout every run makes — mora, adventure and companionship
				// experience. The materials people actually enter for are
				// the ones with no count, because how many drop is not
				// fixed. That absence is the only structural difference
				// between the two, and it is what separates them here.
				if r.ID == 0 || r.Count != 0 || have[r.ID] {
					continue
				}
				have[r.ID] = true
				entry.Rewards = append(entry.Rewards, r.ID)
			}
			sort.Ints(entry.Rewards)
			if len(entry.Rewards) == 0 {
				return nil
			}
		}

		snap.Domains[key] = entry
		return nil
	})
	if err != nil {
		return err
	}

	byKind := map[string]int{}
	for _, d := range snap.Domains {
		byKind[d.Kind]++
	}
	m.Log("domains: %d artifact, %d talent, %d weapon",
		byKind["artifact"], byKind["talent"], byKind["weapon"])
	return nil
}

// applyResinCosts fills in each domain's price from the supplied cost table.
// A domain with no price cannot be ranked per resin, and says so rather than
// being silently free.
func applyResinCosts(snap *gamedata.Snapshot) {
	for key, d := range snap.Domains {
		if d.ResinCost > 0 {
			continue
		}
		if cost, ok := snap.ResinCosts[d.Kind+"_domain"]; ok {
			d.ResinCost = cost
			snap.Domains[key] = d
		}
	}
}
