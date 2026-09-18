package advisor

import (
	"context"
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/model"
)

func statusRequest(t *testing.T) BuildStatusRequest {
	t.Helper()
	r := rankingRequest(t)
	return BuildStatusRequest{
		Snapshot:  r.Snapshot,
		Loadout:   r.Loadout,
		Inventory: r.Inventory,
	}
}

func TestCharacterStatusCoversEverySlotAndRanksThem(t *testing.T) {
	got, err := CharacterStatus(context.Background(), statusRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Slots) == 0 {
		t.Fatalf("no slots came back; skipped: %v", got.Skipped)
	}
	for i := 1; i < len(got.Slots); i++ {
		if got.Slots[i-1].Best < got.Slots[i].Best {
			t.Fatalf("slots are not ranked by what is available: %s (%v) before %s (%v)",
				got.Slots[i-1].Slot, got.Slots[i-1].Best, got.Slots[i].Slot, got.Slots[i].Best)
		}
	}
	for _, s := range got.Slots {
		t.Logf("%-8s best %+.2f%% via %-6s (level %+.2f%%, swap %+.2f%%)",
			s.Slot, s.Best*100, s.Action, s.LevelGain*100, s.SwapGain*100)
	}
}

// The number Sabrina asked for: "status på hvor godt karakteren er bygget".
// It has to sit between an empty build and the idealised one, or it is not a
// status, it is a decoration.
func TestBuiltIsAFractionOfTheIdealisedBuild(t *testing.T) {
	got, err := CharacterStatus(context.Background(), statusRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.Built <= 0 {
		t.Fatalf("Built is %v; a build wearing five artifacts does more than nothing", got.Built)
	}
	if got.Built > 1 {
		t.Errorf("Built is %v — this build beats one with perfect substats in every slot, which means the denominator is wrong", got.Built)
	}
	t.Logf("built: %.1f%% of the idealised build", got.Built*100)
}

// And it has to move the right way. A better piece in a slot must raise the
// status, or the status is not measuring the build.
func TestBuiltRisesWhenTheBuildImproves(t *testing.T) {
	ctx := context.Background()

	before, err := CharacterStatus(ctx, statusRequest(t))
	if err != nil {
		t.Fatal(err)
	}

	// Take the best swap the page found and actually make it, then ask again.
	var swap *SlotStatus
	for i := range before.Slots {
		if before.Slots[i].Action == "swap" && before.Slots[i].SwapTo != 0 {
			swap = &before.Slots[i]
			break
		}
	}
	if swap == nil {
		t.Skip("this fixture offers no swap to make")
	}

	req := statusRequest(t)
	var chosen model.Artifact
	for _, a := range req.Inventory {
		if a.ID == swap.SwapTo {
			chosen = a
			break
		}
	}
	equipped := make([]model.Artifact, 0, len(req.Loadout.Artifacts))
	for _, a := range req.Loadout.Artifacts {
		if a.SlotKey != swap.Slot {
			equipped = append(equipped, a)
		}
	}
	req.Loadout.Artifacts = append(equipped, chosen)

	after, err := CharacterStatus(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if after.Built <= before.Built {
		t.Errorf("built went %v → %v after making the upgrade the page recommended",
			before.Built, after.Built)
	}
	t.Logf("built %.1f%% → %.1f%% after taking the %s swap",
		before.Built*100, after.Built*100, swap.Slot)
}

// Levelling is priced main-stat-only on purpose, and the page has to say so.
// A number that silently leaves out the substat rolls is a number somebody
// will hold against it when the piece lands badly.
func TestLevellingGainStatesThatItUnderreports(t *testing.T) {
	got, err := CharacterStatus(context.Background(), statusRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	var said bool
	for _, c := range got.Caveats {
		if strings.Contains(c, "main stat's growth alone") {
			said = true
		}
	}
	if !said {
		t.Errorf("no caveat explains that levelling is priced on the main stat alone: %v", got.Caveats)
	}
}

func TestBuiltSaysItIsARulerRatherThanAGrade(t *testing.T) {
	got, err := CharacterStatus(context.Background(), statusRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	var said bool
	for _, c := range got.Caveats {
		if strings.Contains(c, "ruler, not a grade") {
			said = true
		}
	}
	if !said {
		t.Errorf("Built is reported with no warning that nothing reaches 100: %v", got.Caveats)
	}
}

// A slot the character is not wearing anything in is the one place "upgrade"
// means "put something on", and the action has to say that rather than
// reporting a level-up of a piece that is not there.
func TestAnEmptySlotAsksForSomethingToBeEquipped(t *testing.T) {
	req := statusRequest(t)
	kept := make([]model.Artifact, 0, len(req.Loadout.Artifacts))
	var dropped model.Slot
	for _, a := range req.Loadout.Artifacts {
		if dropped == "" {
			dropped = a.SlotKey
			continue
		}
		kept = append(kept, a)
	}
	req.Loadout.Artifacts = kept

	got, err := CharacterStatus(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got.Slots {
		if s.Slot != dropped {
			continue
		}
		if s.Worn != nil {
			t.Fatalf("%s reports a worn piece after it was taken off", dropped)
		}
		if s.LevelGain != 0 {
			t.Errorf("%s prices levelling a piece that is not equipped: %v", dropped, s.LevelGain)
		}
		if s.SwapGain > 0 && s.Action != "equip" {
			t.Errorf("%s has a piece available but the action is %q, want \"equip\"", dropped, s.Action)
		}
		return
	}
	t.Fatalf("the emptied slot %s is missing from the page entirely", dropped)
}

func TestCharacterStatusRefusesWithoutASnapshot(t *testing.T) {
	if _, err := CharacterStatus(context.Background(), BuildStatusRequest{}); err == nil {
		t.Fatal("built a character page with no game data")
	}
}

// The half Sabrina actually asked about: "hvilke stykker får jeg mest ud af at
// opgradere". The shared fixture equips nothing but +20 pieces, so levelling
// never fires there and this builds the case it misses.
func TestAnUnlevelledPieceIsPricedAndNamedAsALevelUp(t *testing.T) {
	req := statusRequest(t)

	// Drop one worn piece to +0 and take its substats off with it: a piece
	// that low has not rolled them yet, and leaving them on would price the
	// main stat's growth against stats the piece would not have.
	var lowered model.Slot
	for i := range req.Loadout.Artifacts {
		if req.Loadout.Artifacts[i].SlotKey == model.Goblet {
			req.Loadout.Artifacts[i].Level = 0
			req.Loadout.Artifacts[i].Substats = nil
			lowered = model.Goblet
			break
		}
	}
	if lowered == "" {
		t.Fatal("the fixture has no goblet to lower")
	}

	got, err := CharacterStatus(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got.Slots {
		if s.Slot != lowered {
			continue
		}
		if s.LevelGain <= 0 {
			t.Fatalf("a +0 goblet gains nothing from being levelled: %+v", s)
		}
		if s.LevelTo != 20 {
			t.Errorf("levelling a 5★ goes to +%d, want +20", s.LevelTo)
		}
		t.Logf("levelling the +0 goblet: %+.2f%% (swap offers %+.2f%%, page says %q)",
			s.LevelGain*100, s.SwapGain*100, s.Action)
		return
	}
	t.Fatalf("the lowered slot %s is missing from the page", lowered)
}

// Sabrina, after being given the page: "Tager den nye model kun udgangspunkt i
// ting/stykker jeg har — fordi jeg vil gerne have at den anbefaler det bedste
// artifact sæt til mine karaktere i stedet for kun at kigge på artifacts jeg
// ejer?"
//
// Most of the page is about the bag, deliberately. This is the half that is
// not, and it has to be on the page rather than on another screen — an answer
// somewhere else is the same as no answer.
func TestThePageCarriesWhatToFarmTowardsIndependentOfTheBag(t *testing.T) {
	got, err := CharacterStatus(context.Background(), statusRequest(t))
	if err != nil {
		t.Fatalf("%v (skipped: %v)", err, got.Skipped)
	}
	if got.Aim == nil {
		t.Fatalf("the page has no aim on it; skipped: %v", got.Skipped)
	}
	if len(got.Aim.Sets) == 0 {
		t.Fatal("the aim recommends no sets at all")
	}
	if len(got.Aim.MainStats) == 0 {
		t.Error("the aim names no main stats to farm for")
	}
}

// And it must not quietly prefer what the account already has. Owned is a
// label on the answer, never a thumb on the scale — otherwise "what should I
// farm towards" collapses back into "what have you got", which is the
// question the rest of the page already answers.
func TestTheAimDoesNotPreferSetsTheAccountOwns(t *testing.T) {
	ctx := context.Background()

	req := statusRequest(t)
	owned, err := CharacterStatus(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	// Same request, but claiming the account owns nothing at all. The
	// ranking and the scores must be identical; only the labels may move.
	bare := statusRequest(t)
	bare.OwnedSets = map[string]bool{}
	none, err := CharacterStatus(ctx, bare)
	if err != nil {
		t.Fatal(err)
	}

	if len(owned.Aim.Sets) != len(none.Aim.Sets) {
		t.Fatalf("ownership changed how many sets were recommended: %d vs %d",
			len(owned.Aim.Sets), len(none.Aim.Sets))
	}
	for i := range owned.Aim.Sets {
		a, b := owned.Aim.Sets[i], none.Aim.Sets[i]
		if a.Config != b.Config {
			t.Errorf("position %d is %s when the sets are owned and %s when they are not",
				i, a.Config, b.Config)
		}
		if a.Score != b.Score {
			t.Errorf("%s scored %v owned and %v unowned; ownership must not touch the score",
				a.Config, a.Score, b.Score)
		}
	}
}

// The limit that matters more than the ranking. Most four-piece bonuses are
// conditional prose rather than numbers, so most entries are ranked on their
// stats alone — and a reader who takes "best" at face value has been misled
// by omission. Both the per-entry flag and the count have to survive onto
// the page.
func TestTheAimSaysHowMuchOfItIsActuallyModelled(t *testing.T) {
	got, err := CharacterStatus(context.Background(), statusRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	var counted bool
	for _, c := range got.Aim.Caveats {
		if strings.Contains(c, "four-piece bonus with numbers behind it") {
			counted = true
		}
	}
	if !counted {
		t.Errorf("the aim never says how many sets it can actually score: %v", got.Aim.Caveats)
	}
}
