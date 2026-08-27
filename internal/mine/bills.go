package mine

import (
	"context"
	"fmt"
	"sort"

	"github.com/kristianwind/mimir/internal/gamedata"
)

// Upgrade bills: what an ascension phase or a talent level actually costs.
//
// These are pure numbers and they come straight from the datamine, keyed by
// item id. Nothing here is derived, inferred or rounded — the whole point of
// the material accounting is that the bill is exact, so that the parts which
// cannot be made exact (how many domain runs a bill takes) are visibly the
// only soft edge in the answer.

// bill turns a datamine cost list into a snapshot bill.
//
// The upstream lists are padded with empty objects; an entry with no id is a
// hole in the padding, not a free material. Mora is lifted out of the item
// list because it is never resin-gated and folding it in would make it look
// like something to farm.
func bill(level int, coin int, items []costItem) gamedata.Bill {
	b := gamedata.Bill{Level: level, Mora: coin}
	for _, it := range items {
		switch {
		case it.ID == 0 || it.Count == 0:
			continue
		case it.ID == MoraID:
			b.Mora += it.Count
		default:
			b.Items = append(b.Items, gamedata.ItemCost{ID: it.ID, Count: it.Count})
		}
	}
	sort.Slice(b.Items, func(i, j int) bool { return b.Items[i].ID < b.Items[j].ID })
	return b
}

// applyAscensionBills records what each of the six ascension phases costs.
func applyAscensionBills(c *gamedata.Character, rows []promoteRow) {
	var out []gamedata.Bill
	for _, r := range rows {
		// Phase 0 is the unascended state, which nothing is paid for.
		if r.PromoteLevel <= 0 || r.PromoteLevel > 6 {
			continue
		}
		b := bill(r.PromoteLevel, r.ScoinCost, r.CostItems)
		if len(b.Items) == 0 && b.Mora == 0 {
			continue
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Level < out[j].Level })
	c.AscensionBills = out
}

// mineTalentBills reads the talent level-up costs, one table per talent.
//
// Per talent and not per character, on the strength of one character in 117:
// the Geo Traveler's normal attack takes Resistance books and Dvalin's Sigh
// where the skill and burst take Diligence and Tail of Boreas. Reading a
// single table and reusing it for all three would be right everywhere else
// and quietly wrong there, which is the worst shape an error can take.
func (m *Miner) mineTalentBills(ctx context.Context, snap *gamedata.Snapshot) error {
	var rows []proudSkillRow
	if err := m.Src.GetJSON(ctx, m.excel("ProudSkillExcelConfigData"), &rows); err != nil {
		return fmt.Errorf("mine: proud skills: %w", err)
	}

	byGroup := map[int][]proudSkillRow{}
	for _, r := range rows {
		if r.ProudSkillGroupID != 0 {
			byGroup[r.ProudSkillGroupID] = append(byGroup[r.ProudSkillGroupID], r)
		}
	}

	priced := 0
	for key, c := range snap.Characters {
		groups := map[string]int{
			gamedata.TalentAuto:  c.ProudSkillGroupIDs.Auto,
			gamedata.TalentSkill: c.ProudSkillGroupIDs.Skill,
			gamedata.TalentBurst: c.ProudSkillGroupIDs.Burst,
		}
		bills := map[string][]gamedata.Bill{}
		for slot, group := range groups {
			if group == 0 {
				continue
			}
			var out []gamedata.Bill
			for _, r := range byGroup[group] {
				// Level 1 is free; the bill is for reaching the level.
				if r.Level < 2 || r.Level > 10 {
					continue
				}
				b := bill(r.Level, r.CoinCost, r.CostItems)
				if len(b.Items) == 0 && b.Mora == 0 {
					continue
				}
				out = append(out, b)
			}
			if len(out) == 0 {
				continue
			}
			sort.Slice(out, func(i, j int) bool { return out[i].Level < out[j].Level })
			bills[slot] = out
		}
		if len(bills) == 0 {
			continue
		}
		c.TalentBills = bills
		snap.Characters[key] = c
		priced++
	}

	m.Log("talent bills: %d characters", priced)
	return nil
}
