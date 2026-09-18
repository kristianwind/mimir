package effect

import (
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// A rule can be word-perfect about the game and still be worth nothing.
//
// Transformative reaction bonuses are the live case: the wording is right, the
// numbers check out, and calc.Transformative — which would price an overload —
// is called from nowhere. A rule made only of those would flip
// FourPieceModelled to true, take the "ranked on stats alone" warning off the
// set, and leave every number exactly as it was.
func TestARuleThatGrantsNothingTheEngineReadsIsRejected(t *testing.T) {
	dead := gamedata.EffectRule{
		Key: "ThunderingFury", Kind: gamedata.EffectKindArtifactSet, Trigger: "4pc",
		Description: "Increases the DMG caused by Overloaded, Electro-Charged, Superconduct, and Hyperbloom by 40%.",
		Effects: []gamedata.Effect{
			{Grants: model.ReactionBonus("overloaded"), Phase: gamedata.EffectPhasePost, Rate: 0.4},
			{Grants: model.ReactionBonus("hyperbloom"), Phase: gamedata.EffectPhasePost, Rate: 0.4},
		},
	}
	err := Validate([]gamedata.EffectRule{dead})
	if err == nil {
		t.Fatal("a rule granting only unpriced reactions was accepted")
	}
	if !strings.Contains(err.Error(), "nothing the damage engine reads") {
		t.Errorf("rejected for the wrong reason: %v", err)
	}
}

// But a rule that mixes them is fine, and must stay fine. Crimson Witch is
// exactly this shape, and deleting its overload wording to satisfy a checker
// would be throwing away correct game data because the engine has not caught
// up with it yet.
func TestARuleMixingLiveAndUnpricedGrantsIsAccepted(t *testing.T) {
	mixed := gamedata.EffectRule{
		Key: "CrimsonWitchOfFlames", Kind: gamedata.EffectKindArtifactSet, Trigger: "4pc",
		Description: "Increases Overloaded and Burning DMG by 40%. Increases Vaporize and Melt DMG by 15%.",
		Effects: []gamedata.Effect{
			{Grants: model.ReactionBonus("overloaded"), Phase: gamedata.EffectPhasePost, Rate: 0.4},
			{Grants: model.ReactionBonus("vaporize"), Phase: gamedata.EffectPhasePost, Rate: 0.15},
		},
	}
	if err := Validate([]gamedata.EffectRule{mixed}); err != nil {
		t.Fatalf("a rule with one live grant among unpriced ones was rejected: %v", err)
	}
}
