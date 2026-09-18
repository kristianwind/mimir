package advisor

// What one more roll is worth.
//
// Sabrina, testing: "Det ville være smart med en vurdering af de enkelte
// substats, fx mindre hp% mere crit rate."
//
// She asked for it as a recommendation copied from a build guide. This
// answers the same question by measuring instead, and the difference is not
// pedantry — it is the only reason the answer is worth more than the guide.
//
// A guide gives one static target to every reader: 70% crit rate, 180% crit
// damage, and so on down the list. That target is a good summary of where
// most builds end up, and it is silent about the thing the player is actually
// deciding, which is what the NEXT roll should be. Those two are not the same
// question. Crit rate is the most valuable stat in the game right up to the
// point where it is not: at 90% it is still buying damage, at 100% the engine
// has nothing left to give it and the roll is worth exactly zero. A number
// printed on a wiki cannot know where on that curve this build sits. The
// damage engine already does.
//
// So: take the build as it stands, add one average roll of a stat, and ask
// the same evaluator the rest of Mimir uses what the rotation does now. The
// difference is what that roll bought, measured on the build in front of the
// player rather than on the average of everybody's.
//
// What this is NOT is a claim about what will drop. Rolls are random and this
// says nothing about the odds of getting one; it says what a roll is worth if
// it lands. The farm simulator is where the odds live.

import (
	"context"
	"fmt"
	"sort"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// rollable is every stat an artifact can roll as a substat.
//
// Listed rather than taken from the snapshot's roll table, because that table
// is keyed by rarity and a missing entry there should be an error about
// missing game data, not a stat that quietly disappears from the ranking.
var rollable = []model.Stat{
	model.HP, model.HPPercent,
	model.ATK, model.ATKPercent,
	model.DEF, model.DEFPercent,
	model.ElementalMastery, model.EnergyRecharge,
	model.CritRate, model.CritDMG,
}

// SubstatValue is what one average roll of one stat buys on one build.
type SubstatValue struct {
	Stat model.Stat `json:"stat"`
	// PerRoll is the fraction the yardstick damage grows by. 0.021 means a
	// roll of this stat is worth 2.1% more damage on this build.
	PerRoll float64 `json:"perRoll"`
	// Relative is PerRoll measured against the best stat in the list, so the
	// winner is 1 and everything else is the fraction of a best-roll it is
	// worth. This is the number the phrase "less HP%, more crit rate" is
	// really about: it is a comparison, not a magnitude.
	Relative float64 `json:"relative"`
	// Note is set only where the number needs one — a stat that buys nothing
	// looks like a bug to somebody who has read a guide recommending it, and
	// the reason is usually interesting.
	Note string `json:"note,omitempty"`
}

// SubstatValues ranks every rollable substat by what one roll of it would add
// to this build.
//
// The state is the build as worn. Nothing here searches, re-equips or
// suggests: it is a reading of the build in front of it, which is what makes
// it cheap enough to compute on a page load — eleven evaluations, against the
// hundreds the ranking pages do.
func SubstatValues(
	ctx context.Context,
	snap *gamedata.Snapshot,
	s State,
	conditions map[string]float64,
) ([]SubstatValue, error) {
	if snap == nil {
		return nil, fmt.Errorf("advisor: substat values need a game data snapshot")
	}
	if _, ok := snap.SubstatRolls[5]; !ok {
		return nil, fmt.Errorf("%w: five-star substat roll values", gamedata.ErrMissing)
	}

	eval := yardstickEvaluator{Snapshot: snap, Conditions: conditions}
	base, err := eval.Score(ctx, Goal{}, s)
	if err != nil {
		return nil, err
	}
	// A build that does no damage has no ratios in it. Dividing by it would
	// produce an infinity and a ranking of infinities is not a ranking.
	if base <= 0 {
		return nil, fmt.Errorf("advisor: %s scores zero on the yardstick, so a roll cannot be measured against it",
			s.Character.Key)
	}

	out := make([]SubstatValue, 0, len(rollable))
	for _, stat := range rollable {
		// One roll, at the same average-of-the-mined-tiers convention the
		// target build uses. Sharing that convention is what lets the two
		// views be read side by side.
		roll, err := substatBlock(snap, map[model.Stat]int{stat: 1})
		if err != nil {
			return nil, err
		}

		// Added to Fixed rather than to a piece. The stat total is identical
		// either way, and Fixed is the half of the build that carries no set
		// count — putting a hypothetical roll on an artifact would risk
		// moving a set bonus that the player has not actually changed.
		probe := s
		probe.Fixed = s.Fixed.Add(roll)

		after, err := eval.Score(ctx, Goal{}, probe)
		if err != nil {
			return nil, err
		}

		out = append(out, SubstatValue{
			Stat:    stat,
			PerRoll: (after - base) / base,
		})
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].PerRoll > out[j].PerRoll })

	best := out[0].PerRoll
	for i := range out {
		if best > 0 {
			out[i].Relative = out[i].PerRoll / best
		}
		out[i].Note = substatNote(out[i], best)
	}
	return out, nil
}

