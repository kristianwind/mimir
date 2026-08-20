package gamedata

// The effect layer's data types.
//
// They live here rather than in package effect because they are game data
// like every other table in this file — and because the snapshot carries
// them, which it could not do if the types lived in a package that imports
// this one. Package effect owns the behaviour: evaluation, citation checking
// and loading.

import "github.com/kristianwind/mimir/internal/model"

// Phase decides when an effect is applied.
type EffectPhase string

const (
	// PhasePre adds to the bonus block before base stats are resolved, so a
	// granted ATK% multiplies base ATK the way an artifact's would.
	EffectPhasePre EffectPhase = "pre"
	// PhasePost adds to the resolved totals. Conversions belong here: they
	// read a final stat, so they cannot run before it exists.
	EffectPhasePost EffectPhase = "post"
)

// Kind is what an effect hangs off.
type EffectKind string

const (
	EffectKindArtifactSet EffectKind = "artifactSet"
	EffectKindCharacter   EffectKind = "character"
	EffectKindWeapon      EffectKind = "weapon"
)

// TalentRef points at a mined talent row, so an effect's rate can track the
// game data instead of being frozen into the effect file. Raiden's skill
// grants burst damage per point of energy, and that per-energy figure is a
// talent scaling value like any other.
type TalentRef struct {
	Talent string `json:"talent"`
	Entry  string `json:"entry"`
}

// Effect is one grant.
//
//	value = clamp(flat + rate × (source − offset), min, max) × times × stacks
//
// where source is the value of From, or 1 when From is empty; and stacks is
// the player-declared condition named by StacksFrom, or 1.
type Effect struct {
	// Grants is the stat this effect adds to.
	Grants model.Stat  `json:"grants"`
	Phase  EffectPhase `json:"phase"`

	Flat     float64    `json:"flat,omitempty"`
	Rate     float64    `json:"rate,omitempty"`
	RateFrom *TalentRef `json:"rateFrom,omitempty"`
	// ByRefinement gives the rate at R1..R5. Weapon passives are the only
	// place a bonus depends on refinement, and every one of the five values
	// is checked against that refinement's own wording.
	ByRefinement []float64 `json:"byRefinement,omitempty"`

	// From names the stat being converted. Empty means the effect is a flat
	// grant and source is 1.
	From   model.Stat `json:"from,omitempty"`
	Offset float64    `json:"offset,omitempty"`

	Times     float64    `json:"times,omitempty"`
	TimesFrom *TalentRef `json:"timesFrom,omitempty"`

	// StacksFrom names a condition the player declares on the goal, e.g.
	// "MarechausseeHunter.stacks". Absent means always fully active.
	StacksFrom string  `json:"stacksFrom,omitempty"`
	MaxStacks  float64 `json:"maxStacks,omitempty"`

	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`

	// Instance turns this effect into an extra damage hit rather than a stat
	// grant. The computed value becomes the hit's total scaling multiplier.
	Instance *EffectInstance `json:"instance,omitempty"`

	Note string `json:"note,omitempty"`
}

// EffectInstance describes a damage hit an effect adds to the rotation.
//
// Some of the game's most-used effects are not bonuses at all — Prototype
// Archaic's proc, Noelle's C4 shield explosion, Xiangling's C2 Implode. They
// deal their own damage, and a build sheet that models only stat bonuses
// simply does not see them.
type EffectInstance struct {
	// Label names the hit in a damage breakdown.
	Label string `json:"label"`
	// Element is the damage type. Empty inherits the character's.
	Element model.Element `json:"element,omitempty"`
	// Scaling names the stat the multiplier applies to.
	Scaling model.Stat `json:"scaling,omitempty"`
	// Category decides which attack-category bonuses reach it. Most procs
	// take none — a weapon's proc is not a Normal Attack even when a Normal
	// Attack triggered it — so the default of none is usually right.
	Category model.Category `json:"category,omitempty"`
}

// Rule groups the effects that fire together, with the citation that makes
// their numbers checkable.
type EffectRule struct {
	// Key is the artifact set, character or weapon this belongs to.
	Key  string     `json:"key"`
	Kind EffectKind `json:"kind"`
	// Trigger is "2pc", "4pc", "passive1".."passive3", "c1".."c6", or
	// "always".
	Trigger string `json:"trigger"`
	// MinAscension gates a character passive. A4 passives do nothing on an
	// unascended character, and pretending otherwise inflates every early
	// build.
	MinAscension int `json:"minAscension,omitempty"`
	// Cites names further triggers whose wording this rule also draws on.
	// Crimson Witch's four-piece is defined in terms of its two-piece
	// bonus, so its numbers only make sense against both texts.
	Cites []string `json:"cites,omitempty"`
	// Description is the in-game wording, filled in from the snapshot at
	// load time. It is what Validate checks the numbers against.
	Description string `json:"description,omitempty"`
	// DescriptionByRefinement is the wording at R1..R5, for weapon rules.
	DescriptionByRefinement []string `json:"descriptionByRefinement,omitempty"`
	Effects                 []Effect `json:"effects"`
}
