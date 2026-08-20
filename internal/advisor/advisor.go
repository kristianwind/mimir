// Package advisor answers the one question no existing Genshin tool answers
// well: what should I do with tomorrow's resin?
//
// Every other tool tells you what your best build *would* be. That is a
// static answer to a dynamic question. A player has 180 resin a day, a
// weekday-gated domain rotation, a half-finished roster and a banner ending
// on Thursday — and needs the upgrades ranked by damage gained per resin
// spent, across the whole account, with the free ones first.
//
// The ranking is deterministic and comes entirely from the damage engine.
// The AI layer explains these numbers; it never produces them.
package advisor

import (
	"context"
	"fmt"
	"sort"

	"github.com/kristianwind/mimir/internal/calc"
	"github.com/kristianwind/mimir/internal/effect"
	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
	"github.com/kristianwind/mimir/internal/optimizer"
)

// Kind classifies an upgrade action.
type Kind string

const (
	// KindReequip is a free rearrangement of artifacts the player already
	// owns. It costs no resin, so it always outranks everything else — and
	// it is the single most common untapped gain on a real account.
	KindReequip Kind = "reequip"
	// KindTalent is levelling one talent by one level.
	KindTalent Kind = "talent"
	// KindAscend is a character or weapon ascension.
	KindAscend Kind = "ascend"
	// KindLevel is levelling a character or weapon within its ascension.
	KindLevel Kind = "level"
	// KindFarm is spending days in an artifact domain.
	KindFarm Kind = "farm"
	// KindWeapon is switching to a better weapon already owned.
	KindWeapon Kind = "weapon"
)

// Action is one ranked recommendation.
type Action struct {
	Kind    Kind   `json:"kind"`
	Subject string `json:"subject"`
	// Headline is the one-line recommendation, already phrased for a human.
	Headline string `json:"headline"`
	// GainPct is the expected increase in the goal's rotation damage, as a
	// fraction: 0.062 means +6.2%.
	GainPct float64 `json:"gainPct"`
	// ResinCost is the total resin. Zero for free actions.
	ResinCost float64 `json:"resinCost"`
	// Efficiency is GainPct per 100 resin. It is zero for free actions —
	// they are ranked by the Free flag below, not by an infinite
	// efficiency. A sentinel of +Inf sorts correctly and then fails to
	// serialise, which is a silent empty response rather than an error.
	Efficiency float64 `json:"efficiency"`
	// Free marks an action that costs no resin. These lead the plan
	// regardless of size: damage you already own but have not equipped is
	// the one gain no amount of farming buys back.
	Free bool `json:"free"`
	// Unpriced marks an action whose resin cost is not known — usually
	// farming, whose price depends on a drop rate the player has to measure.
	// It is not the same as free, and conflating the two would put "farm a
	// domain for a week" at the top of the plan.
	Unpriced bool `json:"unpriced,omitempty"`
	// Note qualifies the number, e.g. that a gain is stated per hundred
	// artifacts rather than per resin.
	Note string `json:"note,omitempty"`
	// BlockedBy names what stops this action today: a missing crown, a
	// domain that is closed until Thursday, an ascension gate.
	BlockedBy string `json:"blockedBy,omitempty"`
	// Detail carries the structured specifics for the UI and the AI layer.
	Detail map[string]any `json:"detail,omitempty"`
}

// Goal is what the player is trying to improve.
type Goal struct {
	CharacterKey string
	// Spec is the damage cycle the gain is measured against. Ranking without
	// one is meaningless: +8% on a burst nobody casts is +0%.
	//
	// It is stored as a spec rather than a resolved rotation because a
	// talent upgrade changes the multipliers. Resolving once up front would
	// make every talent candidate report a gain of exactly zero.
	Spec   Spec
	Target calc.Target
	// Conditions answers the questions the effect layer cannot: is Noblesse
	// up, how many Marechaussee stacks, is the enemy frozen. Mimir asks
	// rather than assumes, because these are facts about how the player
	// plays, not about the set.
	Conditions map[string]float64
	// Priority breaks ties between goals; higher comes first.
	Priority int
}

