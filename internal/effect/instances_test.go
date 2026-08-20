package effect

import (
	"testing"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

func prototypeArchaic() gamedata.EffectRule {
	return gamedata.EffectRule{
		Key: "PrototypeArchaic", Kind: gamedata.EffectKindWeapon, Trigger: "always",
		Description: "On hit, Normal or Charged Attacks have a 50% chance to deal an " +
			"additional 240% ATK DMG. Can only occur once every 15s.",
		DescriptionByRefinement: []string{
			"...an additional 240% ATK DMG...",
			"...an additional 300% ATK DMG...",
			"...an additional 360% ATK DMG...",
			"...an additional 420% ATK DMG...",
			"...an additional 480% ATK DMG...",
		},
		Effects: []gamedata.Effect{{
			Grants: model.ATK, Phase: gamedata.EffectPhasePost,
			ByRefinement: []float64{2.4, 3.0, 3.6, 4.2, 4.8},
			StacksFrom:   "PrototypeArchaic.procs", MaxStacks: 10,
			Instance: &gamedata.EffectInstance{
				Label: "Prototype Archaic", Scaling: model.ATK, Element: model.Physical,
			},
		}},
	}
}

func archaicCtx(refinement int, procs float64) Context {
	ctx := Context{
		Snapshot:   &gamedata.Snapshot{},
		WeaponKey:  "PrototypeArchaic",
		Refinement: refinement,
	}
	if procs >= 0 {
		ctx.Conditions = map[string]float64{"PrototypeArchaic.procs": procs}
	}
	return ctx
}

func TestInstanceEffectAddsAHit(t *testing.T) {
	got, err := Instances([]gamedata.EffectRule{prototypeArchaic()},
		archaicCtx(1, 1), nil, model.Pyro)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d instances, want 1", len(got))
	}
	near(t, "R1 multiplier", got[0].Instance.Multiplier, 2.4)
	if got[0].Instance.Element != model.Physical {
		t.Errorf("element = %q; the proc is physical, not the character's", got[0].Instance.Element)
	}
	if got[0].Instance.Category != "" {
		t.Errorf("category = %q; a weapon proc is not a Normal Attack even when one triggers it",
			got[0].Instance.Category)
	}
}

func TestInstanceCountFoldsIntoTheMultiplier(t *testing.T) {
	// Every occurrence is identical, so three procs of 240% is one hit of
	// 720% — same expected damage, one line in the breakdown.
	got, err := Instances([]gamedata.EffectRule{prototypeArchaic()},
		archaicCtx(1, 3), nil, model.Pyro)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "three procs", got[0].Instance.Multiplier, 7.2)
}

func TestInstanceRespectsRefinement(t *testing.T) {
	got, _ := Instances([]gamedata.EffectRule{prototypeArchaic()},
		archaicCtx(5, 1), nil, model.Pyro)
	near(t, "R5 multiplier", got[0].Instance.Multiplier, 4.8)
}

func TestUndeclaredProcsAddNothing(t *testing.T) {
	// Nobody said the proc happens, so it does not. Assuming an occurrence
	// count would put damage in a build the player never claimed.
	got, err := Instances([]gamedata.EffectRule{prototypeArchaic()},
		archaicCtx(5, -1), nil, model.Pyro)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("an undeclared proc contributed damage: %+v", got)
	}
}

func TestInstanceCountIsClamped(t *testing.T) {
	got, _ := Instances([]gamedata.EffectRule{prototypeArchaic()},
		archaicCtx(1, 9999), nil, model.Pyro)
	near(t, "clamped to ten procs", got[0].Instance.Multiplier, 24.0)
}

func TestInstanceInheritsTheCharactersElement(t *testing.T) {
	rule := prototypeArchaic()
	rule.Effects[0].Instance.Element = ""
	got, _ := Instances([]gamedata.EffectRule{rule}, archaicCtx(1, 1), nil, model.Dendro)
	if got[0].Instance.Element != model.Dendro {
		t.Errorf("element = %q, want the character's dendro", got[0].Instance.Element)
	}
}

func TestInstanceEffectMustBePostPhase(t *testing.T) {
	// A hit may scale off a final stat, so running it before the totals
	// exist would read zeros and report no damage.
	bad := prototypeArchaic()
	bad.Effects[0].Phase = gamedata.EffectPhasePre

	if _, err := Instances([]gamedata.EffectRule{bad}, archaicCtx(1, 1), nil, model.Pyro); err == nil {
		t.Error("a pre-phase damage hit was accepted at evaluation")
	}
	if err := Validate([]gamedata.EffectRule{bad}); err == nil {
		t.Error("a pre-phase damage hit was accepted at load")
	}
}

func TestValidateStillChecksStatStackCaps(t *testing.T) {
	// The exemption for occurrence counts must not leak into stat effects,
	// where the cap is a number the game states.
	bad := gamedata.EffectRule{
		Key: "MarechausseeHunter", Kind: gamedata.EffectKindArtifactSet, Trigger: "4pc",
		Description: "CRIT Rate is increased by 12%. Max 3 stacks.",
		Effects: []gamedata.Effect{{
			Grants: model.CritRate, Phase: gamedata.EffectPhasePre,
			Rate: 0.12, StacksFrom: "x", MaxStacks: 7,
		}},
	}
	if err := Validate([]gamedata.EffectRule{bad}); err == nil {
		t.Error("an invented stack cap passed validation")
	}
}
