package advisor

import (
	"context"
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/model"
)

func rankingRequest(t *testing.T) ArtifactRankingRequest {
	t.Helper()
	req := planRequest(t)
	snap := req.Snapshot
	// BuildTarget supplies the ceiling, and it refuses without the roll
	// counts — a target build has no consistent substat treatment without
	// them, so there would be nothing fair to compare candidates against.
	snap.ArtifactRolls = map[int]int{5: 9}
	return ArtifactRankingRequest{
		Snapshot:  snap,
		Loadout:   req.Loadout,
		Inventory: req.Inventory,
	}
}

// The reason this exists. The plan needs a goal and a rotation; the tester who
// asked for this had neither on the character she was asking about.
func TestRankArtifactsNeedsNoGoal(t *testing.T) {
	got, err := RankArtifacts(context.Background(), rankingRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Slots) == 0 {
		t.Fatalf("nothing was scored; skipped: %v", got.Skipped)
	}
	var pieces int
	for _, s := range got.Slots {
		pieces += len(s.Pieces)
	}
	if pieces == 0 {
		t.Fatalf("slots came back with no pieces in them: %+v", got.Slots)
	}
}

// The scale has to mean something. Zero is an empty slot, so a real piece in a
// slot that matters scores above it, and nothing should come back as a wild
// number that would render as a broken bar.
func TestScoresAreOnASaneScale(t *testing.T) {
	got, err := RankArtifacts(context.Background(), rankingRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got.Slots {
		for _, p := range s.Pieces {
			if p.Score < 0 {
				t.Errorf("%s %d scored %.1f — below an empty slot", p.Slot, p.ArtifactID, p.Score)
			}
			if p.Score > 300 {
				t.Errorf("%s %d scored %.1f, which is not a score anybody can read", p.Slot, p.ArtifactID, p.Score)
			}
		}
	}
}

// Best first, per slot. A ranking that is not ordered is a list.
func TestPiecesAreRankedWithinASlot(t *testing.T) {
	got, err := RankArtifacts(context.Background(), rankingRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got.Slots {
		for i := 1; i < len(s.Pieces); i++ {
			if s.Pieces[i-1].Score < s.Pieces[i].Score {
				t.Fatalf("%s is out of order at %d: %.1f then %.1f",
					s.Slot, i, s.Pieces[i-1].Score, s.Pieces[i].Score)
			}
		}
	}
}

// A candidate must be scored in a build that does not still contain the piece
// it is replacing. Getting this wrong would have every candidate measured with
// two artifacts in one slot, which reads as an enormous upgrade.
func TestOnlyOnePieceOccupiesASlot(t *testing.T) {
	req := rankingRequest(t)
	base, err := Assemble(req.Snapshot, req.Loadout)
	if err != nil {
		t.Fatal(err)
	}
	var slot model.Slot
	for _, e := range base.Equipped {
		slot = e.SlotKey
		break
	}
	if slot == "" {
		t.Skip("the fixture equips nothing")
	}
	swapped := withPiece(base, slot, &model.Artifact{ID: -99, SlotKey: slot}, model.StatBlock{})
	var n int
	for _, e := range swapped.Equipped {
		if e.SlotKey == slot {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d pieces in %s after a swap, want 1", n, slot)
	}
	if len(swapped.Equipped) != len(base.Equipped) {
		t.Fatalf("swap changed the number of equipped pieces: %d then %d",
			len(base.Equipped), len(swapped.Equipped))
	}
}

// withPiece must not write through to the caller's state — every candidate is
// scored against the same base, and a shared slice would have each measuring
// the leftovers of the last.
func TestWithPieceDoesNotDisturbTheBase(t *testing.T) {
	req := rankingRequest(t)
	base, err := Assemble(req.Snapshot, req.Loadout)
	if err != nil {
		t.Fatal(err)
	}
	before := len(base.Equipped)
	ids := make(map[int64]bool, before)
	for _, e := range base.Equipped {
		ids[e.ID] = true
	}
	withPiece(base, model.Circlet, &model.Artifact{ID: -99, SlotKey: model.Circlet}, model.StatBlock{})
	withPiece(base, model.Circlet, nil, nil)
	if len(base.Equipped) != before {
		t.Fatalf("base lost or gained pieces: %d then %d", before, len(base.Equipped))
	}
	for _, e := range base.Equipped {
		if !ids[e.ID] {
			t.Fatalf("a candidate leaked into the base state: %d", e.ID)
		}
	}
	if _, ok := base.ArtifactStats[-99]; ok {
		t.Fatal("a candidate's stats leaked into the base state")
	}
}

// The piece the character has on must be findable, and it is the anchor Gain
// is measured from, so it must read as no change against itself.
func TestTheWornPieceIsMarkedAndGainsNothing(t *testing.T) {
	got, err := RankArtifacts(context.Background(), rankingRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	var worn int
	for _, s := range got.Slots {
		for _, p := range s.Pieces {
			if !p.Worn {
				continue
			}
			worn++
			if p.Gain > 0.0001 || p.Gain < -0.0001 {
				t.Errorf("the worn %s reports a gain of %.4f against itself", p.Slot, p.Gain)
			}
		}
	}
	if worn == 0 {
		t.Error("no piece was marked as worn, so nothing anchors the comparison")
	}
}

// A number with no stated conditions is not comparable to anything, and this
// one hides two large exclusions behind a tidy 0-100.
func TestTheRankingStatesItsAnchors(t *testing.T) {
	got, err := RankArtifacts(context.Background(), rankingRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.ToLower(strings.Join(got.Caveats, " "))
	for _, want := range []string{"empty", "hundred", "normal attacks"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the caveats never mention %q: %v", want, got.Caveats)
		}
	}
}

// Every slot that gets scored says which main stat it was scored against, or
// a low score looks like a verdict rather than a mismatch somebody can fix.
func TestEverySlotNamesTheMainStatItWants(t *testing.T) {
	got, err := RankArtifacts(context.Background(), rankingRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got.Slots {
		if s.Ideal == "" {
			t.Errorf("%s does not say what main stat it wants", s.Slot)
		}
	}
}

func TestRankArtifactsRefusesWithoutASnapshot(t *testing.T) {
	if _, err := RankArtifacts(context.Background(), ArtifactRankingRequest{}); err == nil {
		t.Fatal("want an error with no snapshot")
	}
}
