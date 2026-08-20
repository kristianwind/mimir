package calc

import (
	"math"
	"testing"

	"github.com/kristianwind/mimir/internal/model"
)

func near(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

func TestResolveFoldsPercentAndFlat(t *testing.T) {
	base := Base{HP: 15000, ATK: 800, DEF: 700}
	got := Resolve(base, model.StatBlock{
		model.ATKPercent: 0.90,
		model.ATK:        311,
		model.HPPercent:  0.20,
		model.CritRate:   0.60,
	})
	near(t, "ATK", got[model.ATK], 800*1.9+311)
	near(t, "HP", got[model.HP], 15000*1.2)
	near(t, "DEF", got[model.DEF], 700)
	near(t, "CritRate", got[model.CritRate], 0.60)

	if _, ok := got[model.ATKPercent]; ok {
		t.Error("ATK% survived Resolve; re-resolving would double-count it")
	}
}

func TestDefenseMultiplier(t *testing.T) {
	tgt := Target{Level: 90}
	// Equal levels, no shred: exactly half the damage is eaten by DEF.
	near(t, "even levels", tgt.DefenseMultiplier(90), 0.5)

	// 100% shred+ignore would divide by zero without the clamp.
	full := Target{Level: 90, DefReduction: 1}
	near(t, "full shred", full.DefenseMultiplier(90), 1.0)

	// Shred is worth more against higher-level enemies.
	hi := Target{Level: 103, DefReduction: 0.20}
	want := 190.0 / (190.0 + 203.0*0.8)
	near(t, "shredded lvl 103", hi.DefenseMultiplier(90), want)
}

func TestResistanceMultiplierBranches(t *testing.T) {
	std := Target{Resistance: map[model.Element]float64{model.Pyro: 0.10}}
	near(t, "10% RES", std.ResistanceMultiplier(model.Pyro), 0.90)

	// Shredded below zero: the negative branch halves the benefit, which is
	// why stacking shred past 0% RES has sharply diminishing value.
	shred := Target{
		Resistance:   map[model.Element]float64{model.Pyro: 0.10},
		ResReduction: map[model.Element]float64{model.Pyro: 0.50},
	}
	near(t, "-40% RES", shred.ResistanceMultiplier(model.Pyro), 1.20)

	high := Target{Resistance: map[model.Element]float64{model.Cryo: 0.90}}
	near(t, "90% RES", high.ResistanceMultiplier(model.Cryo), 1/(1+4*0.90))
}

func TestCritMultipliersClampRate(t *testing.T) {
	_, crit, avg := CritMultipliers(model.StatBlock{
		model.CritRate: 1.30,
		model.CritDMG:  2.00,
	})
	near(t, "crit", crit, 3.0)
	// Overcapped crit rate must not inflate the average — that is how a
	// build ranking learns that the 11th crit-rate roll is wasted.
	near(t, "average", avg, 3.0)

	_, _, half := CritMultipliers(model.StatBlock{
		model.CritRate: 0.50,
		model.CritDMG:  1.00,
	})
	near(t, "50/100", half, 1.5)
}

func TestEMTermsAreAsymptotic(t *testing.T) {
	near(t, "amplify at 1400 EM", EMAmplifyBonus(1400), 1.39)
	near(t, "transformative at 2000 EM", EMTransformativeBonus(2000), 8.0)
	near(t, "crystallize at 1400 EM", EMCrystallizeBonus(1400), 2.22)
	near(t, "zero EM", EMAmplifyBonus(0), 0)

	// Diminishing returns: doubling EM from 200 to 400 adds less than the
	// first 200 did.
	first := EMAmplifyBonus(200)
	second := EMAmplifyBonus(400) - first
	if second >= first {
		t.Errorf("EM bonus is not diminishing: first 200 = %v, next 200 = %v", first, second)
	}
}

func TestAmplifyingMultiplier(t *testing.T) {
	stats := model.StatBlock{model.ElementalMastery: 0}
	near(t, "forward vape, 0 EM", VaporizeHydro.Multiplier(0, stats), 2.0)
	near(t, "reverse vape, 0 EM", VaporizePyro.Multiplier(0, stats), 1.5)
	near(t, "no reaction", NoAmplify.Multiplier(1000, stats), 1.0)

	// Crimson Witch's +15% vaporize is additive with the EM term.
	buffed := model.StatBlock{model.ReactionBonus("vaporize"): 0.15}
	near(t, "buffed vape", VaporizeHydro.Multiplier(0, buffed), 2.0*1.15)

	// A melt bonus must not leak into vaporize.
	meltOnly := model.StatBlock{model.ReactionBonus("melt"): 0.40}
	near(t, "melt bonus on vape", VaporizeHydro.Multiplier(0, meltOnly), 2.0)
}

func TestDamageEndToEnd(t *testing.T) {
	// Hand-computed: 2000 ATK, 300% talent, +46.6% pyro, 70/140 crit,
	// level 90 vs level 90 at 10% pyro RES, no reaction.
	totals := Resolve(Base{ATK: 2000}, model.StatBlock{
		model.PyroDMG:  0.466,
		model.CritRate: 0.70,
		model.CritDMG:  1.40,
	})
	tgt := Target{Level: 90, Resistance: map[model.Element]float64{model.Pyro: 0.10}}

	got := Damage(90, totals, Instance{
		Label:      "Burst",
		Element:    model.Pyro,
		Scaling:    model.ATK,
		Multiplier: 3.00,
	}, tgt)

	want := 2000 * 3.00 * 1.466 * 0.5 * 0.9
	near(t, "non-crit", got.NonCrit, want)
	near(t, "crit", got.Crit, want*2.4)
	near(t, "average", got.Average, want*(1+0.7*1.4))
}

func TestDamageInstanceExtraDoesNotLeak(t *testing.T) {
	totals := Resolve(Base{ATK: 1000}, nil)
	tgt := Target{Level: 90}
	inst := Instance{
		Element:    model.Pyro,
		Scaling:    model.ATK,
		Multiplier: 1.0,
		Extra:      model.StatBlock{model.CritRate: 1.0, model.CritDMG: 1.0},
	}

	withExtra := Damage(90, totals, inst, tgt)
	near(t, "extra applied", withExtra.Average, 1000*0.5*2.0)

	// The same totals reused for a second instance must be unbuffed.
	inst.Extra = nil
	plain := Damage(90, totals, inst, tgt)
	near(t, "extra did not leak", plain.Average, 1000*0.5)
}

func TestTransformativeRequiresSyncedConstants(t *testing.T) {
	stats := model.StatBlock{model.ElementalMastery: 2000}
	tgt := Target{}

	// A missing level multiplier is an error, never a guess: reporting a
	// fabricated hyperbloom number would poison every ranking downstream.
	if _, err := Transformative("hyperbloom", 0, 3.0, model.Dendro, stats, tgt); err == nil {
		t.Error("expected an error when the level multiplier is unsynced")
	}
	if _, err := Transformative("hyperbloom", 1446.85, 0, model.Dendro, stats, tgt); err == nil {
		t.Error("expected an error when the coefficient is unsynced")
	}

	got, err := Transformative("hyperbloom", 1000, 3.0, model.Dendro, stats, tgt)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "hyperbloom", got, 1000*3.0*(1+8.0))
}

