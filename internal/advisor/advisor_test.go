package advisor

import (
	"context"
	"math"
	"testing"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

func TestRankPutsFreeActionsFirst(t *testing.T) {
	got := Rank([]Action{
		{Kind: KindFarm, Subject: "Emblem", GainPct: 0.20, ResinCost: 400},
		{Kind: KindReequip, Subject: "Kazuha", GainPct: 0.01},
		{Kind: KindTalent, Subject: "Bennett", GainPct: 0.004, ResinCost: 60},
	})
	if got[0].Kind != KindReequip {
		t.Errorf("first action is %q; a free +1%% must outrank a paid +20%%", got[0].Kind)
	}
	if !got[0].Free {
		t.Error("the free action is not flagged free")
	}
	// A sentinel of +Inf would sort correctly and then fail to serialise,
	// which reaches the user as an empty response rather than an error.
	for _, a := range got {
		if math.IsInf(a.Efficiency, 0) || math.IsNaN(a.Efficiency) {
			t.Errorf("efficiency %v is not JSON-encodable", a.Efficiency)
		}
	}
	// 20%/400 resin beats 0.4%/60 resin.
	if got[1].Subject != "Emblem" {
		t.Errorf("second action is %q, want the farm", got[1].Subject)
	}
}

func TestRankDemotesBlockedActions(t *testing.T) {
	got := Rank([]Action{
		{Kind: KindTalent, Subject: "Nahida", GainPct: 0.30, ResinCost: 60, BlockedBy: "needs a Crown of Insight"},
		{Kind: KindTalent, Subject: "Furina", GainPct: 0.02, ResinCost: 60},
	})
	if got[0].Subject != "Furina" {
		t.Errorf("first action is %q; a blocked action cannot lead the plan", got[0].Subject)
	}
}

func TestGain(t *testing.T) {
	if got := Gain(1000, 1062); math.Abs(got-0.062) > 1e-9 {
		t.Errorf("Gain = %v, want 0.062", got)
	}
	if got := Gain(0, 100); got != 0 {
		t.Errorf("Gain from a zero baseline = %v, want 0", got)
	}
}

// sumEvaluator scores a build by a monotone weighted sum, so the simulation
// can be tested without dragging the whole damage engine and a full gamedata
// snapshot into the fixture.
type sumEvaluator struct{}

func (sumEvaluator) Score(_ context.Context, _ Goal, s State) (float64, error) {
	total := 1000.0
	for _, a := range s.Equipped {
		st := s.ArtifactStats[a.ID]
		total += st[model.ATK] + 1000*st[model.CritRate] + 500*st[model.CritDMG]
	}
	return total, nil
}

func simSnapshot() *gamedata.Snapshot {
	return &gamedata.Snapshot{
		Version: "test",
		DropModel: gamedata.DropModel{
			PiecesPerRun:      1.07,
			FiveStarChance:    0.435,
			FourSubstatChance: 0.20,
			RollsToMax:        9,
			SlotWeights: map[model.Slot]float64{
				model.Flower: 1, model.Plume: 1, model.Sands: 1, model.Goblet: 1, model.Circlet: 1,
			},
			MainStatWeights: map[model.Slot]map[model.Stat]float64{
				model.Flower:  {model.HP: 1},
				model.Plume:   {model.ATK: 1},
				model.Sands:   {model.ATKPercent: 26.68, model.HPPercent: 26.68, model.EnergyRecharge: 10},
				model.Goblet:  {model.ATKPercent: 19.25, model.PyroDMG: 5},
				model.Circlet: {model.ATKPercent: 22, model.CritRate: 10, model.CritDMG: 10},
			},
			SubstatWeights: map[model.Stat]float64{
				model.HP: 6, model.ATK: 6, model.DEF: 6,
				model.HPPercent: 4, model.ATKPercent: 4, model.DEFPercent: 4,
				model.ElementalMastery: 4, model.EnergyRecharge: 4,
				model.CritRate: 3, model.CritDMG: 3,
			},
		},
		SubstatRolls: map[int]map[model.Stat][]float64{
			5: {
				model.HP:               {209, 239, 269, 299},
				model.ATK:              {14, 16, 18, 19},
				model.DEF:              {16, 19, 21, 23},
				model.HPPercent:        {0.041, 0.047, 0.052, 0.058},
				model.ATKPercent:       {0.041, 0.047, 0.052, 0.058},
				model.DEFPercent:       {0.051, 0.058, 0.065, 0.073},
				model.ElementalMastery: {16, 19, 21, 23},
				model.EnergyRecharge:   {0.045, 0.052, 0.058, 0.065},
				model.CritRate:         {0.027, 0.031, 0.035, 0.039},
				model.CritDMG:          {0.054, 0.062, 0.070, 0.078},
			},
		},
		MainStatValues: map[int]map[model.Stat][]float64{
			5: {
				model.HP:             fill(4780),
				model.ATK:            fill(311),
				model.ATKPercent:     fill(0.466),
				model.HPPercent:      fill(0.466),
				model.EnergyRecharge: fill(0.518),
				model.PyroDMG:        fill(0.466),
				model.CritRate:       fill(0.311),
				model.CritDMG:        fill(0.622),
			},
		},
	}
}

// fill returns a 21-entry curve whose +20 value is v. Only index 20 is used
// by the simulator, which always rolls fully upgraded pieces.
func fill(v float64) []float64 {
	out := make([]float64, 21)
	out[20] = v
	return out
}

func baseState() State {
	// A deliberately mediocre starting build, so there is headroom to find.
	stats := map[int64]model.StatBlock{
		1: {model.HP: 4780},
		2: {model.ATK: 311},
		3: {model.ATKPercent: 0.466},
		4: {model.ATKPercent: 0.466},
		5: {model.CritRate: 0.311},
	}
	return State{
		Character: model.Character{Key: "Test", Level: 90},
		Equipped: []model.Artifact{
			{ID: 1, SlotKey: model.Flower, SetKey: "A"},
			{ID: 2, SlotKey: model.Plume, SetKey: "A"},
			{ID: 3, SlotKey: model.Sands, SetKey: "A"},
			{ID: 4, SlotKey: model.Goblet, SetKey: "A"},
			{ID: 5, SlotKey: model.Circlet, SetKey: "A"},
		},
		ArtifactStats: stats,
	}
}

func testDomain() gamedata.Domain {
	return gamedata.Domain{Key: "test_domain", Kind: "artifact", Sets: []string{"A", "B"}, ResinCost: 20}
}

func TestFarmEstimateIsReproducible(t *testing.T) {
	sim := FarmSim{Snapshot: simSnapshot(), Trials: 60, Seed: 7}
	a, err := sim.Estimate(context.Background(), Goal{}, baseState(), testDomain(), 20, sumEvaluator{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := sim.Estimate(context.Background(), Goal{}, baseState(), testDomain(), 20, sumEvaluator{})
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("same seed gave different estimates:\n%+v\n%+v", a, b)
	}
}

func TestFarmEstimateGrowsWithRunsAndDiminishes(t *testing.T) {
	sim := FarmSim{Snapshot: simSnapshot(), Trials: 120, Seed: 11}
	ctx := context.Background()

	short, err := sim.Estimate(ctx, Goal{}, baseState(), testDomain(), 10, sumEvaluator{})
	if err != nil {
		t.Fatal(err)
	}
	long, err := sim.Estimate(ctx, Goal{}, baseState(), testDomain(), 40, sumEvaluator{})
	if err != nil {
		t.Fatal(err)
	}
	if long.MeanGain <= short.MeanGain {
		t.Errorf("40 runs gained %v, 10 runs gained %v; more farming must not be worth less",
			long.MeanGain, short.MeanGain)
	}
	// Four times the resin must not buy four times the gain — the whole
	// point of the simulation is to show that farming saturates.
	if long.MeanGain >= 4*short.MeanGain {
		t.Errorf("gain scaled linearly with runs (%v vs %v); the simulation is not saturating",
			long.MeanGain, short.MeanGain)
	}
	if short.ResinCost != 200 {
		t.Errorf("resin cost = %v, want 200", short.ResinCost)
	}
	t.Logf("10 runs: mean %.3f%% (p10 %.3f%%, p90 %.3f%%, nothing gained %.0f%% of the time)",
		short.MeanGain*100, short.P10Gain*100, short.P90Gain*100, short.NoImprovementChance*100)
	t.Logf("40 runs: mean %.3f%% (p10 %.3f%%, p90 %.3f%%, nothing gained %.0f%% of the time)",
		long.MeanGain*100, long.P10Gain*100, long.P90Gain*100, long.NoImprovementChance*100)
}

func TestFarmEstimateNeverGoesBackwards(t *testing.T) {
	sim := FarmSim{Snapshot: simSnapshot(), Trials: 80, Seed: 3}
	got, err := sim.Estimate(context.Background(), Goal{}, baseState(), testDomain(), 15, sumEvaluator{})
	if err != nil {
		t.Fatal(err)
	}
	// A player only equips an upgrade, so no trial may end worse than it
	// started. A negative p10 would mean the swap logic is losing pieces.
	if got.P10Gain < 0 {
		t.Errorf("p10 gain = %v; farming cannot make a build worse", got.P10Gain)
	}
	if got.MedianGain > got.P90Gain {
		t.Errorf("median %v above p90 %v", got.MedianGain, got.P90Gain)
	}
}

func TestFarmEstimateRefusesUnsyncedDropModel(t *testing.T) {
	snap := simSnapshot()
	snap.DropModel = gamedata.DropModel{}
	sim := FarmSim{Snapshot: snap, Trials: 10, Seed: 1}
	if _, err := sim.Estimate(context.Background(), Goal{}, baseState(), testDomain(), 5, sumEvaluator{}); err == nil {
		t.Error("expected an error rather than a fabricated estimate")
	}
}

func TestFarmEstimateRejectsNonArtifactDomain(t *testing.T) {
	sim := FarmSim{Snapshot: simSnapshot(), Trials: 10, Seed: 1}
	d := testDomain()
	d.Kind = "talent"
	if _, err := sim.Estimate(context.Background(), Goal{}, baseState(), d, 5, sumEvaluator{}); err == nil {
		t.Error("a talent domain drops no artifacts; expected an error")
	}
}
