package advisor

import (
	"context"
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

func targetRequest(t *testing.T) TargetRequest {
	t.Helper()
	req := planRequest(t)
	snap := req.Snapshot
	snap.ArtifactRolls = map[int]int{5: 9}
	snap.SubstatRolls = map[int]map[model.Stat][]float64{
		5: {
			model.CritRate:   {0.027, 0.031, 0.035, 0.039},
			model.CritDMG:    {0.054, 0.062, 0.070, 0.078},
			model.ATKPercent: {0.041, 0.047, 0.052, 0.058},
		},
	}
	// Two sets exist, and only one of them is dropped by a domain.
	snap.ArtifactSets["Unfarmable"] = gamedata.ArtifactSet{
		Key: "Unfarmable", Name: "Unfarmable",
		// A crit-rate two-piece, so it would win on the numbers if it were
		// allowed to compete at all.
		TwoPiece: model.StatBlock{model.CritRate: 0.12},
	}
	return TargetRequest{Snapshot: snap, Character: req.Loadout.Character}
}

// A target build is only useful if it names things you can farm. The rarities
// recorded on a set do not say that — they list every rarity the set has
// pieces for — so the filter is which domain drops it.
func TestOnlyFarmableSetsAreRecommended(t *testing.T) {
	got, err := BuildTarget(context.Background(), targetRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sets) == 0 {
		t.Fatal("no set was recommended")
	}
	for _, s := range got.Sets {
		if s.Config == "Unfarmable" {
			t.Errorf("recommended %q, which no domain drops", s.Config)
		}
	}
}

// Every slot's main stat has to be one the game can actually roll there. A
// circlet with energy recharge on it is a build that does not exist.
func TestMainStatsAreOnesTheSlotCanRoll(t *testing.T) {
	got, err := BuildTarget(context.Background(), targetRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	for slot, stat := range got.MainStats {
		if !mainStatAllowed(slot, stat) {
			t.Errorf("%s was given %s, which it cannot roll", slot, stat)
		}
	}
	for _, slot := range targetSlots {
		if got.MainStats[slot] == "" {
			t.Errorf("%s has no main stat", slot)
		}
	}
	// Flower and plume are not decisions and must not be offered as ones.
	for _, slot := range []model.Slot{model.Flower, model.Plume} {
		if _, ok := got.MainStats[slot]; ok {
			t.Errorf("%s was offered a choice the game does not have", slot)
		}
	}
}

// Weapons are deliberately not ranked, and the answer has to say so. A
// ranking built on base attack and substat alone puts a four-star above a
// five-star, because the passive is most of what makes a weapon good and the
// passives are not mined as numbers.
func TestWeaponsAreNotRankedAndItSaysSo(t *testing.T) {
	got, err := BuildTarget(context.Background(), targetRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	var admitted bool
	for _, c := range got.Caveats {
		if strings.Contains(c, "Weapons are not ranked") {
			admitted = true
		}
	}
	if !admitted {
		t.Errorf("the caveats do not say weapons are unranked: %v", got.Caveats)
	}
}

// The substat allocation is the one part of a target that is not a fact about
// the game, so it travels with the answer instead of being applied silently.
func TestTheSubstatAllocationIsReported(t *testing.T) {
	got, err := BuildTarget(context.Background(), targetRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Substats) == 0 {
		t.Fatal("the allocation every candidate was given is not reported")
	}
	total := 0
	for _, n := range got.Substats {
		total += n
	}
	if want := 9 * len(model.Slots); total != want {
		t.Errorf("allocated %d rolls, want the %d a five-piece set gains", total, want)
	}
	if got.Substats[model.CritDMG] <= got.Substats[model.CritRate] {
		t.Errorf("crit damage got %d rolls against crit rate's %d; the ratio is the wrong way round",
			got.Substats[model.CritDMG], got.Substats[model.CritRate])
	}
}

// Without the roll count there is no consistent treatment to compare
// candidates under, and a ranking would be measuring nothing.
func TestATargetRefusesWithoutTheRollCount(t *testing.T) {
	req := targetRequest(t)
	req.Snapshot.ArtifactRolls = nil

	_, err := BuildTarget(context.Background(), req)
	if err == nil {
		t.Fatal("a target was built with no substat treatment at all")
	}
	if !strings.Contains(err.Error(), "roll") {
		t.Errorf("the error does not name what is missing: %v", err)
	}
}

// The ordering is the answer's whole shape, so the winner has to be the
// winner: nothing may be listed above something that scored higher.
func TestSetsAreOrderedByScore(t *testing.T) {
	got, err := BuildTarget(context.Background(), targetRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(got.Sets); i++ {
		if got.Sets[i].Score > got.Sets[i-1].Score {
			t.Fatalf("set %d scored above the one before it", i)
		}
		if got.Sets[i].Behind < got.Sets[i-1].Behind {
			t.Errorf("the gap to the best shrinks as the list goes down")
		}
	}
	if len(got.Sets) > 0 && got.Sets[0].Behind != 0 {
		t.Errorf("the winner is %v behind itself", got.Sets[0].Behind)
	}
}
