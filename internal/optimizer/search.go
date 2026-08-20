package optimizer

import (
	"context"
	"fmt"
	"sort"

	"github.com/kristianwind/mimir/internal/model"
)

// Objective scores a resolved stat block. Higher is better.
//
// The bound below is only admissible if the objective is monotone
// non-decreasing in every stat it reads. Damage objectives satisfy this:
// more ATK, more crit, more DMG% is never worse. An objective that *punishes*
// a stat (say, one that penalises overcapped Energy Recharge) breaks the
// bound and must be expressed as a Constraint instead.
type Objective func(stats model.StatBlock) float64

// Constraint is a hard requirement a build must satisfy, e.g. minimum Energy
// Recharge for a rotation to work. Constraints are checked on complete builds
// and, where the stat can only grow, used to prune early.
type Constraint struct {
	Stat model.Stat
	Min  float64
}

// Problem is one optimisation request.
type Problem struct {
	// Fixed is everything that does not come from artifacts: character base
	// contribution, weapon substat, passives, team buffs.
	Fixed model.StatBlock
	// Pool is the candidate artifacts per slot, already filtered by the
	// caller (set configuration, rarity floor, level floor).
	Pool map[model.Slot][]model.Artifact
	// Stats maps an artifact's identity to its precomputed stat block, so
	// the search never recomputes main-stat lookups on the hot path.
	Stats map[int64]model.StatBlock
	// SetBonus is the stat bonus for this set configuration. It is constant
	// across the search because the pool is already restricted to it.
	SetBonus model.StatBlock
	// Constraints must all hold for a build to be reported.
	Constraints []Constraint
	// Valid is an optional predicate on the assembled pieces, used for
	// requirements that are not expressible as a stat floor — "this build
	// must actually contain four Emblem pieces", say.
	//
	// Like Constraints, it is checked on complete builds only, and pruning
	// stays sound: a subtree is dropped only when its optimistic bound
	// cannot beat the worst build already kept, and every kept build is
	// valid. An invalid completion could not have improved the ranking
	// either way.
	Valid func(pieces []model.Artifact) bool
	// TopN is how many builds to keep. Zero means one.
	TopN int
}

// Build is one complete five-piece result.
type Build struct {
	Pieces []model.Artifact `json:"pieces"`
	Stats  model.StatBlock  `json:"stats"`
	Score  float64          `json:"score"`
}

// Result carries the ranked builds plus search telemetry, which is what makes
// the pruning trustworthy: if Pruned is zero on a large pool, the bound is
// broken and the run was an expensive brute force.
type Result struct {
	Builds   []Build `json:"builds"`
	Visited  int     `json:"visited"`
	Pruned   int     `json:"pruned"`
	Complete bool    `json:"complete"`
}

// Search runs the branch-and-bound.
func Search(ctx context.Context, p Problem, obj Objective) (Result, error) {
	for _, slot := range model.Slots {
		if len(p.Pool[slot]) == 0 {
			return Result{}, fmt.Errorf("optimizer: no candidates for slot %q", slot)
		}
	}
	topN := p.TopN
	if topN < 1 {
		topN = 1
	}

	// Order slots by pool size, smallest first. Narrow slots near the root
	// mean the bound gets tested against a real incumbent sooner.
	slots := append([]model.Slot(nil), model.Slots...)
	sort.SliceStable(slots, func(i, j int) bool {
		return len(p.Pool[slots[i]]) < len(p.Pool[slots[j]])
	})

	// suffixMax[d] holds, per stat, the sum of the best value obtainable from
	// slots d..end. Adding it to a partial build gives an upper bound on any
	// completion of that build.
	suffixMax := make([]model.StatBlock, len(slots)+1)
	suffixMax[len(slots)] = model.StatBlock{}
	for d := len(slots) - 1; d >= 0; d-- {
		best := suffixMax[d+1].Clone()
		perStat := make(model.StatBlock)
		for _, a := range p.Pool[slots[d]] {
			for k, v := range p.Stats[a.ID] {
				if v > perStat[k] {
					perStat[k] = v
				}
			}
		}
		best.AddInto(perStat)
		suffixMax[d] = best
	}

	// Sort each slot's candidates by their standalone objective value. Good
	// incumbents early make the bound bite harder for the rest of the run.
	for _, slot := range slots {
		pool := p.Pool[slot]
		sort.SliceStable(pool, func(i, j int) bool {
			return obj(p.Stats[pool[i].ID]) > obj(p.Stats[pool[j].ID])
		})
	}

	res := Result{Complete: true}
	keeper := newTopN(topN)
	current := make([]model.Artifact, len(slots))

	var walk func(depth int, acc model.StatBlock) error
	walk = func(depth int, acc model.StatBlock) error {
		if err := ctx.Err(); err != nil {
			res.Complete = false
			return err
		}
		if depth == len(slots) {
			res.Visited++
			stats := p.Fixed.Add(p.SetBonus).Add(acc)
			if !satisfies(stats, p.Constraints) {
				return nil
			}
			pieces := append([]model.Artifact(nil), current...)
			if p.Valid != nil && !p.Valid(pieces) {
				return nil
			}
			keeper.offer(Build{Pieces: pieces, Stats: stats, Score: obj(stats)})
			return nil
		}

		// Upper bound: this partial build plus the best conceivable
		// completion. If that cannot beat the worst kept build, no descendant
		// can either.
		if keeper.full() {
			optimistic := p.Fixed.Add(p.SetBonus).Add(acc).Add(suffixMax[depth])
			if obj(optimistic) <= keeper.worst() {
				res.Pruned++
				return nil
			}
		}

		for _, a := range p.Pool[slots[depth]] {
			current[depth] = a
			next := acc.Add(p.Stats[a.ID])
			if err := walk(depth+1, next); err != nil {
				return err
			}
		}
		return nil
	}

	err := walk(0, model.StatBlock{})
	res.Builds = keeper.sorted()
	if err != nil {
		// Partial results still travel with the error: a cancelled search
		// has usually found something worth showing, and the caller decides
		// whether "best so far" is good enough.
		return res, err
	}
	return res, nil
}

func satisfies(stats model.StatBlock, cs []Constraint) bool {
	for _, c := range cs {
		if stats[c.Stat] < c.Min {
			return false
		}
	}
	return true
}

// topN keeps the best n builds seen. It is a plain sorted slice: n is a
// handful in practice, so a heap would cost more in complexity than it saves.
type topN struct {
	n     int
	items []Build
}

func newTopN(n int) *topN { return &topN{n: n, items: make([]Build, 0, n+1)} }

func (t *topN) full() bool { return len(t.items) >= t.n }

func (t *topN) worst() float64 {
	if len(t.items) == 0 {
		return 0
	}
	return t.items[len(t.items)-1].Score
}

func (t *topN) offer(b Build) {
	if t.full() && b.Score <= t.worst() {
		return
	}
	i := sort.Search(len(t.items), func(i int) bool { return t.items[i].Score < b.Score })
	t.items = append(t.items, Build{})
	copy(t.items[i+1:], t.items[i:])
	t.items[i] = b
	if len(t.items) > t.n {
		t.items = t.items[:t.n]
	}
}

func (t *topN) sorted() []Build { return t.items }
