package advisor

import (
	"context"
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/model"
)

// twoGoals builds two characters competing for the same inventory: the
// classic real-account situation where each one's plan is right in isolation
// and the pair of them is a loop.
func twoGoals(t *testing.T) []Request {
	t.Helper()

	base := planRequest(t)
	snap := base.Snapshot
	c := snap.Characters["Tester"]
	rival := c
	rival.Key = "Rival"
	rival.Name = "Rival"
	snap.Characters["Rival"] = rival

	inv := base.Inventory
	// Everything unequipped belongs to Rival, so improving Tester means
	// taking it off them.
	for i := range inv {
		if inv[i].Location == "" {
			inv[i].Location = "Rival"
		}
	}

	var rivalEquipped []model.Artifact
	for _, a := range inv {
		if a.Location == "Rival" && len(rivalEquipped) < len(model.Slots) {
			taken := false
			for _, have := range rivalEquipped {
				if have.SlotKey == a.SlotKey {
					taken = true
				}
			}
			if !taken {
				rivalEquipped = append(rivalEquipped, a)
			}
		}
	}

	high := base
	high.Inventory = inv
	high.Goal.Priority = 10

	low := base
	low.Inventory = inv
	low.Goal.CharacterKey = "Rival"
	low.Goal.Priority = 1
	low.Loadout.Character.Key = "Rival"
	low.Loadout.Artifacts = rivalEquipped

	return []Request{low, high} // deliberately out of priority order
}

func TestAccountPlanRunsHighestPriorityFirst(t *testing.T) {
	plan, err := BuildAccountPlan(context.Background(), twoGoals(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plans) != 2 {
		t.Fatalf("got %d plans, want 2", len(plan.Plans))
	}
	if plan.Plans[0].Goal != "Tester" {
		t.Errorf("first plan is for %q; the priority 10 goal must be planned first", plan.Plans[0].Goal)
	}
}

func TestAccountPlanBlocksTheLowerPriorityClaim(t *testing.T) {
	plan, err := BuildAccountPlan(context.Background(), twoGoals(t))
	if err != nil {
		t.Fatal(err)
	}

	var high, low *AccountAction
	for i := range plan.Ranked {
		a := &plan.Ranked[i]
		if a.Kind != KindReequip {
			continue
		}
		if a.Goal == "Tester" && high == nil {
			high = a
		}
		if a.Goal == "Rival" && low == nil {
			low = a
		}
	}
	if high == nil {
		t.Fatal("the high-priority goal got no re-equip")
	}
	if high.BlockedBy != "" {
		t.Errorf("the highest-priority goal was blocked: %q", high.BlockedBy)
	}
	if low != nil && low.BlockedBy == "" {
		t.Error("the lower-priority goal was offered gear the higher one is using, unblocked")
	}
}

func TestAccountPlanReportsConflicts(t *testing.T) {
	plan, err := BuildAccountPlan(context.Background(), twoGoals(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) == 0 {
		t.Fatal("two goals fighting over one inventory produced no conflicts")
	}
	for _, c := range plan.Conflicts {
		if c.Item == c.Wants {
			t.Errorf("conflict names the character as the contested item: %+v", c)
		}
		if c.Item == "" || c.Holds == "" || c.Resolution == "" {
			t.Errorf("incomplete conflict: %+v", c)
		}
	}
}

func TestAccountPlanRanksFreeAndUnblockedFirst(t *testing.T) {
	plan, err := BuildAccountPlan(context.Background(), twoGoals(t))
	if err != nil {
		t.Fatal(err)
	}
	seenBlocked := false
	for _, a := range plan.Ranked {
		if a.BlockedBy != "" {
			seenBlocked = true
			continue
		}
		if seenBlocked {
			t.Errorf("an actionable item (%s) sorts below a blocked one", a.Headline)
			break
		}
	}
}

func TestAccountPlanSurvivesOneBrokenGoal(t *testing.T) {
	reqs := twoGoals(t)
	reqs[0].Goal.Spec.Steps = nil // break the low-priority goal

	plan, err := BuildAccountPlan(context.Background(), reqs)
	if err != nil {
		t.Fatalf("one broken goal took the whole account plan down: %v", err)
	}
	if !skipMentions(plan, "could not be calculated") {
		t.Error("the broken goal failed silently")
	}
	if len(plan.Ranked) == 0 {
		t.Error("the working goal produced no actions")
	}
}

func skipMentions(plan AccountPlan, want string) bool {
	for _, p := range plan.Plans {
		for _, s := range p.Skipped {
			if strings.Contains(s, want) {
				return true
			}
		}
	}
	return false
}
