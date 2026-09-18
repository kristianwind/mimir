package advisor

import (
	"context"
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/model"
)

// valueState is a build on the plan snapshot's Tester, whose talents all
// scale on ATK and whose ascension stat is crit damage. That shape is what
// makes the assertions below facts rather than taste: a character who scales
// on ATK must value an ATK roll and must not value a DEF roll, whatever the
// engine does in between.
func valueState(t *testing.T, subs model.StatBlock) State {
	t.Helper()
	snap := planSnapshot()

	stats := map[int64]model.StatBlock{
		1: {model.HP: 4780},
		2: {model.ATK: 311},
		3: {model.ATKPercent: 0.466},
		4: {model.ATKPercent: 0.466},
		5: {model.CritRate: 0.311},
	}
	l := Loadout{
		Character: model.Character{
			Key: "Tester", Level: 90, Ascension: 6,
			TalentAuto: 9, TalentSkill: 9, TalentBurst: 9,
		},
		Weapon: &model.Weapon{ID: 1, Key: "Starter", Level: 90, Ascension: 6, Refinement: 1},
		Artifacts: []model.Artifact{
			{ID: 1, SlotKey: model.Flower, SetKey: "A", Rarity: 5, Level: 20, MainStat: model.HP},
			{ID: 2, SlotKey: model.Plume, SetKey: "A", Rarity: 5, Level: 20, MainStat: model.ATK},
			{ID: 3, SlotKey: model.Sands, SetKey: "A", Rarity: 5, Level: 20, MainStat: model.ATKPercent},
			{ID: 4, SlotKey: model.Goblet, SetKey: "A", Rarity: 5, Level: 20, MainStat: model.ATKPercent},
			{ID: 5, SlotKey: model.Circlet, SetKey: "A", Rarity: 5, Level: 20, MainStat: model.CritRate},
		},
	}
	s, err := Assemble(snap, l)
	if err != nil {
		t.Fatal(err)
	}
	// Assemble resolves the artifacts from their own substats, which these
	// fixtures do not carry. Overriding the resolved blocks keeps the build
	// deliberate: the test controls exactly what stats are on it.
	s.ArtifactStats = stats
	if subs != nil {
		s.Fixed = s.Fixed.Add(subs)
	}
	return s
}

func valuesByStat(vs []SubstatValue) map[model.Stat]SubstatValue {
	out := make(map[model.Stat]SubstatValue, len(vs))
	for _, v := range vs {
		out[v.Stat] = v
	}
	return out
}

