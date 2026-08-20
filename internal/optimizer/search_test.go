package optimizer

import (
	"context"
	"testing"

	"github.com/kristianwind/mimir/internal/model"
)

// objective is monotone non-decreasing in every stat it reads, which is the
// contract branch-and-bound relies on.
func objective(s model.StatBlock) float64 {
	return s[model.ATK] * (1 + s[model.CritRate]*s[model.CritDMG])
}

// synthetic builds a deterministic pool: pseudo-random but reproducible, so a
// failure is always reproducible from the test name alone.
func synthetic(perSlot int) Problem {
	pool := make(map[model.Slot][]model.Artifact, len(model.Slots))
	stats := make(map[int64]model.StatBlock)
	var id int64
	seed := uint64(1)
	next := func(mod int) int {
		seed = seed*6364136223846793005 + 1442695040888963407
		return int((seed >> 33) % uint64(mod))
	}
	for _, slot := range model.Slots {
		for i := 0; i < perSlot; i++ {
			id++
			a := model.Artifact{ID: id, SlotKey: slot, SetKey: "Test", Rarity: 5, Level: 20}
			stats[id] = model.StatBlock{
				model.ATK:            float64(next(300)),
				model.CritRate:       float64(next(30)) / 100,
				model.CritDMG:        float64(next(60)) / 100,
				model.EnergyRecharge: float64(next(40)) / 100,
			}
			pool[slot] = append(pool[slot], a)
		}
	}
	return Problem{
		Fixed: model.StatBlock{model.ATK: 800, model.CritRate: 0.05, model.CritDMG: 0.50, model.EnergyRecharge: 1.0},
		Pool:  pool,
		Stats: stats,
		TopN:  3,
	}
}

// brute force, for the equivalence check.
func exhaustive(p Problem, obj Objective) float64 {
	var best float64
	pieces := make([]model.Artifact, len(model.Slots))
	var walk func(d int, acc model.StatBlock)
	walk = func(d int, acc model.StatBlock) {
		if d == len(model.Slots) {
			stats := p.Fixed.Add(p.SetBonus).Add(acc)
			if !satisfies(stats, p.Constraints) {
				return
			}
			if p.Valid != nil && !p.Valid(pieces) {
				return
			}
			if s := obj(stats); s > best {
				best = s
			}
			return
		}
		for _, a := range p.Pool[model.Slots[d]] {
			pieces[d] = a
			walk(d+1, acc.Add(p.Stats[a.ID]))
		}
	}
	walk(0, model.StatBlock{})
	return best
}

func TestSearchMatchesBruteForce(t *testing.T) {
	p := synthetic(6)
	got, err := Search(context.Background(), p, objective)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Builds) != 3 {
		t.Fatalf("got %d builds, want 3", len(got.Builds))
	}
	want := exhaustive(p, objective)
	if got.Builds[0].Score != want {
		t.Errorf("best score = %v, brute force says %v", got.Builds[0].Score, want)
	}
	for i := 1; i < len(got.Builds); i++ {
		if got.Builds[i].Score > got.Builds[i-1].Score {
			t.Errorf("builds are not sorted descending at %d", i)
		}
	}
}

func TestSearchRespectsConstraints(t *testing.T) {
	p := synthetic(6)
	p.Constraints = []Constraint{{Stat: model.EnergyRecharge, Min: 2.0}}

	got, err := Search(context.Background(), p, objective)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range got.Builds {
		if b.Stats[model.EnergyRecharge] < 2.0 {
			t.Errorf("build below the ER floor: %v", b.Stats[model.EnergyRecharge])
		}
	}
	if want := exhaustive(p, objective); len(got.Builds) > 0 && got.Builds[0].Score != want {
		t.Errorf("constrained best = %v, brute force says %v", got.Builds[0].Score, want)
	}
}

