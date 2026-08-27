package advisor

import (
	"context"
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
	"github.com/kristianwind/mimir/internal/optimizer"
)

// The same miniature game the plan tests use, minus the goal — which is the
// whole difference between the two views.
func potentialRequest(t *testing.T) PotentialRequest {
	t.Helper()
	req := planRequest(t)
	return PotentialRequest{
		Snapshot:      req.Snapshot,
		Loadout:       req.Loadout,
		Inventory:     req.Inventory,
		Weapons:       req.Weapons,
		MaxSetConfigs: 4,
	}
}

// The whole point of the yardstick: a character with no goal still gets a
// number. The plan cannot see them at all.
func TestAssessNeedsNoRotation(t *testing.T) {
	got, err := Assess(context.Background(), potentialRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.Current <= 0 {
		t.Fatalf("current = %v, want a positive score", got.Current)
	}
	if got.Character == "" || got.Element == "" {
		t.Errorf("assessment does not identify what it measured: %+v", got)
	}
}

// A talent upgrade has to move the score, or "which character is worth
// investing in" cannot see half of what investment means. This is why the
// yardstick uses the real talent multipliers rather than a flat 100%.
func TestTalentLevelsMoveTheYardstick(t *testing.T) {
	req := potentialRequest(t)

	low, err := Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	req.Loadout.Character.TalentSkill++
	req.Loadout.Character.TalentBurst++
	high, err := Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	if high.Current <= low.Current {
		t.Errorf("levelling two talents did not raise the score: %v then %v",
			low.Current, high.Current)
	}
}

// Resin must not enter the ordering here. A talent book that costs 20 resin
// and gains 8% belongs above a free re-equip that gains 3%, which is the
// opposite of what the daily plan does.
func TestRankByGainIgnoresResin(t *testing.T) {
	got := RankByGain([]Action{
		{Kind: KindReequip, Headline: "free but small", GainPct: 0.03},
		{Kind: KindTalent, Headline: "costs resin, larger", GainPct: 0.08, ResinCost: 20},
		{Kind: KindTalent, Headline: "blocked, largest", GainPct: 0.30, ResinCost: 20, BlockedBy: "needs a crown"},
	})

	if got[0].Headline != "costs resin, larger" {
		t.Errorf("first = %q, want the larger paid gain", got[0].Headline)
	}
	if got[1].Headline != "free but small" {
		t.Errorf("second = %q", got[1].Headline)
	}
	// Blocked still trails: an action you cannot carry out is not an answer.
	if got[2].BlockedBy == "" {
		t.Errorf("a blocked action did not trail: %+v", got)
	}
}

// "If an artifact piece needs upgrading, let me know."
func TestUnlevelledPiecesAreReported(t *testing.T) {
	req := potentialRequest(t)
	for i := range req.Loadout.Artifacts {
		req.Loadout.Artifacts[i].Level = 0
	}
	req.Inventory = req.Loadout.Artifacts

	got, err := Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	var levelling []Action
	for _, a := range got.Actions {
		if a.Kind == KindArtifact {
			levelling = append(levelling, a)
		}
	}
	if len(levelling) == 0 {
		t.Fatalf("no levelling suggested for a build of +0 pieces: %+v", got.Actions)
	}
	for _, a := range levelling {
		if a.ResinCost != 0 {
			t.Errorf("%q was priced in resin; artifact experience is not resin", a.Headline)
		}
		if !strings.Contains(a.Note, "substat") {
			t.Errorf("%q does not say the substat rolls are unprojected: %q", a.Headline, a.Note)
		}
		if a.Detail["to"] == nil || a.Detail["from"] == nil {
			t.Errorf("%q does not say which levels: %v", a.Headline, a.Detail)
		}
	}
}

// A piece already at its cap is not an upgrade, and the cap comes from the
// mined table rather than from a constant in the source.
func TestAPieceAtItsCapIsNotSuggested(t *testing.T) {
	req := potentialRequest(t)
	cap, err := maxArtifactLevel(req.Snapshot, req.Loadout.Artifacts[0])
	if err != nil {
		t.Fatal(err)
	}
	for i := range req.Loadout.Artifacts {
		req.Loadout.Artifacts[i].Level = cap
	}
	req.Inventory = req.Loadout.Artifacts

	got, err := Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range got.Actions {
		if a.Kind == KindArtifact {
			t.Errorf("suggested levelling a piece already at +%d: %q", cap, a.Headline)
		}
	}
}

// The yardstick has to be the same ruler for every character, or the ranking
// compares nothing. Two runs of the same build must agree exactly.
func TestTheYardstickIsStable(t *testing.T) {
	req := potentialRequest(t)
	first, err := Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Current != second.Current {
		t.Errorf("the same build scored %v then %v", first.Current, second.Current)
	}
}

// A character whose talents carry no mined damage rows cannot be measured, and
// that has to be an error naming the gap rather than a zero that ranks last.
func TestTheYardstickRefusesACharacterWithNoDamageRows(t *testing.T) {
	req := potentialRequest(t)
	key := req.Loadout.Character.Key

	chars := make(map[string]gamedata.Character, len(req.Snapshot.Characters))
	for k, v := range req.Snapshot.Characters {
		chars[k] = v
	}
	stripped := chars[key]
	stripped.Talents = map[string]gamedata.Talent{}
	chars[key] = stripped

	snap := *req.Snapshot
	snap.Characters = chars
	req.Snapshot = &snap

	_, err := Assess(context.Background(), req)
	if err == nil {
		t.Fatal("a character with no damage rows was scored anyway")
	}
	if !strings.Contains(err.Error(), "nothing to measure") {
		t.Errorf("the error does not name the gap: %v", err)
	}
}

// The ranking answers "where does one upgrade buy the most damage", so a
// character still wearing the worse half of the inventory must outrank one
// already wearing the better half.
func TestTheRankingLeadsWithTheBiggestUpgrade(t *testing.T) {
	neglected := potentialRequest(t)

	// The same character, but wearing the good tier of every slot, so the
	// optimizer has far less left to find.
	settled := potentialRequest(t)
	var equipped []model.Artifact
	for i, a := range settled.Inventory {
		if a.SetKey == "A" && i%2 == 1 {
			equipped = append(equipped, a)
		}
	}
	if len(equipped) != len(model.Slots) {
		t.Fatalf("fixture gave %d pieces, want one per slot", len(equipped))
	}
	settled.Loadout.Artifacts = equipped

	got, err := AccountPotential(context.Background(), []PotentialRequest{settled, neglected})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Characters) != 2 {
		t.Fatalf("measured %d characters, want 2", len(got.Characters))
	}
	if got.Characters[0].TopGain < got.Characters[1].TopGain {
		t.Errorf("the ranking is not sorted by the biggest upgrade: %v then %v",
			got.Characters[0].TopGain, got.Characters[1].TopGain)
	}
	if got.Characters[0].TopAction == nil {
		t.Fatal("the leader does not name the upgrade that put it there")
	}
	// Headroom is reported separately and deliberately does not decide the
	// order: the same upgrade buys more absolute damage on a strong build, so
	// a settled character can lead while a neglected one has all the room.
	// Both numbers are needed to read the ranking correctly.
	var withRoom bool
	for _, c := range got.Characters {
		if c.Headroom > 0 {
			withRoom = true
		}
	}
	if !withRoom {
		t.Error("neither character reports headroom, so the neglected one is invisible")
	}
}