// substatNote explains a number that would otherwise read as a fault.
//
// Only the surprising cases get one. A stat that is simply mediocre needs no
// sentence: the ranking already says so, and annotating all ten would bury
// the two that matter.
//
// Energy recharge gets the longest one, and it is the most important thing on
// this page. The yardstick is one cast of the skill and one of the burst,
// which is a rotation that never waits for energy — so measured against it, a
// recharge roll moves nothing and this function would otherwise report a
// confident zero for a stat the game is largely built around. That zero is
// true about the measurement and false about the game. Saying which is the
// difference between a stated gap and an estimate dressed as a fact.
func substatNote(v SubstatValue, best float64) string {
	switch {
	case best <= 0:
		return "nothing this build can roll adds damage to the rotation being measured"
	case v.Stat == model.EnergyRecharge && v.PerRoll <= 0:
		return "not measured rather than worthless: the yardstick is one skill and one burst, " +
			"a rotation that never waits for energy, so recharge has nothing to change here. " +
			"What it actually buys is how often you get the burst off at all, which this does not model"
	case v.PerRoll <= 0 && v.Stat == model.CritRate:
		return "crit rate is already at the ceiling for this rotation, so a further roll buys nothing"
	case v.PerRoll <= 0 && v.Stat == model.ElementalMastery:
		return "nothing in the rotation being measured reacts, so mastery has nothing to amplify. " +
			"Declare the reaction on the goal and this number changes"
	case v.PerRoll <= 0:
		return "the rotation being measured does not scale on this stat, so a roll buys nothing"
	default:
		return ""
	}
}

// SubstatVerdict grades the rolls a single artifact actually has, against
// what a roll is worth on the character wearing it.
//
// This is the second half of the question. Knowing that crit rate is worth
// four times what HP% is does not yet tell anybody which of their pieces is
// the problem; the rolls are already spent, and what the player wants to see
// is which piece spent them badly.
//
// It reports, not prescribes. A piece whose rolls all went somewhere cheap is
// a piece to replace, and saying "this one put four rolls into HP% on a
// character where HP% is worth a fifth of a crit roll" is a fact the player
// can act on without being told what to do about it.
type SubstatVerdict struct {
	Stat model.Stat `json:"stat"`
	// Value is the piece's own rolled total for this stat.
	Value float64 `json:"value"`
	// Relative is what one roll of it is worth on this character, against
	// the best available roll. Carried per substat so the piece can be read
	// on its own without the character's whole table beside it.
	Relative float64 `json:"relative"`
}

// GradeSubstats attaches the character's roll values to one piece's substats.
//
// values must come from SubstatValues for the character wearing the piece.
// Passing another character's table would produce a confident, wrong answer,
// which is why this takes the table rather than recomputing one: the caller
// holds the pairing and can be read.
func GradeSubstats(subs []model.Substat, values []SubstatValue) []SubstatVerdict {
	byStat := make(map[model.Stat]float64, len(values))
	for _, v := range values {
		byStat[v.Stat] = v.Relative
	}
	out := make([]SubstatVerdict, 0, len(subs))
	for _, s := range subs {
		out = append(out, SubstatVerdict{
			Stat:     s.Key,
			Value:    s.Value,
			Relative: byStat[s.Key],
		})
	}
	return out
}