func TestSearchActuallyPrunes(t *testing.T) {
	p := synthetic(10)
	got, err := Search(context.Background(), p, objective)
	if err != nil {
		t.Fatal(err)
	}
	// 10^5 combinations exist. If the bound were broken we would visit all of
	// them; a pruning run visits a small fraction.
	if got.Pruned == 0 {
		t.Fatal("nothing was pruned — the upper bound is not biting")
	}
	if got.Visited >= 100000 {
		t.Errorf("visited %d complete builds; the search degenerated to brute force", got.Visited)
	}
	t.Logf("visited %d of 100000 complete builds, pruned %d subtrees", got.Visited, got.Pruned)
}

func TestSearchRejectsEmptySlot(t *testing.T) {
	p := synthetic(2)
	delete(p.Pool, model.Goblet)
	if _, err := Search(context.Background(), p, objective); err == nil {
		t.Error("expected an error when a slot has no candidates")
	}
}

func TestSearchHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := Search(ctx, synthetic(4), objective)
	if err == nil {
		t.Fatal("expected the cancelled context to surface")
	}
	if res.Complete {
		t.Error("a cancelled search must not report itself complete")
	}
}

func TestSetConfigAllows(t *testing.T) {
	four := SetConfig{Four: "Emblem"}
	if !four.Allows(map[string]int{"Emblem": 4, "Other": 1}) {
		t.Error("4pc Emblem rejected a build with four Emblem pieces")
	}
	if four.Allows(map[string]int{"Emblem": 3, "Other": 2}) {
		t.Error("4pc Emblem accepted a build with three Emblem pieces")
	}

	split := SetConfig{TwoA: "Emblem", TwoB: "Noblesse"}
	if !split.Allows(map[string]int{"Emblem": 3, "Noblesse": 2}) {
		t.Error("2+2 rejected a valid build")
	}
	if split.Allows(map[string]int{"Emblem": 4, "Noblesse": 1}) {
		t.Error("2+2 accepted a build with only one Noblesse piece")
	}
}

func TestEnumerateSetConfigsNeedsDistinctSlots(t *testing.T) {
	// Five gobles of one set are five pieces and zero four-piece sets.
	var inv []model.Artifact
	for i := 0; i < 5; i++ {
		inv = append(inv, model.Artifact{ID: int64(i + 1), SetKey: "Hoarder", SlotKey: model.Goblet})
	}
	for _, cfg := range EnumerateSetConfigs(inv, 20) {
		if cfg.Four == "Hoarder" {
			t.Error("offered a 4pc from five pieces in one slot")
		}
	}

	// A genuine spread does offer the four-piece.
	inv = nil
	for i, slot := range model.Slots {
		inv = append(inv, model.Artifact{ID: int64(i + 1), SetKey: "Spread", SlotKey: slot})
	}
	var found bool
	for _, cfg := range EnumerateSetConfigs(inv, 20) {
		if cfg.Four == "Spread" {
			found = true
		}
	}
	if !found {
		t.Error("a set covering all five slots was not offered as a 4pc")
	}
}

func TestSearchValidPredicateIsEnforced(t *testing.T) {
	p := synthetic(5)
	// Reject any build using the top-scoring flower, and check the search
	// returns the best build that obeys that rather than nothing.
	banned := p.Pool[model.Flower][0].ID
	p.TopN = 1
	p.Valid = func(pieces []model.Artifact) bool {
		for _, a := range pieces {
			if a.ID == banned {
				return false
			}
		}
		return true
	}

	got, err := Search(context.Background(), p, objective)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Builds) == 0 {
		t.Fatal("no build satisfied the predicate")
	}
	for _, a := range got.Builds[0].Pieces {
		if a.ID == banned {
			t.Error("the search returned a build the predicate rejects")
		}
	}
	want := exhaustive(p, objective)
	if got.Builds[0].Score != want {
		t.Errorf("best valid score = %v, brute force says %v", got.Builds[0].Score, want)
	}
}
