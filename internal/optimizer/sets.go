package optimizer

import (
	"context"
	"fmt"
	"sort"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// SetConfig is one artifact set arrangement to search within.
//
// The search is run once per configuration rather than once over everything,
// for two reasons. Set bonuses are discrete, so they break the branch-and-
// bound's stat-based upper bound. And players think in configurations —
// "4pc Emblem" is a decision, not an emergent property — so results grouped
// this way are the ones they can act on.
type SetConfig struct {
	// Four names a four-piece set, or is empty for a 2+2.
	Four string
	// TwoA and TwoB name the halves of a 2+2, or are empty for a 4pc.
	TwoA, TwoB string
}

// String renders the configuration the way a player would say it.
func (c SetConfig) String() string {
	if c.Four != "" {
		return "4pc " + c.Four
	}
	if c.TwoA != "" && c.TwoB != "" {
		return "2pc " + c.TwoA + " + 2pc " + c.TwoB
	}
	return "any"
}

// Allows reports whether a build satisfies the configuration.
func (c SetConfig) Allows(counts map[string]int) bool {
	if c.Four != "" {
		return counts[c.Four] >= 4
	}
	if c.TwoA != "" && c.TwoB != "" {
		if c.TwoA == c.TwoB {
			return counts[c.TwoA] >= 4
		}
		return counts[c.TwoA] >= 2 && counts[c.TwoB] >= 2
	}
	return true
}

// EnumerateSetConfigs lists the configurations an inventory can actually
// produce, best-supported first.
//
// A 4pc needs four owned pieces in four distinct slots; a 2+2 needs two of
// each. Checking distinct slots rather than raw counts matters: five Emblem
// gobles are five pieces and zero four-piece sets.
func EnumerateSetConfigs(inventory []model.Artifact, maxConfigs int) []SetConfig {
	slotsBySet := map[string]map[model.Slot]int{}
	for _, a := range inventory {
		if a.SetKey == "" {
			continue
		}
		if slotsBySet[a.SetKey] == nil {
			slotsBySet[a.SetKey] = map[model.Slot]int{}
		}
		slotsBySet[a.SetKey][a.SlotKey]++
	}

	type ranked struct {
		key   string
		slots int
		total int
	}
	var sets []ranked
	for key, slots := range slotsBySet {
		total := 0
		for _, n := range slots {
			total += n
		}
		sets = append(sets, ranked{key: key, slots: len(slots), total: total})
	}
	sort.Slice(sets, func(i, j int) bool {
		if sets[i].slots != sets[j].slots {
			return sets[i].slots > sets[j].slots
		}
		if sets[i].total != sets[j].total {
			return sets[i].total > sets[j].total
		}
		return sets[i].key < sets[j].key
	})

	var out []SetConfig
	for _, s := range sets {
		if s.slots >= 4 {
			out = append(out, SetConfig{Four: s.key})
		}
	}
	for i := 0; i < len(sets); i++ {
		if sets[i].slots < 2 {
			continue
		}
		for j := i + 1; j < len(sets); j++ {
			if sets[j].slots < 2 {
				continue
			}
			out = append(out, SetConfig{TwoA: sets[i].key, TwoB: sets[j].key})
		}
	}

	if maxConfigs > 0 && len(out) > maxConfigs {
		out = out[:maxConfigs]
	}
	return out
}

// BestBuild searches every configuration and returns the best build overall,
// plus the best per configuration so the UI can show the trade-off.
//
// objectiveFor supplies the scoring function for a configuration. It is a
// factory rather than a single objective because set bonuses and their
// conditional effects differ per configuration, and a build has to be scored
// with the bonuses it would actually have.
func BestBuild(
	ctx context.Context,
	snap *gamedata.Snapshot,
	inventory []model.Artifact,
	fixed model.StatBlock,
	constraints []Constraint,
	configs []SetConfig,
	objectiveFor func(SetConfig) Objective,
	budget int,
) (ConfigResult, error) {
	stats := make(map[int64]model.StatBlock, len(inventory))
	for _, a := range inventory {
		block, err := ArtifactStats(a, snap)
		if err != nil {
			return ConfigResult{}, err
		}
		stats[a.ID] = block
	}

	var out ConfigResult
	for _, cfg := range configs {
		pools := poolsFor(inventory, cfg)
		if len(pools) == 0 {
			continue
		}
		// The objective is built per configuration because a set's
		// conditional effects are part of what makes that configuration
		// worth wearing — scoring 4pc Emblem without its burst conversion
		// would rank it as two pieces of Energy Recharge.
		obj := objectiveFor(cfg)
		setBonus, err := SetBonuses(configCounts(cfg), snap)
		if err != nil {
			return ConfigResult{}, err
		}

		// The budget is the configuration's, not each sub-search's, so
		// splitting a four-piece set into five problems does not quietly
		// buy it five times the work.
		// Each piece scores the same in every sub-search of this
		// configuration, so it is scored once here rather than five times
		// inside them.
		order := make(map[int64]float64, len(inventory))
		for _, pool := range pools {
			for _, slot := range model.Slots {
				for _, a := range pool[slot] {
					if _, seen := order[a.ID]; !seen {
						order[a.ID] = obj(stats[a.ID])
					}
				}
			}
		}

		share := budget
		if share > 0 && len(pools) > 1 {
			share = share / len(pools)
			if share < 1 {
				share = 1
			}
		}

		var (
			best   Build
			found  bool
			capped bool
		)
		for _, pool := range pools {
			res, err := Search(ctx, Problem{
				Fixed:       fixed,
				Pool:        pool,
				Stats:       stats,
				SetBonus:    setBonus,
				Constraints: constraints,
				// Even a pool split by free slot can assemble a build that
				// misses the count — the free slot may hold a set piece
				// while another slot is short. Enforcing the requirement
				// inside the search means the winner is the best *valid*
				// build, not the best build that then gets rejected.
				Valid:     func(pieces []model.Artifact) bool { return cfg.Allows(SetCounts(pieces)) },
				TopN:      1,
				MaxVisits: share,
				Order:     order,
			}, obj)
			if err != nil {
				return out, err
			}
			if !res.Complete {
				capped = true
			}
			if len(res.Builds) == 0 {
				continue
			}
			if !found || res.Builds[0].Score > best.Score {
				best, found = res.Builds[0], true
			}
		}
		if !found {
			continue
		}

		if capped {
			out.Capped = append(out.Capped, cfg)
		}
		out.PerConfig = append(out.PerConfig, ConfigBuild{Config: cfg, Build: best})
		if out.Best.Score == 0 || best.Score > out.Best.Score {
			out.Best = best
			out.BestConfig = cfg
		}
	}

	sort.SliceStable(out.PerConfig, func(i, j int) bool {
		return out.PerConfig[i].Build.Score > out.PerConfig[j].Build.Score
	})
	if len(out.PerConfig) == 0 {
		return out, fmt.Errorf("optimizer: no build satisfies any of the %d set configurations", len(configs))
	}
	return out, nil
}

// ConfigBuild pairs a set configuration with its best build.
type ConfigBuild struct {
	Config SetConfig `json:"config"`
	Build  Build     `json:"build"`
}

// ConfigResult is the outcome of searching every configuration.
type ConfigResult struct {
	Best       Build         `json:"best"`
	BestConfig SetConfig     `json:"bestConfig"`
	PerConfig  []ConfigBuild `json:"perConfig"`
	// Capped names the configurations whose search ran out of budget. Their
	// builds are the best found rather than the best there are, and anything
	// that reports them has to pass that on.
	Capped []SetConfig `json:"capped,omitempty"`
}

// poolsFor restricts the inventory to the pieces a configuration can use.
//
// A four-piece set is searched as five separate problems, one for each choice
// of which slot is *not* wearing the set. That is exact — a build with four
// or more pieces of a set has at most one slot outside it, and a build
// wearing all five turns up in every branch — and it is the difference
// between a search that answers and one that does not.
//
// The alternative, letting every piece into every slot and leaving the set
// requirement to the Valid predicate on finished builds, is what this used to
// do. On a small inventory it is indistinguishable. On a real one it is
// close to useless: with a dozen sets in the bag, roughly two builds in ten
// thousand actually contain four of the wanted set, so virtually the entire
// search — and virtually the entire budget — is spent assembling builds that
// are thrown away at the last step.
//
// A 2+2 needs no such split. Every slot is already restricted to the two
// sets, so almost everything the search assembles is valid. A 2+2+1 with a
// third set is a different configuration, and enumerating it here would
// multiply the work to buy a build almost nobody assembles on purpose.
func poolsFor(inventory []model.Artifact, cfg SetConfig) []map[model.Slot][]model.Artifact {
	if cfg.Four != "" {
		var out []map[model.Slot][]model.Artifact
		for _, free := range model.Slots {
			pool := map[model.Slot][]model.Artifact{}
			for _, a := range inventory {
				if a.SlotKey == free || a.SetKey == cfg.Four {
					pool[a.SlotKey] = append(pool[a.SlotKey], a)
				}
			}
			if complete(pool) {
				out = append(out, pool)
			}
		}
		return out
	}

	pool := map[model.Slot][]model.Artifact{}
	for _, a := range inventory {
		if cfg.TwoA != "" && a.SetKey != cfg.TwoA && a.SetKey != cfg.TwoB {
			continue
		}
		pool[a.SlotKey] = append(pool[a.SlotKey], a)
	}
	if !complete(pool) {
		return nil
	}
	return []map[model.Slot][]model.Artifact{pool}
}

func complete(pool map[model.Slot][]model.Artifact) bool {
	for _, slot := range model.Slots {
		if len(pool[slot]) == 0 {
			return false
		}
	}
	return true
}

// Counts returns the set counts a configuration guarantees.
func (c SetConfig) Counts() map[string]int { return configCounts(c) }

func configCounts(cfg SetConfig) map[string]int {
	switch {
	case cfg.Four != "":
		return map[string]int{cfg.Four: 4}
	case cfg.TwoA != "" && cfg.TwoB != "":
		if cfg.TwoA == cfg.TwoB {
			return map[string]int{cfg.TwoA: 4}
		}
		return map[string]int{cfg.TwoA: 2, cfg.TwoB: 2}
	default:
		return map[string]int{}
	}
}
