package model

// Which stats actually reach the damage engine.
//
// Every stat in this package is a real thing in the game. Not every one of
// them is a thing Mimir computes with, and the gap between those two is
// invisible from inside a stat block: a bonus to a mechanic nothing models
// lands in the map, adds up correctly, and changes no number anywhere.
//
// That gap became load-bearing when the effect file started growing. A rule
// is what makes a set count as modelled, and a rule granting only inert stats
// would make the interface stop warning that a set is ranked on its stats
// alone — while the set's bonus stayed exactly as absent as before. The count
// would improve and the advice would not, which is the one thing this
// codebase is not allowed to do.
//
// Crimson Witch is the live example and the reason this exists. Its rule
// grants vaporize and melt, which the engine applies, and also overloaded,
// burning and burgeon, which it does not: calc.Damage reads Instance.Amplify
// and nothing else, and calc.Transformative — the function that would price
// an overload — is exported, tested, and called from nowhere. Three of those
// six grants have never moved a number.
//
// So: a claim about the engine, verified against the engine. TestReachAgrees
// WithTheDamageEngine in internal/calc perturbs each stat below and asserts
// the damage moves exactly when this says it should. If somebody teaches the
// engine to compute overloads, that test fails until this file is updated,
// which is the right direction for the failure to point.

import "strings"

// AmplifyingReactions are the two reaction families calc.Amplifying prices.
// A bonus to either changes damage; a bonus to any other reaction does not,
// because no rotation produces one.
var AmplifyingReactions = []string{"vaporize", "melt"}

// ReachesDamage reports whether a bonus to this stat can change a damage
// number as the engine stands today.
//
// False is not a judgement about the game — energy recharge matters enormously
// and healing bonus keeps people alive. It means only that this particular
// number, added to a build, moves nothing that Mimir currently computes, and
// therefore that a recommendation resting on it rests on nothing.
func ReachesDamage(s Stat) bool {
	switch s {
	// Scaling stats: read through calc.Scaling.
	case HP, HPPercent, ATK, ATKPercent, DEF, DEFPercent:
		return true
	// Mastery feeds the amplifying term, so it moves damage on any
	// instance carrying a reaction.
	case ElementalMastery:
		return true
	// Crit, and the target's defences.
	case CritRate, CritDMG, TargetDefIgnore, TargetDefReduction:
		return true
	// Every damage bonus the engine adds up in calc.Damage.
	case AllDMG, NormalDMG, ChargedDMG, PlungeDMG, SkillDMG, BurstDMG,
		PyroDMG, HydroDMG, AnemoDMG, ElectroDMG, DendroDMG, CryoDMG, GeoDMG,
		PhysicalDMG:
		return true

	// Energy recharge is the interesting false.
	//
	// It reaches damage only indirectly, through effects that convert it —
	// Emblem's burst bonus, Raiden's passive — and those conversions are
	// themselves effects whose grants are checked here. The stat on its own
	// changes nothing, because the yardstick rotation is one skill and one
	// burst and never waits for energy. Returning true would let a set be
	// called modelled on the strength of a recharge bonus alone.
	case EnergyRecharge:
		return false
	case HealingBonus:
		return false
	}

	// Category-scoped crit: "burst_critRate_" and friends, which The Catch
	// already uses and calc.Damage reads per instance.
	for _, c := range []Category{CategoryNormal, CategoryCharged, CategoryPlunge, CategorySkill, CategoryBurst} {
		if s == CategoryScoped(c, CritRate) || s == CategoryScoped(c, CritDMG) {
			return true
		}
	}

	// Resistance shred on the target, one key per element.
	for _, e := range []Element{Pyro, Hydro, Anemo, Electro, Dendro, Cryo, Geo, Physical} {
		if s == TargetResShred(e) {
			return true
		}
	}

	// Reaction bonuses: only the amplifying families are priced. Everything
	// else — overloaded, bloom, hyperbloom, burgeon, superconduct, aggravate,
	// burning — needs calc.Transformative, which no rotation calls.
	if strings.HasPrefix(string(s), "react_") {
		for _, r := range AmplifyingReactions {
			if s == ReactionBonus(r) {
				return true
			}
		}
		return false
	}

	return false
}
