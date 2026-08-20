// Package optimizer searches an owned artifact inventory for the equipment
// that maximises an objective, subject to constraints such as "at least 200%
// Energy Recharge".
//
// Brute force is not an option: five slots with a few hundred owned pieces
// each is a combination count with fifteen digits. Mimir uses the same shape
// of solution as Genshin Optimizer — depth-first search over slots with an
// admissible upper bound per node — plus an up-front split by artifact set
// configuration, which both makes the bound valid in the presence of set
// bonuses and matches how players actually think about builds.
package optimizer

import (
	"fmt"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// ArtifactStats returns the stat contribution of a single artifact: its main
// stat at its current level plus its rolled substats.
func ArtifactStats(a model.Artifact, snap *gamedata.Snapshot) (model.StatBlock, error) {
	out := make(model.StatBlock, len(a.Substats)+1)

	byRarity, ok := snap.MainStatValues[a.Rarity]
	if !ok {
		return nil, fmt.Errorf("%w: main stat table for %d★ artifacts",
			gamedata.ErrMissing, a.Rarity)
	}
	curve, ok := byRarity[a.MainStat]
	if !ok {
		return nil, fmt.Errorf("%w: main stat %q on %d★", gamedata.ErrMissing, a.MainStat, a.Rarity)
	}
	if a.Level < 0 || a.Level >= len(curve) {
		return nil, fmt.Errorf("%w: %d★ %q at level %d (table has %d entries)",
			gamedata.ErrMissing, a.Rarity, a.MainStat, a.Level, len(curve))
	}
	out[a.MainStat] = curve[a.Level]

	for _, s := range a.Substats {
		out[s.Key] += s.Value
	}
	return out, nil
}

// SetCounts counts how many pieces of each set a build contains.
func SetCounts(pieces []model.Artifact) map[string]int {
	counts := make(map[string]int, 4)
	for _, a := range pieces {
		if a.SetKey != "" {
			counts[a.SetKey]++
		}
	}
	return counts
}

// SetBonuses returns the stat bonuses granted by a build's set counts.
// Conditional four-piece effects (Marechaussee's stacks, Emblem's ER
// conversion) are not stat blocks and are handled by the effect layer; this
// function covers the unconditional ones only.
func SetBonuses(counts map[string]int, snap *gamedata.Snapshot) (model.StatBlock, error) {
	out := make(model.StatBlock, 4)
	for key, n := range counts {
		if n < 2 {
			continue
		}
		def, err := snap.Set(key)
		if err != nil {
			return nil, err
		}
		out.AddInto(def.TwoPiece)
		if n >= 4 {
			out.AddInto(def.FourPiece)
		}
	}
	return out, nil
}