// A ranking with no stated limits reads as a verdict on who is worth playing,
// which is not what it is.
func TestTheRankingStatesWhatItDoesNotMeasure(t *testing.T) {
	got, err := AccountPotential(context.Background(), []PotentialRequest{potentialRequest(t)})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got.Caveats, " ")
	for _, must := range []string{"Normal attacks", "not added together", "Resin is not"} {
		if !strings.Contains(joined, must) {
			t.Errorf("the caveats do not mention %q: %v", must, got.Caveats)
		}
	}
}

// One broken character must not take the whole roster's ranking with it.
func TestTheRankingSurvivesOneBrokenCharacter(t *testing.T) {
	good := potentialRequest(t)
	broken := potentialRequest(t)
	broken.Loadout.Character.Key = "NotInTheSnapshot"

	got, err := AccountPotential(context.Background(), []PotentialRequest{broken, good})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Characters) != 1 {
		t.Errorf("measured %d characters, want the one that works", len(got.Characters))
	}
	if len(got.Skipped) != 1 || !strings.Contains(got.Skipped[0], "NotInTheSnapshot") {
		t.Errorf("the broken character is not named in skipped: %v", got.Skipped)
	}
}

// A derived goal has to score the character exactly as the potential view did,
// or the ranking changes the moment you act on it.
func TestADerivedSpecMatchesTheYardstick(t *testing.T) {
	req := potentialRequest(t)
	ch := req.Loadout.Character

	spec, err := DeriveSpec(req.Snapshot, ch)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Steps) == 0 {
		t.Fatal("derived an empty rotation")
	}
	for _, s := range spec.Steps {
		if s.Hits != 1 {
			t.Errorf("step %q claims %d hits; the yardstick is one cast of each", s.Entry, s.Hits)
		}
	}

	// It must resolve against the real talent tables, or the goal will not
	// save and the whole derivation is theatre.
	rot, err := BuildRotation(req.Snapshot, ch, spec)
	if err != nil {
		t.Fatalf("the derived rotation does not resolve: %v", err)
	}

	yard, err := yardstickRotation(req.Snapshot, ch)
	if err != nil {
		t.Fatal(err)
	}
	if len(rot.Instances) != len(yard.Instances) {
		t.Fatalf("derived %d instances, the yardstick has %d",
			len(rot.Instances), len(yard.Instances))
	}
	for i := range rot.Instances {
		if rot.Instances[i].Multiplier != yard.Instances[i].Multiplier {
			t.Errorf("instance %d: derived %v, yardstick %v",
				i, rot.Instances[i].Multiplier, yard.Instances[i].Multiplier)
		}
	}
}

// The artifact search has to be optimising against something.
//
// It used to be handed a goal with no rotation. An empty rotation has no
// damage instances, so the objective summed over nothing and returned zero
// for every build it was offered — the search kept whichever arrangement it
// saw first and called it best. The gain reported afterwards was measured
// honestly with the yardstick, which is exactly what made it hard to see: a
// real number attached to an arbitrary build.
func TestTheArtifactSearchHasSomethingToOptimise(t *testing.T) {
	req := potentialRequest(t)
	base, err := Assemble(req.Snapshot, req.Loadout)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := DeriveSpec(req.Snapshot, base.Character)
	if err != nil {
		t.Fatal(err)
	}

	obj := statObjective(Request{
		Snapshot: req.Snapshot,
		Goal:     Goal{CharacterKey: base.Character.Key, Spec: spec},
		Loadout:  req.Loadout,
	}, base, optimizer.SetConfig{})

	// Two different stat blocks must not score the same, and neither may be
	// zero: a flat objective is indistinguishable from no objective at all.
	poor := obj(model.StatBlock{})
	rich := obj(model.StatBlock{model.ATKPercent: 1.0, model.CritRate: 0.5, model.CritDMG: 1.0})
	if poor <= 0 {
		t.Fatalf("the objective scores a build at %v; it is measuring nothing", poor)
	}
	if rich <= poor {
		t.Errorf("more attack and crit scored %v against %v; the objective is flat", rich, poor)
	}
}
