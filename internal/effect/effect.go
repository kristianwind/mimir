// Package effect is the small declarative language for the bonuses that are
// not stat blocks.
//
// Most of a build's numbers are additive stats and need no machinery. A
// minority are conversions and conditionals — Emblem turning Energy Recharge
// into Burst DMG, Raiden turning it into Electro DMG, Marechaussee stacking
// crit rate — and those are exactly the ones that matter most, because they
// are attached to the sets and characters people actually build.
//
// They are data, not Go: a patch that retunes Emblem should be an edit to a
// JSON file, not a release. And because the numbers cannot be mined (they
// live in ability configs, not tables), every effect must cite the in-game
// wording it came from, and Validate checks that the numbers it claims
// actually appear in that wording. A fabricated multiplier fails the build.
package effect

import (
	"fmt"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// Context is everything an evaluation needs.
type Context struct {
	Snapshot  *gamedata.Snapshot
	Character model.Character
	// SetCounts is how many pieces of each artifact set the build wears.
	SetCounts map[string]int
	// WeaponKey is the equipped weapon, or empty.
	WeaponKey string
	// Refinement is that weapon's refinement, R1..R5.
	Refinement int
	// Conditions are the player's declared answers to "is this active, and
	// how many stacks?". Mimir asks rather than assumes: whether your
	// Marechaussee is at three stacks is a fact about how you play.
	Conditions map[string]float64
}

// Applies reports whether a rule fires in this context.
func Applies(r gamedata.EffectRule, ctx Context) bool {
	switch r.Kind {
	case gamedata.EffectKindArtifactSet:
		need := 0
		switch r.Trigger {
		case "2pc":
			need = 2
		case "4pc":
			need = 4
		default:
			return false
		}
		return ctx.SetCounts[r.Key] >= need

	case gamedata.EffectKindCharacter:
		if ctx.Character.Key != r.Key {
			return false
		}
		if ctx.Character.Ascension < r.MinAscension {
			return false
		}
		if c, ok := constellationTrigger(r.Trigger); ok {
			return ctx.Character.Constellation >= c
		}
		return true

	case gamedata.EffectKindWeapon:
		return ctx.WeaponKey == r.Key

	default:
		return false
	}
}

func constellationTrigger(trigger string) (int, bool) {
	if len(trigger) == 2 && (trigger[0] == 'c' || trigger[0] == 'C') &&
		trigger[1] >= '1' && trigger[1] <= '6' {
		return int(trigger[1] - '0'), true
	}
	return 0, false
}

// Value computes one effect's contribution. stats supplies the source stat.
func Value(e gamedata.Effect, ctx Context, stats model.StatBlock) (float64, error) {
	rate := e.Rate
	if len(e.ByRefinement) > 0 {
		i := ctx.Refinement - 1
		if i < 0 {
			i = 0
		}
		if i >= len(e.ByRefinement) {
			i = len(e.ByRefinement) - 1
		}
		rate = e.ByRefinement[i]
	}
	if e.RateFrom != nil {
		v, err := talentValue(ctx, *e.RateFrom)
		if err != nil {
			return 0, err
		}
		rate = v
	}

	times := e.Times
	if times == 0 {
		times = 1
	}
	if e.TimesFrom != nil {
		v, err := talentValue(ctx, *e.TimesFrom)
		if err != nil {
			return 0, err
		}
		times = v
	}

	source := 1.0
	if e.From != "" {
		source = stats[e.From]
	}

	value := e.Flat + rate*(source-e.Offset)
	if e.Min != nil && value < *e.Min {
		value = *e.Min
	}
	if e.Max != nil && value > *e.Max {
		value = *e.Max
	}

	stacks := 1.0
	if e.StacksFrom != "" {
		stacks = ctx.Conditions[e.StacksFrom]
		if stacks < 0 {
			stacks = 0
		}
		if e.MaxStacks > 0 && stacks > e.MaxStacks {
			stacks = e.MaxStacks
		}
	}

	return value * times * stacks, nil
}

// talentValue resolves a talent row at the character's current level.
func talentValue(ctx Context, ref gamedata.TalentRef) (float64, error) {
	def, err := ctx.Snapshot.Char(ctx.Character.Key)
	if err != nil {
		return 0, err
	}
	entry, err := def.TalentEntry(ref.Talent, ref.Entry)
	if err != nil {
		return 0, err
	}

	var level int
	switch ref.Talent {
	case gamedata.TalentAuto:
		level = ctx.Character.TalentAuto
	case gamedata.TalentSkill:
		level = ctx.Character.TalentSkill
	case gamedata.TalentBurst:
		level = ctx.Character.TalentBurst
	default:
		return 0, fmt.Errorf("effect: unknown talent slot %q", ref.Talent)
	}
	if level < 1 {
		level = 1
	}
	return entry.Multiplier(level)
}

// Grant is one effect's contribution, recorded so a number on the build sheet
// can be traced back to the wording it came from.
type Grant struct {
	Source string     `json:"source"`
	Stat   model.Stat `json:"stat"`
	Value  float64    `json:"value"`
	Note   string     `json:"note,omitempty"`
	// Cite is the in-game text the effect's numbers were checked against.
	Cite string `json:"cite,omitempty"`
}

// Apply runs every applicable rule for one phase and returns the grants.
//
// stats is what the effects read: the bonus block during the pre phase, the
// resolved totals during the post phase.
//
// It does not build the trace. That matters more than it looks: this runs
// once per candidate build inside the artifact search, hundreds of thousands
// of times for a single character, and assembling a provenance record — a
// concatenated source name and the rule's whole description, per grant, per
// call — was one of the largest costs in the entire program. Nobody was
// reading any of it.
func Apply(rules []gamedata.EffectRule, ctx Context, phase gamedata.EffectPhase, stats model.StatBlock) (model.StatBlock, error) {
	out, _, err := apply(rules, ctx, phase, stats, false)
	return out, err
}

// ApplyTraced is Apply plus a record of what each effect contributed.
//
// The trace is not debugging output. A build sheet that says "46.6% Electro
// DMG" and cannot say where it came from is asking to be trusted; one that
// shows "+40.0% from Enlightened One — Each 1% above 100% Energy Recharge…"
// can be checked against the game by anyone who doubts it.
func ApplyTraced(
	rules []gamedata.EffectRule,
	ctx Context,
	phase gamedata.EffectPhase,
	stats model.StatBlock,
) (model.StatBlock, []Grant, error) {
	return apply(rules, ctx, phase, stats, true)
}

func apply(
	rules []gamedata.EffectRule,
	ctx Context,
	phase gamedata.EffectPhase,
	stats model.StatBlock,
	wantTrace bool,
) (model.StatBlock, []Grant, error) {
	out := model.StatBlock{}
	var trace []Grant

	for _, rule := range rules {
		if !Applies(rule, ctx) {
			continue
		}
		for _, e := range rule.Effects {
			if e.Phase != phase {
				continue
			}
			if e.Grants == "" {
				return nil, nil, fmt.Errorf("effect: %s/%s grants nothing", rule.Key, rule.Trigger)
			}
			v, err := Value(e, ctx, stats)
			if err != nil {
				return nil, nil, fmt.Errorf("effect: %s/%s: %w", rule.Key, rule.Trigger, err)
			}
			if v == 0 {
				continue
			}
			out[e.Grants] += v
			if wantTrace {
				trace = append(trace, Grant{
					Source: rule.Key + " " + rule.Trigger,
					Stat:   e.Grants,
					Value:  v,
					Note:   e.Note,
					Cite:   rule.Description,
				})
			}
		}
	}
	return out, trace, nil
}

// Missing is a condition an active rule needs but the player has not answered.
type Missing struct {
	// Key is the condition to declare, e.g. "MarechausseeHunter.stacks".
	Key string `json:"key"`
	// Source is the rule that wants it.
	Source string `json:"source"`
	// MaxStacks is the highest meaningful answer.
	MaxStacks float64 `json:"maxStacks,omitempty"`
	// Note is the effect's own explanation.
	Note string `json:"note,omitempty"`
}

// Undeclared lists the conditions that would change this build's numbers but
// have not been answered.
//
// Without this, wearing 4pc Marechaussee and never declaring its stacks looks
// exactly like wearing a set whose four-piece does nothing — the build sheet
// is quietly missing 36% crit rate and says nothing about it. A conditional
// effect that is off because nobody was asked is a bug in the tool, not a
// fact about the build.
func Undeclared(rules []gamedata.EffectRule, ctx Context) []Missing {
	var out []Missing
	seen := map[string]bool{}

	for _, rule := range rules {
		if !Applies(rule, ctx) {
			continue
		}
		for _, e := range rule.Effects {
			if e.StacksFrom == "" || seen[e.StacksFrom] {
				continue
			}
			if _, declared := ctx.Conditions[e.StacksFrom]; declared {
				continue
			}
			seen[e.StacksFrom] = true
			out = append(out, Missing{
				Key:       e.StacksFrom,
				Source:    rule.Key + " " + rule.Trigger,
				MaxStacks: e.MaxStacks,
				Note:      e.Note,
			})
		}
	}
	return out
}
