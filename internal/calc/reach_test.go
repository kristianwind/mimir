package calc_test

import (
	"testing"

	"github.com/kristianwind/mimir/internal/calc"
	"github.com/kristianwind/mimir/internal/model"
)

// Does a bonus to this stat actually change a damage number?
//
// model.ReachesDamage answers that question, and effects.json now leans on the
// answer: a rule granting only stats that reach nothing must not make an
// artifact set count as modelled. A claim like that is worth exactly as much
// as its agreement with the engine, so this measures rather than restates it.
//
// The method is blunt on purpose. Take a build, add some of one stat, run the
// same evaluation the advisor runs, and see whether the total moved. No
// knowledge of the formula is encoded here — if calc.Damage learns to price
// overloads tomorrow, this test starts failing and points at the file that
// needs updating.

// reachRotation covers every path a stat could travel down: one instance per
// element so each elemental bonus and each resistance shred has something to
// apply to, categories spread across all five so the category bonuses and the
// category-scoped crit have a home, scalings spread across ATK, HP and DEF,
// and two amplifying reactions so mastery and the reaction bonuses matter.
func reachRotation() calc.Rotation {
	elements := []model.Element{
		model.Pyro, model.Hydro, model.Anemo, model.Electro,
		model.Dendro, model.Cryo, model.Geo, model.Physical,
	}
	categories := []model.Category{
		model.CategoryNormal, model.CategoryCharged, model.CategoryPlunge,
		model.CategorySkill, model.CategoryBurst,
	}
	scalings := []model.Stat{model.ATK, model.HP, model.DEF}

	rot := calc.Rotation{Name: "reach"}
	for i, e := range elements {
		inst := calc.Instance{
			Label:      string(e),
			Element:    e,
			Category:   categories[i%len(categories)],
			Scaling:    scalings[i%len(scalings)],
			Multiplier: 2.0,
		}
		switch e {
		case model.Hydro:
			inst.Amplify = calc.VaporizeHydro
		case model.Pyro:
			inst.Amplify = calc.MeltPyro
		}
		rot.Instances = append(rot.Instances, inst)
	}
	return rot
}

// reachWhite is the character's pre-bonus sheet. It has to exist separately
// from the bonuses, because the percentage stats are consumed by Resolve on
// the way in — atk_ never reaches Evaluate under its own name. A first draft
// of this test skipped Resolve and duly reported hp_, atk_ and def_ as inert,
// which is exactly the kind of wrong answer a confident test produces.
func reachWhite() calc.Base {
	return calc.Base{HP: 15000, ATK: 900, DEF: 700}
}

func reachBase() model.StatBlock {
	return model.StatBlock{
		model.ATK: 600, model.HP: 5000, model.DEF: 200,
		// Crit low enough that adding half a rate still lands under the
		// clamp, or the perturbation would be swallowed and read as "this
		// stat does nothing".
		model.CritRate: 0.10, model.CritDMG: 0.60,
		model.ElementalMastery: 100,
	}
}

// reachTotal runs the three steps the advisor's evaluators run, in order:
// resolve the bonuses against the white sheet, hand the debuff stats to the
// target, evaluate. Each step matters. Skipping Resolve hides the percentage
// stats; skipping WithDebuffs reports the resistance shreds as inert.
func reachTotal(t *testing.T, bonuses model.StatBlock) float64 {
	t.Helper()
	stats := calc.Resolve(reachWhite(), bonuses)
	tgt := calc.Target{
		Level:        90,
		Resistance:   map[model.Element]float64{},
		ResReduction: map[model.Element]float64{},
	}
	for _, e := range []model.Element{
		model.Pyro, model.Hydro, model.Anemo, model.Electro,
		model.Dendro, model.Cryo, model.Geo, model.Physical,
	} {
		tgt.Resistance[e] = 0.10
	}
	res, err := calc.Evaluate(90, stats, reachRotation(), tgt.WithDebuffs(stats))
	if err != nil {
		t.Fatal(err)
	}
	return res.Total
}

func TestReachAgreesWithTheDamageEngine(t *testing.T) {
	// Flat stats need a flat nudge; everything else is a fraction.
	nudge := func(s model.Stat) float64 {
		switch s {
		case model.HP, model.ATK, model.DEF, model.ElementalMastery:
			return 500
		}
		return 0.5
	}

	var candidates []model.Stat
	candidates = append(candidates,
		model.HP, model.HPPercent, model.ATK, model.ATKPercent, model.DEF, model.DEFPercent,
		model.ElementalMastery, model.EnergyRecharge, model.CritRate, model.CritDMG,
		model.HealingBonus, model.AllDMG, model.NormalDMG, model.ChargedDMG,
		model.PlungeDMG, model.SkillDMG, model.BurstDMG,
		model.PyroDMG, model.HydroDMG, model.AnemoDMG, model.ElectroDMG,
		model.DendroDMG, model.CryoDMG, model.GeoDMG, model.PhysicalDMG,
		model.TargetDefIgnore, model.TargetDefReduction,
	)
	for _, c := range []model.Category{
		model.CategoryNormal, model.CategoryCharged, model.CategoryPlunge,
		model.CategorySkill, model.CategoryBurst,
	} {
		candidates = append(candidates,
			model.CategoryScoped(c, model.CritRate),
			model.CategoryScoped(c, model.CritDMG))
	}
	for _, e := range []model.Element{
		model.Pyro, model.Hydro, model.Anemo, model.Electro,
		model.Dendro, model.Cryo, model.Geo, model.Physical,
	} {
		candidates = append(candidates, model.TargetResShred(e))
	}
	// Both amplifying families, and the transformative ones a set bonus
	// would plausibly name. The second group is the point of the exercise.
	for _, r := range []string{
		"vaporize", "melt",
		"overloaded", "burning", "burgeon", "bloom", "hyperbloom",
		"superconduct", "aggravate", "spread", "electrocharged", "shatter",
	} {
		candidates = append(candidates, model.ReactionBonus(r))
	}

	base := reachTotal(t, reachBase())
	if base <= 0 {
		t.Fatalf("the baseline rotation deals %v damage; the fixture is broken", base)
	}

	for _, s := range candidates {
		stats := reachBase()
		stats[s] += nudge(s)
		moved := reachTotal(t, stats) != base
		want := model.ReachesDamage(s)
		if moved != want {
			verb := map[bool]string{true: "changed", false: "did not change"}
			t.Errorf("adding %v of %s %s the damage, but ReachesDamage says %v",
				nudge(s), s, verb[moved], want)
		}
	}
}

// The specific case the check exists for. Crimson Witch's four-piece grants
// both kinds, and until this distinction existed the three inert grants were
// indistinguishable from the three live ones.
func TestTransformativeReactionBonusesReachNothingYet(t *testing.T) {
	for _, r := range []string{"overloaded", "burning", "burgeon", "hyperbloom", "superconduct"} {
		s := model.ReactionBonus(r)
		if model.ReachesDamage(s) {
			t.Errorf("%s is reported as reaching damage; calc.Transformative is still called from nowhere", s)
		}
		stats := reachBase()
		stats[s] += 1.0
		if got := reachTotal(t, stats); got != reachTotal(t, reachBase()) {
			t.Errorf("%s moved the damage after all — the engine has learned something and model/reach.go has not", s)
		}
	}
	for _, r := range model.AmplifyingReactions {
		s := model.ReactionBonus(r)
		if !model.ReachesDamage(s) {
			t.Errorf("%s is reported as inert, but the rotation prices it", s)
		}
	}
}
