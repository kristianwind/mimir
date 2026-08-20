package advisor

import (
	"fmt"

	"github.com/kristianwind/mimir/internal/calc"
	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// Step is one entry in a player-defined rotation: which talent, which of its
// damage rows, how many times, and whether it reacts.
//
// Rotations are authored, not inferred. Mimir will not guess that your Raiden
// casts burst then five normals — the guess would be wrong for half of all
// teams, and every gain figure downstream inherits the error. What Mimir does
// instead is make authoring cheap: the talent rows come straight from the
// mined tables, so a step names a real label and is validated against it.
type Step struct {
	// Talent is "auto", "skill" or "burst".
	Talent string `json:"talent"`
	// Entry is the talent row's label, or a prefix of it: "Press DMG".
	Entry string `json:"entry"`
	// Hits is how many times this instance lands in one rotation.
	Hits int `json:"hits"`
	// Amplify names an amplifying reaction on this instance.
	Amplify calc.Amplifying `json:"amplify,omitempty"`
	// Element overrides the damage type, for infusions.
	Element model.Element `json:"element,omitempty"`
	// Extra holds bonuses that apply only to this instance.
	Extra model.StatBlock `json:"extra,omitempty"`
}

// Spec is a rotation before it is resolved against a character's talent
// levels. It is what gets stored on a goal and edited in the UI.
type Spec struct {
	Name     string  `json:"name"`
	Steps    []Step  `json:"steps"`
	Duration float64 `json:"duration"`
}

// BuildRotation resolves a spec into engine instances using a character's
// actual talent levels.
func BuildRotation(snap *gamedata.Snapshot, ch model.Character, spec Spec) (calc.Rotation, error) {
	def, err := snap.Char(ch.Key)
	if err != nil {
		return calc.Rotation{}, err
	}

	rot := calc.Rotation{Name: spec.Name, Duration: spec.Duration}
	for i, step := range spec.Steps {
		if _, err := talentLevel(ch, step.Talent); err != nil {
			return calc.Rotation{}, fmt.Errorf("step %d: %w", i+1, err)
		}
		// The constellation bonus is applied here rather than stored,
		// because storing it compounds: a C5 character re-imported three
		// times would climb from level 9 to 18.
		level := EffectiveTalentLevel(def, ch, step.Talent)

		entry, err := def.TalentEntry(step.Talent, step.Entry)
		if err != nil {
			return calc.Rotation{}, fmt.Errorf("step %d: %w", i+1, err)
		}
		if !entry.IsDamage() {
			return calc.Rotation{}, fmt.Errorf(
				"step %d: %q on %s is a %s value, not a damage multiplier",
				i+1, entry.Label, ch.Key, entry.Unit)
		}

		mult, err := entry.Multiplier(level)
		if err != nil {
			return calc.Rotation{}, fmt.Errorf("step %d: %w", i+1, err)
		}

		element := entry.Element
		if step.Element != "" {
			element = step.Element
		}
		if element == "" {
			element = def.Element
		}

		hits := step.Hits
		if hits < 1 {
			hits = 1
		}
		for h := 0; h < hits; h++ {
			rot.Instances = append(rot.Instances, calc.Instance{
				Label:   fmt.Sprintf("%s · %s", step.Talent, entry.Label),
				Element: element,
				// The category comes from the mined row, not from the
				// talent slot. Raiden's burst table lists the Musou Isshin
				// sword swings, and those take normal-attack bonuses.
				Category:   entry.Category,
				Scaling:    entry.Scaling,
				Multiplier: mult,
				Extra:      step.Extra,
				Amplify:    step.Amplify,
			})
		}
	}

	if len(rot.Instances) == 0 {
		return calc.Rotation{}, fmt.Errorf("rotation %q has no damage steps", spec.Name)
	}
	return rot, nil
}

// talentLevel returns the character's level in a talent slot, including the
// three levels a C3 or C5 grants.
func talentLevel(ch model.Character, slot string) (int, error) {
	switch slot {
	case gamedata.TalentAuto:
		return ch.TalentAuto, nil
	case gamedata.TalentSkill:
		return ch.TalentSkill, nil
	case gamedata.TalentBurst:
		return ch.TalentBurst, nil
	default:
		return 0, fmt.Errorf("unknown talent slot %q (want auto, skill or burst)", slot)
	}
}

// EffectiveTalentLevel adds the constellation bonus to a stored base level.
//
// Stored levels are base levels 1–10; two constellations each add three to
// one talent, and which talent varies by character. A snapshot that has not
// mined the mapping reports the base level rather than inventing a bonus —
// understating a build is recoverable, inventing three talent levels is not.
func EffectiveTalentLevel(def gamedata.Character, ch model.Character, slot string) int {
	base, err := talentLevel(ch, slot)
	if err != nil {
		return 0
	}
	for constellation, boost := range def.ConstellationTalentBonus {
		if boost.Slot != slot || ch.Constellation < constellation {
			continue
		}
		base += boost.Levels
		if boost.MaxLevel > 0 && base > boost.MaxLevel {
			base = boost.MaxLevel
		}
	}
	return base
}
