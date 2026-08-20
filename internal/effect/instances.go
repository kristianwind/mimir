package effect

import (
	"fmt"

	"github.com/kristianwind/mimir/internal/calc"
	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// InstanceGrant is a damage hit an effect adds, with its provenance.
type InstanceGrant struct {
	Instance calc.Instance `json:"instance"`
	Source   string        `json:"source"`
	Note     string        `json:"note,omitempty"`
	Cite     string        `json:"cite,omitempty"`
}

// Instances returns the extra damage hits a build's effects contribute.
//
// The count is folded into the multiplier rather than emitted as repeated
// hits. Every occurrence of a proc is identical — same scaling, same crit,
// same target — so two hits of 240% and one of 480% are the same expected
// damage, and collapsing them keeps a rotation readable.
//
// stats must be the resolved totals: an instance may scale off a final stat,
// so it belongs to the post phase like every other conversion.
func Instances(
	rules []gamedata.EffectRule,
	ctx Context,
	stats model.StatBlock,
	fallbackElement model.Element,
) ([]InstanceGrant, error) {
	var out []InstanceGrant

	for _, rule := range rules {
		if !Applies(rule, ctx) {
			continue
		}
		for _, e := range rule.Effects {
			if e.Instance == nil {
				continue
			}
			if e.Phase != gamedata.EffectPhasePost {
				return nil, fmt.Errorf(
					"effect: %s/%s adds a damage hit in the %q phase; hits read final stats and must be post",
					rule.Key, rule.Trigger, e.Phase)
			}

			multiplier, err := Value(e, ctx, stats)
			if err != nil {
				return nil, fmt.Errorf("effect: %s/%s: %w", rule.Key, rule.Trigger, err)
			}
			if multiplier <= 0 {
				// Undeclared occurrences mean the player has not said the
				// proc happens, which is an answer.
				continue
			}

			element := e.Instance.Element
			if element == "" {
				element = fallbackElement
			}
			scaling := e.Instance.Scaling
			if scaling == "" {
				scaling = model.ATK
			}
			label := e.Instance.Label
			if label == "" {
				label = rule.Key + " " + rule.Trigger
			}

			out = append(out, InstanceGrant{
				Instance: calc.Instance{
					Label:      label,
					Element:    element,
					Category:   e.Instance.Category,
					Scaling:    scaling,
					Multiplier: multiplier,
				},
				Source: rule.Key + " " + rule.Trigger,
				Note:   e.Note,
				Cite:   rule.Description,
			})
		}
	}
	return out, nil
}
