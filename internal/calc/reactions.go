package calc

import (
	"fmt"

	"github.com/kristianwind/mimir/internal/model"
)

// Amplifying is a reaction that multiplies an existing damage instance.
// The empty value means no reaction and yields a multiplier of 1.
type Amplifying string

const (
	NoAmplify Amplifying = ""
	// Vaporize with Hydro applied onto a Pyro aura ("forward vape", ×2.0).
	VaporizeHydro Amplifying = "vaporize_hydro"
	// Vaporize with Pyro applied onto a Hydro aura ("reverse vape", ×1.5).
	VaporizePyro Amplifying = "vaporize_pyro"
	// Melt with Pyro onto a Cryo aura ("forward melt", ×2.0).
	MeltPyro Amplifying = "melt_pyro"
	// Melt with Cryo onto a Pyro aura ("reverse melt", ×1.5).
	MeltCryo Amplifying = "melt_cryo"
)

// base returns the reaction's base multiplier before EM and bonuses.
func (a Amplifying) base() float64 {
	switch a {
	case VaporizeHydro, MeltPyro:
		return 2.0
	case VaporizePyro, MeltCryo:
		return 1.5
	default:
		return 1.0
	}
}

// family returns the reaction name used for bonus lookups: a Crimson Witch
// vaporize bonus applies to both the forward and reverse variant.
func (a Amplifying) family() string {
	switch a {
	case VaporizeHydro, VaporizePyro:
		return "vaporize"
	case MeltPyro, MeltCryo:
		return "melt"
	default:
		return ""
	}
}

// EMAmplifyBonus is the elemental mastery term for amplifying reactions:
//
//	2.78 · EM / (EM + 1400)
//
// It is asymptotic — the first 200 EM is worth roughly as much as the next
// 600 — which is exactly why the resin advisor needs it rather than a rule of
// thumb like "stack EM".
func EMAmplifyBonus(em float64) float64 {
	if em <= 0 {
		return 0
	}
	return 2.78 * em / (em + 1400)
}

// EMTransformativeBonus is the EM term for transformative reactions:
//
//	16 · EM / (EM + 2000)
func EMTransformativeBonus(em float64) float64 {
	if em <= 0 {
		return 0
	}
	return 16 * em / (em + 2000)
}

// EMCrystallizeBonus is the EM term for crystallize shield strength:
//
//	4.44 · EM / (EM + 1400)
func EMCrystallizeBonus(em float64) float64 {
	if em <= 0 {
		return 0
	}
	return 4.44 * em / (em + 1400)
}

// Multiplier returns the full amplifying multiplier for the given EM and
// stat block. Returns 1 when no reaction is set.
func (a Amplifying) Multiplier(em float64, stats model.StatBlock) float64 {
	if a == NoAmplify {
		return 1
	}
	bonus := stats[model.ReactionBonus(a.family())]
	return a.base() * (1 + EMAmplifyBonus(em) + bonus)
}

// Transformative computes a standalone reaction hit (overloaded, hyperbloom,
// swirl, ...).
//
//	damage = levelMultiplier × coefficient × (1 + EM term + bonus) × RES
//
// The coefficient and the level multiplier both come from gamedata; passing
// them in rather than hardcoding is what lets a patch retune a reaction
// without touching this file.
func Transformative(
	reaction string,
	levelMultiplier float64,
	coefficient float64,
	element model.Element,
	stats model.StatBlock,
	tgt Target,
) (float64, error) {
	if levelMultiplier <= 0 {
		return 0, fmt.Errorf("calc: reaction %q has no level multiplier", reaction)
	}
	if coefficient <= 0 {
		return 0, fmt.Errorf("calc: reaction %q has no coefficient", reaction)
	}
	em := stats[model.ElementalMastery]
	bonus := stats[model.ReactionBonus(reaction)]
	return levelMultiplier * coefficient *
		(1 + EMTransformativeBonus(em) + bonus) *
		tgt.ResistanceMultiplier(element), nil
}