func TestScalingPicksTheRightStat(t *testing.T) {
	totals := model.StatBlock{model.HP: 40000, model.ATK: 1200, model.DEF: 900, model.ElementalMastery: 800}
	near(t, "em scaling", Scaling(totals, model.ElementalMastery), 800)
	near(t, "hp scaling", Scaling(totals, model.HPPercent), 40000)
	near(t, "def scaling", Scaling(totals, model.DEF), 900)
	near(t, "default", Scaling(totals, model.ATK), 1200)
}

func TestDamageBonusesAreAdditiveAcrossSources(t *testing.T) {
	totals := Resolve(Base{ATK: 1000}, model.StatBlock{
		model.ElectroDMG: 0.20,
		model.AllDMG:     0.10,
		model.BurstDMG:   0.30,
		model.NormalDMG:  0.99, // must not apply to a burst instance
	})
	tgt := Target{Level: 90}

	burst := Damage(90, totals, Instance{
		Element: model.Electro, Category: model.CategoryBurst,
		Scaling: model.ATK, Multiplier: 1.0,
	}, tgt)
	near(t, "burst bonus", burst.DMGBonus, 0.60)

	// Raiden's Musou Isshin swings are normal attacks that happen to occur
	// during her burst. Charging them the burst bonus is the classic way to
	// overstate her by tens of percent.
	normal := Damage(90, totals, Instance{
		Element: model.Electro, Category: model.CategoryNormal,
		Scaling: model.ATK, Multiplier: 1.0,
	}, tgt)
	near(t, "normal bonus", normal.DMGBonus, 0.20+0.10+0.99)

	none := Damage(90, totals, Instance{
		Element: model.Electro, Scaling: model.ATK, Multiplier: 1.0,
	}, tgt)
	near(t, "uncategorised bonus", none.DMGBonus, 0.30)
}

