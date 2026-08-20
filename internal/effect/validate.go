package effect

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kristianwind/mimir/internal/gamedata"
)

// numberPattern finds the figures quoted in an in-game description.
var numberPattern = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)

// Validate checks that every number an effect claims actually appears in the
// wording it cites.
//
// This is the whole reason effects are allowed to exist as hand-written data.
// The numbers cannot be mined — they live in ability configs, not tables — so
// the alternative to a check like this is trusting whoever typed the file.
// Here, "Emblem gives 25% of ER, capped at 75%" only loads if the mined text
// for Emblem's four-piece really does say 25 and 75.
func Validate(rules []gamedata.EffectRule) error {
	var problems []string
	for _, rule := range rules {
		if len(rule.Effects) == 0 {
			problems = append(problems, fmt.Sprintf("%s/%s has no effects", rule.Key, rule.Trigger))
			continue
		}
		if strings.TrimSpace(rule.Description) == "" {
			problems = append(problems, fmt.Sprintf(
				"%s/%s cites no description, so its numbers cannot be checked",
				rule.Key, rule.Trigger))
			continue
		}

		quoted := numbersIn(rule.Description)
		for i, e := range rule.Effects {
			if e.Instance != nil && e.Phase != gamedata.EffectPhasePost {
				problems = append(problems, fmt.Sprintf(
					"%s/%s effect %d adds a damage hit in the %q phase; hits read "+
						"final stats and must be post",
					rule.Key, rule.Trigger, i+1, e.Phase))
			}
			for _, claim := range literals(e) {
				if !matches(quoted, claim.value) {
					problems = append(problems, fmt.Sprintf(
						"%s/%s effect %d: %s = %g does not appear in %q",
						rule.Key, rule.Trigger, i+1, claim.field, claim.value,
						truncate(rule.Description, 120)))
				}
			}

			// Refinement-scaled values are checked one by one against the
			// wording for that refinement. Checking them against the whole
			// text would let an R1 number pass on an R5 sentence, which is
			// exactly the mistake worth catching.
			for r, v := range e.ByRefinement {
				if r >= len(rule.DescriptionByRefinement) {
					problems = append(problems, fmt.Sprintf(
						"%s/%s effect %d: no R%d wording to check %g against",
						rule.Key, rule.Trigger, i+1, r+1, v))
					continue
				}
				text := rule.DescriptionByRefinement[r]
				if !matches(numbersIn(text), v) {
					problems = append(problems, fmt.Sprintf(
						"%s/%s effect %d: R%d value %g does not appear in %q",
						rule.Key, rule.Trigger, i+1, r+1, v, truncate(text, 120)))
				}
			}
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("effect: %d unverifiable claim(s):\n  %s",
			len(problems), strings.Join(problems, "\n  "))
	}
	return nil
}

type literal struct {
	field string
	value float64
}

// literals lists the numbers an effect asserts on its own authority. Values
// taken from a mined talent row are not listed: those are already tracked by
// the game data and carry no risk of invention.
func literals(e gamedata.Effect) []literal {
	var out []literal
	add := func(field string, v float64) {
		if v != 0 {
			out = append(out, literal{field, v})
		}
	}
	add("flat", e.Flat)
	if e.RateFrom == nil {
		add("rate", e.Rate)
	}
	add("offset", e.Offset)
	if e.TimesFrom == nil && e.Times != 1 {
		add("times", e.Times)
	}
	// maxStacks means two different things and only one of them is a claim
	// about the game.
	//
	// On a stat effect it is the cap the text states — "Max 3 stacks" — and
	// is checked. A value of 1 is exempt: that is a toggle, not a stack
	// count, and checking it would force every on/off effect to find a "1"
	// in its own wording.
	//
	// On an instance effect it bounds how many times the player says a proc
	// happens in their rotation. The game states no such number, because it
	// is a fact about the rotation rather than about the effect. It caps the
	// user's own input, so it cannot invent damage they did not ask for —
	// but it does stop a typed 9999 from reaching the engine.
	if e.Instance == nil && e.MaxStacks != 1 {
		add("maxStacks", e.MaxStacks)
	}
	if e.Max != nil {
		add("max", *e.Max)
	}
	if e.Min != nil {
		add("min", *e.Min)
	}
	return out
}

// matches accepts a claim if it appears either as a plain number or as a
// percentage — "0.25" reads as 25% in game text, and both spellings occur.
func matches(quoted []float64, claim float64) bool {
	for _, q := range quoted {
		if close(q, claim) || close(q, claim*100) {
			return true
		}
	}
	return false
}

func close(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func numbersIn(text string) []float64 {
	var out []float64
	for _, m := range numberPattern.FindAllString(text, -1) {
		if v, err := strconv.ParseFloat(m, 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Load reads an effects file and fills each rule's citation from the snapshot.
//
// A rule whose citation cannot be resolved is an error rather than a silent
// pass: an effect on a set the snapshot has never heard of is either a typo or
// a stale file, and both produce wrong numbers quietly.
func Load(path string, snap *gamedata.Snapshot) ([]gamedata.EffectRule, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("effect: read %s: %w", path, err)
	}

	var file struct {
		Rules []gamedata.EffectRule `json:"rules"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("effect: parse %s: %w", path, err)
	}

	for i := range file.Rules {
		rule := file.Rules[i]
		desc, err := describe(rule, snap)
		if err != nil {
			return nil, err
		}
		if rule.Kind == gamedata.EffectKindWeapon {
			file.Rules[i].DescriptionByRefinement = snap.Weapons[rule.Key].PassiveTexts
		}
		// A rule may draw on more than one text. Crimson Witch's four-piece
		// scales its own two-piece bonus, so "50% of its starting value" is
		// only checkable with the two-piece wording alongside it.
		for _, also := range rule.Cites {
			extra, err := describe(gamedata.EffectRule{
				Key: rule.Key, Kind: rule.Kind, Trigger: also,
			}, snap)
			if err != nil {
				return nil, fmt.Errorf("effect: %s cites %q: %w", rule.Key, also, err)
			}
			desc += "\n" + extra
		}
		file.Rules[i].Description = desc
	}

	if err := Validate(file.Rules); err != nil {
		return nil, err
	}
	sort.SliceStable(file.Rules, func(a, b int) bool {
		return file.Rules[a].Key < file.Rules[b].Key
	})
	return file.Rules, nil
}

// describe finds the in-game wording a rule hangs off.
func describe(r gamedata.EffectRule, snap *gamedata.Snapshot) (string, error) {
	switch r.Kind {
	case gamedata.EffectKindArtifactSet:
		set, err := snap.Set(r.Key)
		if err != nil {
			return "", fmt.Errorf("effect: %s/%s: %w", r.Key, r.Trigger, err)
		}
		switch r.Trigger {
		case "2pc":
			return set.TwoPieceText, nil
		case "4pc":
			return set.FourPieceText, nil
		default:
			return "", fmt.Errorf("effect: %s: artifact triggers are 2pc or 4pc, got %q", r.Key, r.Trigger)
		}

	case gamedata.EffectKindCharacter:
		def, err := snap.Char(r.Key)
		if err != nil {
			return "", fmt.Errorf("effect: %s/%s: %w", r.Key, r.Trigger, err)
		}
		if p, ok := def.Passives[r.Trigger]; ok {
			return p.Description, nil
		}
		if c, ok := constellationTrigger(r.Trigger); ok {
			if c-1 < len(def.Constellations) {
				return def.Constellations[c-1].Description, nil
			}
			return "", fmt.Errorf("effect: %s has no constellation %d in the snapshot", r.Key, c)
		}
		return "", fmt.Errorf("effect: %s has no passive %q in the snapshot", r.Key, r.Trigger)

	case gamedata.EffectKindWeapon:
		w, ok := snap.Weapons[r.Key]
		if !ok {
			return "", fmt.Errorf("effect: unknown weapon %q", r.Key)
		}
		if len(w.PassiveTexts) == 0 {
			return "", fmt.Errorf("effect: %s has no mined passive text to check against", r.Key)
		}
		return strings.Join(w.PassiveTexts, "\n"), nil

	default:
		return "", fmt.Errorf("effect: unknown kind %q on %s", r.Kind, r.Key)
	}
}