// State is the account snapshot an evaluation runs against.
type State struct {
	Character model.Character
	// Base is the character's white stats including weapon base ATK.
	Base calc.Base
	// Fixed is everything not from artifacts: ascension stat, weapon
	// substat, passives, team buffs.
	Fixed model.StatBlock
	// Equipped is the currently worn artifact set.
	Equipped []model.Artifact
	// ArtifactStats is the resolved stat block per artifact id.
	ArtifactStats map[int64]model.StatBlock
	// WeaponKey and WeaponRefinement identify the equipped weapon, which
	// weapon-passive effects need — their values scale with refinement.
	WeaponKey        string
	WeaponRefinement int
}

// Evaluator turns a state into a rotation damage number. It exists as an
// interface so the farm simulator and the deterministic candidates share one
// definition of "better", and so tests can substitute a trivial scorer.
type Evaluator interface {
	Score(ctx context.Context, g Goal, s State) (float64, error)
}

// EngineEvaluator scores through the damage engine.
type EngineEvaluator struct {
	Snapshot *gamedata.Snapshot
}

// Score returns the rotation's total average damage.
func (e EngineEvaluator) Score(ctx context.Context, g Goal, s State) (float64, error) {
	bonuses := s.Fixed.Clone()
	for _, a := range s.Equipped {
		st, ok := s.ArtifactStats[a.ID]
		if !ok {
			return 0, fmt.Errorf("advisor: no stats for artifact %d", a.ID)
		}
		bonuses.AddInto(st)
	}
	// Set bonuses are not a property of any single piece, so they are added
	// once from the assembled build rather than folded into each artifact.
	counts := optimizer.SetCounts(s.Equipped)
	setBonus, err := optimizer.SetBonuses(counts, e.Snapshot)
	if err != nil {
		return 0, err
	}
	bonuses.AddInto(setBonus)

	ectx := effect.Context{
		Snapshot:   e.Snapshot,
		Character:  s.Character,
		SetCounts:  counts,
		WeaponKey:  s.WeaponKey,
		Refinement: s.WeaponRefinement,
		Conditions: g.Conditions,
	}
	totals, err := resolveWithEffects(e.Snapshot, s.Base, bonuses, ectx)
	if err != nil {
		return 0, err
	}

	rot, err := BuildRotation(e.Snapshot, s.Character, g.Spec)
	if err != nil {
		return 0, err
	}
	rot, _, err = withEffectInstances(e.Snapshot, ectx, rot, totals, defElement(e.Snapshot, s.Character.Key))
	if err != nil {
		return 0, err
	}

	res, err := calc.Evaluate(s.Character.Level, totals, rot, g.Target.WithDebuffs(totals))
	if err != nil {
		return 0, err
	}
	return res.Total, nil
}

// Rank sorts actions best-first and fills in the efficiency field.
//
// Free actions sort above every paid one regardless of size, because a player
// who has not re-equipped is leaving damage on the table that no amount of
// farming buys back.
func Rank(actions []Action) []Action {
	out := append([]Action(nil), actions...)
	for i := range out {
		out[i].Free = !out[i].Unpriced && out[i].ResinCost <= 0 && out[i].GainPct > 0
		out[i].Efficiency = efficiency(out[i])
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		// A blocked action never outranks an actionable one, however good it
		// looks on paper: a plan you cannot execute today is not a plan.
		if (a.BlockedBy == "") != (b.BlockedBy == "") {
			return a.BlockedBy == ""
		}
		if a.Free != b.Free {
			return a.Free
		}
		if a.Efficiency != b.Efficiency {
			return a.Efficiency > b.Efficiency
		}
		return a.GainPct > b.GainPct
	})
	return out
}

func efficiency(a Action) float64 {
	if a.Unpriced || a.GainPct <= 0 || a.ResinCost <= 0 {
		return 0
	}
	return a.GainPct / a.ResinCost * 100
}

// Gain expresses an improvement as a fraction of the baseline.
func Gain(baseline, improved float64) float64 {
	if baseline <= 0 {
		return 0
	}
	return improved/baseline - 1
}
