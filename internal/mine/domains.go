package mine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/kristianwind/mimir/internal/gamedata"
)

// Artifact domains are the farmable half of the resin plan.
//
// Two things make them mineable where the weekday-gated talent domains are
// not: they are open every day, and genshin-db's reward preview names the two
// five-star sets each one drops. The datamine's own daily-rotation table has
// obfuscated weekday keys that rotate between versions, so talent and weapon
// domains stay in the supplements file until that is solved rather than being
// guessed at here.

type gdbDomain struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	DomainType    string `json:"domainType"`
	EntranceName  string `json:"entranceName"`
	RegionName    string `json:"regionName"`
	RewardPreview []struct {
		Name   string `json:"name"`
		Rarity int    `json:"rarity"`
	} `json:"rewardPreview"`
	DaysOfWeek []string `json:"daysofweek"`
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
		if d.DomainType != "UI_ABYSSUS_RELIC" {
			return nil
		}

		// Only the five-star sets. The reward preview also lists the three-
		// and four-star filler every artifact domain drops, and offering to
		// farm a domain for Resolution of Sojourner would be noise.
		seen := map[string]bool{}
		var sets []string
		for _, r := range d.RewardPreview {
			if r.Rarity != 5 || r.Name == "" {
				continue
			}
			key := GOODKey(r.Name)
			if key == "" || seen[key] {
				continue
			}
			if _, ok := snap.ArtifactSets[key]; !ok {
				continue
			}
			seen[key] = true
			sets = append(sets, key)
		}
		if len(sets) == 0 {
			return nil
		}
		sort.Strings(sets)

		key := GOODKey(d.EntranceName)
		if key == "" {
			key = GOODKey(d.Name)
		}
		// Later tiers of the same domain drop the same sets; keep the one
		// with the highest id, which is the level 90 tier a built account
		// actually runs.
		if existing, ok := snap.Domains[key]; ok && existing.ResinCost != 0 {
			return nil
		}
		snap.Domains[key] = gamedata.Domain{
			Key:  key,
			Name: d.EntranceName,
			Kind: "artifact",
			Sets: sets,
			// Artifact domains are open every day; the empty list says so.
			Days: nil,
		}
		return nil
	})
	if err != nil {
		return err
	}

	m.Log("artifact domains: %d", len(snap.Domains))
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