func TestSubstatValuesRankAStatTheCharacterScalesOnAboveOneItIgnores(t *testing.T) {
	snap := planSnapshot()
	vs, err := SubstatValues(context.Background(), snap, valueState(t, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != len(rollable) {
		t.Fatalf("got %d substats, want %d", len(vs), len(rollable))
	}

	by := valuesByStat(vs)
	if by[model.ATKPercent].PerRoll <= 0 {
		t.Errorf("ATK%% is worth %v on a character whose talents scale on ATK; it must buy damage",
			by[model.ATKPercent].PerRoll)
	}
	// Tester's rotation has no DEF scaling anywhere in it, so a DEF roll is
	// not merely a poor choice — it is worth exactly nothing, and saying so
	// is the whole point of measuring rather than repeating a tier list.
	if got := by[model.DEFPercent].PerRoll; got != 0 {
		t.Errorf("DEF%% is worth %v; nothing in this rotation scales on DEF, so it must be 0", got)
	}
	if by[model.DEFPercent].Note == "" {
		t.Error("a stat worth nothing must say why, or it reads as a bug")
	}
	for _, v := range vs {
		t.Logf("%-16s %+.3f%% per roll (%.2f of best) %s", v.Stat, v.PerRoll*100, v.Relative, v.Note)
	}
}

func TestSubstatValuesAreSortedAndRelativeToTheBest(t *testing.T) {
	snap := planSnapshot()
	vs, err := SubstatValues(context.Background(), snap, valueState(t, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(vs); i++ {
		if vs[i-1].PerRoll < vs[i].PerRoll {
			t.Fatalf("not sorted: %s (%v) came before %s (%v)",
				vs[i-1].Stat, vs[i-1].PerRoll, vs[i].Stat, vs[i].PerRoll)
		}
	}
	if vs[0].Relative != 1 {
		t.Errorf("the best roll is %v of the best roll, want 1", vs[0].Relative)
	}
	for _, v := range vs {
		if v.Relative > 1 {
			t.Errorf("%s is worth %v of the best roll, which is more than the best", v.Stat, v.Relative)
		}
	}
}

// The reason this exists rather than a static target from a guide: the answer
// has to change when the build changes. A build already at the crit ceiling
// must not still be told to chase crit rate.
func TestCritRateStopsBeingWorthAnythingAtTheCeiling(t *testing.T) {
	snap := planSnapshot()
	ctx := context.Background()

	lean, err := SubstatValues(ctx, snap, valueState(t, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := valuesByStat(lean)[model.CritRate].PerRoll; got <= 0 {
		t.Fatalf("crit rate is worth %v on a build well short of the ceiling; it must buy damage", got)
	}

	// Enough crit rate that the rotation always crits. Every further roll is
	// spent on a multiplier that has already maxed out.
	capped, err := SubstatValues(ctx, snap, valueState(t, model.StatBlock{model.CritRate: 1.5}), nil)
	if err != nil {
		t.Fatal(err)
	}
	cr := valuesByStat(capped)[model.CritRate]
	if cr.PerRoll != 0 {
		t.Errorf("crit rate is worth %v at the ceiling, want 0", cr.PerRoll)
	}
	if cr.Note == "" {
		t.Error("crit rate worth nothing at the ceiling must explain itself; a guide saying 70%% would not")
	}
	// And the advice has to have moved on to something else.
	if capped[0].Stat == model.CritRate {
		t.Error("crit rate is still the top recommendation on a build that cannot use it")
	}
	t.Logf("at the ceiling the best roll is now %s (%+.3f%% per roll)",
		capped[0].Stat, capped[0].PerRoll*100)
}

func TestGradeSubstatsPairsAPiecesRollsWithWhatTheyAreWorth(t *testing.T) {
	values := []SubstatValue{
		{Stat: model.CritDMG, PerRoll: 0.04, Relative: 1},
		{Stat: model.CritRate, PerRoll: 0.03, Relative: 0.75},
		{Stat: model.HPPercent, PerRoll: 0.002, Relative: 0.05},
	}
	subs := []model.Substat{
		{Key: model.HPPercent, Value: 0.164},
		{Key: model.CritDMG, Value: 0.132},
		{Key: model.DEFPercent, Value: 0.073},
	}

	got := GradeSubstats(subs, values)
	if len(got) != len(subs) {
		t.Fatalf("graded %d substats, want %d", len(got), len(subs))
	}
	if got[0].Stat != model.HPPercent || got[0].Relative != 0.05 {
		t.Errorf("HP%% graded %+v, want relative 0.05", got[0])
	}
	if got[1].Relative != 1 {
		t.Errorf("crit damage graded %v, want 1", got[1].Relative)
	}
	if got[0].Value != 0.164 {
		t.Errorf("the piece's own rolled value was changed to %v", got[0].Value)
	}
	// A stat missing from the table is worth zero, not a panic and not a
	// silently dropped row: the piece still has that substat on it.
	if got[2].Stat != model.DEFPercent || got[2].Relative != 0 {
		t.Errorf("a substat absent from the table graded %+v, want DEF%% at 0", got[2])
	}
}

func TestSubstatValuesRefuseWithoutRollValues(t *testing.T) {
	snap := planSnapshot()
	snap.SubstatRolls = nil
	if _, err := SubstatValues(context.Background(), snap, valueState(t, nil), nil); err == nil {
		t.Fatal("measured a roll with no mined roll values to measure")
	}
}

// The zero that would be a lie if it were left bare. Energy recharge does not
// move the yardstick, because the yardstick is a rotation that never waits for
// energy — but a player reading "energy recharge: 0" without that sentence
// would take it as advice, and it is not advice, it is an unmeasured stat.
func TestEnergyRechargeSaysItIsUnmeasuredRatherThanWorthless(t *testing.T) {
	snap := planSnapshot()
	vs, err := SubstatValues(context.Background(), snap, valueState(t, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	er := valuesByStat(vs)[model.EnergyRecharge]
	if er.PerRoll != 0 {
		t.Skip("the yardstick now models energy; this test's premise is gone")
	}
	if !strings.Contains(er.Note, "not measured") {
		t.Errorf("energy recharge reads as worthless rather than unmeasured: %q", er.Note)
	}
	if strings.Contains(er.Note, "buys nothing") {
		t.Errorf("energy recharge was given the generic does-not-scale note: %q", er.Note)
	}
}
