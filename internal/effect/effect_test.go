package effect

import (
	"math"
	"testing"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

func ptr(v float64) *float64 { return &v }

func near(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

func emblem4pc() gamedata.EffectRule {
	return gamedata.EffectRule{
		Key: "EmblemOfSeveredFate", Kind: gamedata.EffectKindArtifactSet, Trigger: "4pc",
		Description: "Increases Elemental Burst DMG by 25% of Energy Recharge. " +
			"A maximum of 75% bonus DMG can be obtained in this way.",
		Effects: []gamedata.Effect{{
			Grants: model.BurstDMG, Phase: gamedata.EffectPhasePost,
			From: model.EnergyRecharge, Rate: 0.25, Max: ptr(0.75),
		}},
	}
}

func raidenPassive() gamedata.EffectRule {
	return gamedata.EffectRule{
		Key: "RaidenShogun", Kind: gamedata.EffectKindCharacter, Trigger: "passive2",
		MinAscension: 4,
		Description: "Each 1% above 100% Energy Recharge that the Raiden Shogun possesses grants her:\n" +
			"·0.6% greater Energy restoration from Musou Isshin\n·0.4% Electro DMG Bonus.",
		Effects: []gamedata.Effect{{
			Grants: model.ElectroDMG, Phase: gamedata.EffectPhasePost,
			From: model.EnergyRecharge, Rate: 0.4, Offset: 1.0, Min: ptr(0),
		}},
	}
}

func raidenCtx(ascension, constellation int) Context {
	return Context{
		Snapshot:  &gamedata.Snapshot{},
		Character: model.Character{Key: "RaidenShogun", Ascension: ascension, Constellation: constellation},
		SetCounts: map[string]int{"EmblemOfSeveredFate": 4},
	}
}

func TestEmblemConvertsEnergyRechargeToBurstDamage(t *testing.T) {
	stats := model.StatBlock{model.EnergyRecharge: 2.0}
	got, err := Apply([]gamedata.EffectRule{emblem4pc()}, raidenCtx(6, 0), gamedata.EffectPhasePost, stats)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "burst dmg at 200% ER", got[model.BurstDMG], 0.50)
}

func TestEmblemRespectsItsCap(t *testing.T) {
	// 400% ER would convert to 100% without the cap the set text states.
	stats := model.StatBlock{model.EnergyRecharge: 4.0}
	got, _ := Apply([]gamedata.EffectRule{emblem4pc()}, raidenCtx(6, 0), gamedata.EffectPhasePost, stats)
	near(t, "capped burst dmg", got[model.BurstDMG], 0.75)
}

func TestEmblemNeedsFourPieces(t *testing.T) {
	ctx := raidenCtx(6, 0)
	ctx.SetCounts = map[string]int{"EmblemOfSeveredFate": 3}
	got, _ := Apply([]gamedata.EffectRule{emblem4pc()}, ctx, gamedata.EffectPhasePost, model.StatBlock{model.EnergyRecharge: 2.0})
	if got[model.BurstDMG] != 0 {
		t.Errorf("three pieces granted the four-piece bonus: %v", got[model.BurstDMG])
	}
}

func TestRaidenConvertsExcessEnergyRecharge(t *testing.T) {
	stats := model.StatBlock{model.EnergyRecharge: 2.5}
	got, err := Apply([]gamedata.EffectRule{raidenPassive()}, raidenCtx(6, 0), gamedata.EffectPhasePost, stats)
	if err != nil {
		t.Fatal(err)
	}
	// 150 percentage points above 100%, at 0.4% each.
	near(t, "electro dmg", got[model.ElectroDMG], 0.60)
}

func TestRaidenPassiveIsGatedOnAscension(t *testing.T) {
	stats := model.StatBlock{model.EnergyRecharge: 2.5}
	got, _ := Apply([]gamedata.EffectRule{raidenPassive()}, raidenCtx(3, 0), gamedata.EffectPhasePost, stats)
	if got[model.ElectroDMG] != 0 {
		t.Error("an ascension 4 passive fired on an ascension 3 character")
	}
}

func TestRaidenPassiveDoesNotGoNegative(t *testing.T) {
	// Not reachable in game — base ER is 100% — but a floor is cheaper than
	// a build page that reports negative Electro DMG.
	stats := model.StatBlock{model.EnergyRecharge: 0.5}
	got, _ := Apply([]gamedata.EffectRule{raidenPassive()}, raidenCtx(6, 0), gamedata.EffectPhasePost, stats)
	near(t, "floored", got[model.ElectroDMG], 0)
}

func TestEffectsOnlyApplyToTheirOwnCharacter(t *testing.T) {
	ctx := raidenCtx(6, 0)
	ctx.Character.Key = "Xiangling"
	got, _ := Apply([]gamedata.EffectRule{raidenPassive()}, ctx, gamedata.EffectPhasePost, model.StatBlock{model.EnergyRecharge: 2.0})
	if got[model.ElectroDMG] != 0 {
		t.Error("Raiden's passive fired on Xiangling")
	}
}

func TestStacksComeFromDeclaredConditions(t *testing.T) {
	rule := gamedata.EffectRule{
		Key: "MarechausseeHunter", Kind: gamedata.EffectKindArtifactSet, Trigger: "4pc",
		Description: "CRIT Rate is increased by 12% for 5s. Max 3 stacks.",
		Effects: []gamedata.Effect{{
			Grants: model.CritRate, Phase: gamedata.EffectPhasePre,
			Rate: 0.12, StacksFrom: "MarechausseeHunter.stacks", MaxStacks: 3,
		}},
	}
	ctx := Context{
		Snapshot:  &gamedata.Snapshot{},
		SetCounts: map[string]int{"MarechausseeHunter": 4},
	}

	// Undeclared means off. Assuming full uptime is how a tool quietly
	// overstates every build wearing a conditional set.
	got, _ := Apply([]gamedata.EffectRule{rule}, ctx, gamedata.EffectPhasePre, nil)
	near(t, "undeclared", got[model.CritRate], 0)

	ctx.Conditions = map[string]float64{"MarechausseeHunter.stacks": 2}
	got, _ = Apply([]gamedata.EffectRule{rule}, ctx, gamedata.EffectPhasePre, nil)
	near(t, "two stacks", got[model.CritRate], 0.24)

	ctx.Conditions = map[string]float64{"MarechausseeHunter.stacks": 9}
	got, _ = Apply([]gamedata.EffectRule{rule}, ctx, gamedata.EffectPhasePre, nil)
	near(t, "clamped to three", got[model.CritRate], 0.36)
}

func TestConstellationTrigger(t *testing.T) {
	rule := gamedata.EffectRule{
		Key: "RaidenShogun", Kind: gamedata.EffectKindCharacter, Trigger: "c2",
		Description: "Ignores 60% of opponents' DEF.",
		Effects: []gamedata.Effect{{
			Grants: model.CritRate, Phase: gamedata.EffectPhasePre, Rate: 0.6,
		}},
	}
	if Applies(rule, raidenCtx(6, 1)) {
		t.Error("a C2 effect fired at C1")
	}
	if !Applies(rule, raidenCtx(6, 2)) {
		t.Error("a C2 effect did not fire at C2")
	}
}

func TestValidateAcceptsQuotedNumbers(t *testing.T) {
	if err := Validate([]gamedata.EffectRule{emblem4pc(), raidenPassive()}); err != nil {
		t.Errorf("rules whose numbers appear verbatim were rejected: %v", err)
	}
}

func TestValidateRejectsInventedNumbers(t *testing.T) {
	bad := emblem4pc()
	bad.Effects[0].Rate = 0.35 // the text says 25%

	err := Validate([]gamedata.EffectRule{bad})
	if err == nil {
		t.Fatal("a fabricated multiplier passed validation")
	}
	if !contains(err.Error(), "0.35") {
		t.Errorf("the error should name the unverifiable value, got %q", err)
	}
}

func TestValidateRejectsAnUncitedRule(t *testing.T) {
	bad := emblem4pc()
	bad.Description = ""
	if err := Validate([]gamedata.EffectRule{bad}); err == nil {
		t.Error("a rule with no citation passed validation")
	}
}

func TestValidateIgnoresToggleStacks(t *testing.T) {
	rule := gamedata.EffectRule{
		Key: "NoblesseOblige", Kind: gamedata.EffectKindArtifactSet, Trigger: "4pc",
		Description: "Using an Elemental Burst increases all party members' ATK by 20% for 12s.",
		Effects: []gamedata.Effect{{
			Grants: model.ATKPercent, Phase: gamedata.EffectPhasePre,
			Rate: 0.2, StacksFrom: "NoblesseOblige.active", MaxStacks: 1,
		}},
	}
	if err := Validate([]gamedata.EffectRule{rule}); err != nil {
		t.Errorf("a boolean toggle was treated as a claim about the game: %v", err)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

func TestUndeclaredNamesTheQuestionsNobodyAnswered(t *testing.T) {
	rules := []gamedata.EffectRule{
		emblem4pc(), // no conditions
		{
			Key: "MarechausseeHunter", Kind: gamedata.EffectKindArtifactSet, Trigger: "4pc",
			Description: "CRIT Rate is increased by 12%. Max 3 stacks.",
			Effects: []gamedata.Effect{{
				Grants: model.CritRate, Phase: gamedata.EffectPhasePre,
				Rate: 0.12, StacksFrom: "MarechausseeHunter.stacks", MaxStacks: 3,
				Note: "stacks per HP change",
			}},
		},
	}
	ctx := Context{
		Snapshot: &gamedata.Snapshot{},
		SetCounts: map[string]int{
			"EmblemOfSeveredFate": 4, "MarechausseeHunter": 4,
		},
	}

	missing := Undeclared(rules, ctx)
	if len(missing) != 1 {
		t.Fatalf("got %d missing conditions, want 1: %+v", len(missing), missing)
	}
	if missing[0].Key != "MarechausseeHunter.stacks" || missing[0].MaxStacks != 3 {
		t.Errorf("missing = %+v", missing[0])
	}
	if missing[0].Note == "" {
		t.Error("a missing condition should carry the effect's own explanation")
	}

	// Declaring zero is an answer — "I never have stacks" — and must not be
	// nagged about.
	ctx.Conditions = map[string]float64{"MarechausseeHunter.stacks": 0}
	if got := Undeclared(rules, ctx); len(got) != 0 {
		t.Errorf("a declared zero was reported as unanswered: %+v", got)
	}
}

func TestUndeclaredIgnoresSetsNotWorn(t *testing.T) {
	rules := []gamedata.EffectRule{{
		Key: "MarechausseeHunter", Kind: gamedata.EffectKindArtifactSet, Trigger: "4pc",
		Description: "CRIT Rate is increased by 12%. Max 3 stacks.",
		Effects: []gamedata.Effect{{
			Grants: model.CritRate, Phase: gamedata.EffectPhasePre,
			Rate: 0.12, StacksFrom: "MarechausseeHunter.stacks", MaxStacks: 3,
		}},
	}}
	ctx := Context{Snapshot: &gamedata.Snapshot{}, SetCounts: map[string]int{"Gladiator": 4}}
	if got := Undeclared(rules, ctx); len(got) != 0 {
		t.Errorf("asked about a set the build is not wearing: %+v", got)
	}
}

func theCatch() gamedata.EffectRule {
	return gamedata.EffectRule{
		Key: "TheCatch", Kind: gamedata.EffectKindWeapon, Trigger: "always",
		Description: "Increases Elemental Burst DMG by 16/20/24/28/32% and " +
			"Elemental Burst CRIT Rate by 6/7.5/9/10.5/12%.",
		DescriptionByRefinement: []string{
			"Increases Elemental Burst DMG by 16% and Elemental Burst CRIT Rate by 6%.",
			"Increases Elemental Burst DMG by 20% and Elemental Burst CRIT Rate by 7.5%.",
			"Increases Elemental Burst DMG by 24% and Elemental Burst CRIT Rate by 9%.",
			"Increases Elemental Burst DMG by 28% and Elemental Burst CRIT Rate by 10.5%.",
			"Increases Elemental Burst DMG by 32% and Elemental Burst CRIT Rate by 12%.",
		},
		Effects: []gamedata.Effect{
			{
				Grants: model.BurstDMG, Phase: gamedata.EffectPhasePre,
				ByRefinement: []float64{0.16, 0.20, 0.24, 0.28, 0.32},
			},
			{
				Grants:       model.CategoryScoped(model.CategoryBurst, model.CritRate),
				Phase:        gamedata.EffectPhasePre,
				ByRefinement: []float64{0.06, 0.075, 0.09, 0.105, 0.12},
			},
		},
	}
}

func TestWeaponPassiveScalesWithRefinement(t *testing.T) {
	rules := []gamedata.EffectRule{theCatch()}
	for refinement, wantDMG := range map[int]float64{1: 0.16, 3: 0.24, 5: 0.32} {
		ctx := Context{
			Snapshot:   &gamedata.Snapshot{},
			WeaponKey:  "TheCatch",
			Refinement: refinement,
		}
		got, err := Apply(rules, ctx, gamedata.EffectPhasePre, nil)
		if err != nil {
			t.Fatal(err)
		}
		near(t, "burst dmg at R"+string(rune('0'+refinement)), got[model.BurstDMG], wantDMG)
	}
}

func TestWeaponPassiveNeedsThatWeaponEquipped(t *testing.T) {
	ctx := Context{Snapshot: &gamedata.Snapshot{}, WeaponKey: "SacrificialSword", Refinement: 5}
	got, _ := Apply([]gamedata.EffectRule{theCatch()}, ctx, gamedata.EffectPhasePre, nil)
	if len(got) != 0 {
		t.Errorf("The Catch's passive fired on a different weapon: %v", got)
	}
}

func TestWeaponPassiveClampsAnOutOfRangeRefinement(t *testing.T) {
	// A refinement of 0 (unset) must not index out of the table.
	ctx := Context{Snapshot: &gamedata.Snapshot{}, WeaponKey: "TheCatch"}
	got, err := Apply([]gamedata.EffectRule{theCatch()}, ctx, gamedata.EffectPhasePre, nil)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "unset refinement falls back to R1", got[model.BurstDMG], 0.16)
}

func TestValidateChecksEachRefinementAgainstItsOwnText(t *testing.T) {
	if err := Validate([]gamedata.EffectRule{theCatch()}); err != nil {
		t.Fatalf("correct refinement values were rejected: %v", err)
	}

	// The R5 value moved into the R1 slot. Checking against the joined text
	// would accept this, because 32 does appear somewhere in it.
	bad := theCatch()
	bad.Effects[0].ByRefinement = []float64{0.32, 0.20, 0.24, 0.28, 0.32}
	err := Validate([]gamedata.EffectRule{bad})
	if err == nil {
		t.Fatal("an R5 value passed as an R1 claim")
	}
	if !contains(err.Error(), "R1") {
		t.Errorf("the error should name the refinement, got %q", err)
	}
}

func TestValidateRejectsRefinementValuesWithNoWording(t *testing.T) {
	bad := theCatch()
	bad.DescriptionByRefinement = bad.DescriptionByRefinement[:2]
	if err := Validate([]gamedata.EffectRule{bad}); err == nil {
		t.Error("refinement values with no text to check against were accepted")
	}
}

func TestValidateRejectsAnEmptyRule(t *testing.T) {
	bad := theCatch()
	bad.Effects = nil
	if err := Validate([]gamedata.EffectRule{bad}); err == nil {
		t.Error("a rule that grants nothing was accepted")
	}
}
