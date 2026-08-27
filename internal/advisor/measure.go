package advisor

import (
	"context"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
	"github.com/kristianwind/mimir/internal/optimizer"
)

// Measuring one build and nothing else.
//
// Assess answers "what should this player do next", which needs their whole
// inventory and produces a list of upgrades. Comparing two builds needs
// neither: the other account's spare artifacts are none of Mimir's business,
// and advice about somebody else's account is not what anybody asked for.
//
// So this is the score alone, on the same yardstick the ranking uses — one
// cast of the elemental skill and one of the burst, at this character's own
// talent levels, against a level 90 enemy. Same ruler both sides, or the
// comparison means nothing.

// Measurement is one build on the yardstick.
type Measurement struct {
	Character     string          `json:"character"`
	Element       model.Element   `json:"element"`
	Level         int             `json:"level"`
	Constellation int             `json:"constellation"`
	Talents       map[string]int  `json:"talents"`
	Weapon        string          `json:"weapon,omitempty"`
	Refinement    int             `json:"refinement,omitempty"`
	WeaponLevel   int             `json:"weaponLevel,omitempty"`
	Sets          map[string]int  `json:"sets,omitempty"`
	Score         float64         `json:"score"`
	Stats         model.StatBlock `json:"stats"`
	// MeasuredOn names the damage rows the score is the sum of, so two
	// numbers that turn out not to be comparable can be seen not to be.
	MeasuredOn []string `json:"measuredOn,omitempty"`
}

// Measure scores one loadout on the yardstick.
func Measure(
	ctx context.Context,
	snap *gamedata.Snapshot,
	loadout Loadout,
	conditions map[string]float64,
) (Measurement, error) {
	state, err := Assemble(snap, loadout)
	if err != nil {
		return Measurement{}, err
	}

	eval := yardstickEvaluator{Snapshot: snap, Conditions: conditions}
	score, err := eval.Score(ctx, Goal{}, state)
	if err != nil {
		return Measurement{}, err
	}

	sheet, err := BuildSheet(snap, state, conditions)
	if err != nil {
		return Measurement{}, err
	}

	out := Measurement{
		Character:     loadout.Character.Key,
		Element:       defElement(snap, loadout.Character.Key),
		Level:         loadout.Character.Level,
		Constellation: loadout.Character.Constellation,
		Talents: map[string]int{
			gamedata.TalentAuto:  loadout.Character.TalentAuto,
			gamedata.TalentSkill: loadout.Character.TalentSkill,
			gamedata.TalentBurst: loadout.Character.TalentBurst,
		},
		Sets:  optimizer.SetCounts(state.Equipped),
		Score: score,
		Stats: sheet.Totals,
	}
	if loadout.Weapon != nil {
		out.Weapon = loadout.Weapon.Key
		out.Refinement = loadout.Weapon.Refinement
		out.WeaponLevel = loadout.Weapon.Level
	}
	if rot, err := yardstickRotation(snap, state.Character); err == nil {
		for _, inst := range rot.Instances {
			out.MeasuredOn = append(out.MeasuredOn, inst.Label)
		}
	}
	return out, nil
}