func TestCategoryScopedCritOnlyAppliesToItsCategory(t *testing.T) {
	// The Catch at R5: +32% burst DMG, +12% burst crit rate. Neither may
	// reach a normal attack.
	totals := Resolve(Base{ATK: 1000}, model.StatBlock{
		model.CritRate: 0.50,
		model.CritDMG:  1.00,
		model.CategoryScoped(model.CategoryBurst, model.CritRate): 0.12,
	})
	tgt := Target{Level: 90}

	burst := Damage(90, totals, Instance{
		Element: model.Electro, Category: model.CategoryBurst,
		Scaling: model.ATK, Multiplier: 1.0,
	}, tgt)
	near(t, "burst crit rate", burst.CritRate, 0.62)
	near(t, "burst average", burst.Average, 1000*0.5*(1+0.62*1.0))

	normal := Damage(90, totals, Instance{
		Element: model.Electro, Category: model.CategoryNormal,
		Scaling: model.ATK, Multiplier: 1.0,
	}, tgt)
	near(t, "normal crit rate", normal.CritRate, 0.50)
}

func TestCategoryScopedCritStillClamps(t *testing.T) {
	totals := Resolve(Base{ATK: 1000}, model.StatBlock{
		model.CritRate: 0.95,
		model.CritDMG:  2.00,
		model.CategoryScoped(model.CategoryBurst, model.CritRate): 0.30,
	})
	got := Damage(90, totals, Instance{
		Element: model.Electro, Category: model.CategoryBurst,
		Scaling: model.ATK, Multiplier: 1.0,
	}, Target{Level: 90})
	near(t, "clamped", got.CritRate, 1.0)
	near(t, "average equals crit", got.Average, got.Crit)
}

func TestWithDebuffsMovesShredOntoTheTarget(t *testing.T) {
	base := Target{
		Level:      100,
		Resistance: map[model.Element]float64{model.Anemo: 0.10, model.Pyro: 0.10},
	}
	stats := model.StatBlock{
		model.TargetResShred(model.Pyro): 0.40,
		model.TargetDefReduction:         0.20,
	}

	got := base.WithDebuffs(stats)
	near(t, "shredded pyro", got.ResistanceMultiplier(model.Pyro), 1-(0.10-0.40)/2)
	// An element nobody shredded must be untouched.
	near(t, "untouched anemo", got.ResistanceMultiplier(model.Anemo), 0.90)
	near(t, "def reduction", got.DefReduction, 0.20)

	// The original must not be mutated — the same goal target is reused for
	// every candidate build in a search.
	near(t, "original untouched", base.ResistanceMultiplier(model.Pyro), 0.90)
	if base.ResReduction != nil {
		t.Error("WithDebuffs wrote back into the caller's target")
	}
}

func TestWithDebuffsClampsShred(t *testing.T) {
	got := Target{Level: 90}.WithDebuffs(model.StatBlock{
		model.TargetDefReduction: 0.9,
		model.TargetDefIgnore:    0.9,
	})
	near(t, "def reduction clamped", got.DefReduction, 0.9)
	near(t, "def ignore clamped", got.DefIgnore, 0.9)

	over := Target{Level: 90}.WithDebuffs(model.StatBlock{model.TargetDefReduction: 1.6})
	near(t, "over-shred clamped to full", over.DefReduction, 1.0)
	near(t, "full shred removes DEF", over.DefenseMultiplier(90), 1.0)
}
