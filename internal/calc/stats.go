// Package calc is the damage engine: formulas, no game constants.
//
// Every version-dependent number (talent multipliers, base stats, reaction
// coefficients, level multipliers) arrives as an argument from package
// gamedata. That separation is what makes the engine unit-testable against
// the KQM community spreadsheets: a failing test means the formula is wrong,
// never that a patch moved a constant.
//
// No LLM ever computes a number here. The AI layer calls this engine as a
// tool and explains what it returns — see internal/ai.
package calc

import "github.com/kristianwind/mimir/internal/model"

// Base holds a character's white (pre-bonus) HP, ATK and DEF. Weapon base ATK
// is already folded into ATK, matching the in-game character sheet.
type Base struct {
	HP  float64
	ATK float64
	DEF float64
}

// Resolve turns base stats plus a bag of bonuses into final totals.
//
//	total = base * (1 + percent) + flat
//
// Non-scaling stats (crit, EM, ER, DMG bonuses) pass through untouched.
// Crit rate is *not* clamped here — the optimizer needs to see overcapped
// crit rate to know a build is wasting substats.
func Resolve(base Base, bonuses model.StatBlock) model.StatBlock {
	out := bonuses.Clone()

	out[model.HP] = base.HP*(1+bonuses[model.HPPercent]) + bonuses[model.HP]
	out[model.ATK] = base.ATK*(1+bonuses[model.ATKPercent]) + bonuses[model.ATK]
	out[model.DEF] = base.DEF*(1+bonuses[model.DEFPercent]) + bonuses[model.DEF]

	// Percent keys are consumed; leaving them in invites double-counting when
	// a caller re-resolves an already-resolved block.
	delete(out, model.HPPercent)
	delete(out, model.ATKPercent)
	delete(out, model.DEFPercent)

	return out
}

// Scaling returns the value of the stat an ability scales off.
func Scaling(totals model.StatBlock, stat model.Stat) float64 {
	switch stat {
	case model.HP, model.HPPercent:
		return totals[model.HP]
	case model.DEF, model.DEFPercent:
		return totals[model.DEF]
	case model.ElementalMastery:
		// Real, not exotic: Nahida's skill and every dendro-core character
		// scale a damage instance directly off elemental mastery.
		return totals[model.ElementalMastery]
	default:
		return totals[model.ATK]
	}
}

// CritMultipliers returns the non-crit, crit and expected-value multipliers.
//
// The average is what every ranking in Mimir sorts on: crit rate above 100%
// is wasted, so it is clamped here even though Resolve leaves it visible.
func CritMultipliers(totals model.StatBlock) (nonCrit, crit, average float64) {
	cr := totals[model.CritRate]
	cd := totals[model.CritDMG]
	if cr < 0 {
		cr = 0
	}
	if cr > 1 {
		cr = 1
	}
	return 1, 1 + cd, 1 + cr*cd
}
