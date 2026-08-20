package advisor

import (
	"context"
	"fmt"
	"sort"

	"github.com/kristianwind/mimir/internal/model"
)

// AccountPlan is every goal's plan on one account, with the contention
// between them made explicit.
//
// A per-goal plan optimises one character in isolation, which is right for
// answering "how do I improve Xiangling" and wrong for answering "what should
// I do tomorrow". On a real account the two collide: Mimir will happily tell
// Xiangling to take Raiden's Emblem set and Raiden to take Xiangling's, and
// both are true in isolation. Ranking across goals is where that gets
// resolved.
type AccountPlan struct {
	Plans []Plan `json:"plans"`
	// Ranked is every action from every goal in one list, which is the
	// question the player actually has.
	Ranked []AccountAction `json:"ranked"`
	// Conflicts names the gear two goals both want.
	Conflicts []Conflict `json:"conflicts"`
	// Caveats state the limits of the reconciliation, rather than leaving
	// the user to discover them.
	Caveats []string `json:"caveats,omitempty"`
}

// AccountAction is an action tagged with the goal it belongs to.
type AccountAction struct {
	Action
	Goal string `json:"goal"`
	// GoalPriority is carried through so the UI can explain why a smaller
	// gain on a higher-priority character can outrank a larger one.
	GoalPriority int `json:"goalPriority"`
}

// workingInventory returns one mutable copy of the shared inventory.
func workingInventory(reqs []Request) []model.Artifact {
	for _, r := range reqs {
		if len(r.Inventory) > 0 {
			return append([]model.Artifact(nil), r.Inventory...)
		}
	}
	return nil
}

// commitClaims records that a goal's winning re-equip has claimed its pieces,
// so later goals plan against an inventory where those pieces are spoken for.
//
// Only unblocked actions claim anything: a recommendation Mimir has already
// refused to make must not also reserve the gear.
func commitClaims(inventory []model.Artifact, plan Plan, owner string) {
	claimed := map[int64]bool{}
	for _, a := range plan.Actions {
		if a.Kind != KindReequip || a.BlockedBy != "" {
			continue
		}
		pieces, ok := a.Detail["pieces"].([]model.Artifact)
		if !ok {
			continue
		}
		for _, p := range pieces {
			claimed[p.ID] = true
		}
		break // only the winning arrangement is claimed, not every option
	}
	for i := range inventory {
		if claimed[inventory[i].ID] {
			inventory[i].Location = owner
		}
	}
}

// Conflict is one piece of gear that two goals both want.
type Conflict struct {
	// Item is the artifact set or weapon at stake.
	Item string `json:"item"`
	// Wants is the goal that would gain it.
	Wants string `json:"wants"`
	// Holds is the goal currently using it.
	Holds string `json:"holds"`
	// Resolution says which side Mimir ranked first, and why.
	Resolution string `json:"resolution"`
}

// BuildAccountPlan runs every goal and reconciles them.
//
// Goals are processed highest priority first, and an action that would strip
// gear off a higher-priority goal is demoted rather than silently offered.
// The alternative — presenting both sides of a tug-of-war as free wins — is
// how a tool ends up recommending a loop the player can never finish.
func BuildAccountPlan(ctx context.Context, reqs []Request) (AccountPlan, error) {
	ordered := append([]Request(nil), reqs...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Goal.Priority > ordered[j].Goal.Priority
	})

	priorities := map[string]int{}
	for _, r := range ordered {
		priorities[r.Goal.CharacterKey] = r.Goal.Priority
	}

	// One working copy of the inventory, shared across goals. As each goal
	// claims gear, the pieces change hands in this copy — which is what lets
	// the next goal see the claim and be blocked by it instead of being
	// offered the same flower a second time.
	inventory := workingInventory(ordered)

	var out AccountPlan
	out.Caveats = []string{
		"Hvert mål måles mod det udstyr karakteren har nu — ikke mod det, et " +
			"højere prioriteret mål lige har taget. Kør planen igen, når du " +
			"har flyttet tingene i spillet.",
	}

	for _, req := range ordered {
		req.Inventory = inventory
		plan, err := BuildPlan(ctx, req)
		if err != nil {
			// One broken goal must not take the whole account's plan with
			// it; the failure is reported against that goal alone.
			out.Plans = append(out.Plans, Plan{
				Goal:    req.Goal.CharacterKey,
				Skipped: []string{fmt.Sprintf("kunne ikke regnes ud: %v", err)},
			})
			continue
		}

		for i := range plan.Actions {
			action := &plan.Actions[i]
			for _, holder := range takenFromDetail(*action) {
				holderPriority, isGoal := priorities[holder]
				if !isGoal {
					continue
				}
				out.Conflicts = append(out.Conflicts, Conflict{
					Item:  contestedItem(*action),
					Wants: req.Goal.CharacterKey,
					Holds: holder,
					Resolution: fmt.Sprintf("%s har prioritet %d, %s har %d",
						holder, holderPriority, req.Goal.CharacterKey, req.Goal.Priority),
				})
				if holderPriority >= req.Goal.Priority {
					action.BlockedBy = fmt.Sprintf(
						"%s bruger det, og har mindst lige så høj prioritet", holder)
				}
			}
		}

		plan.Actions = Rank(plan.Actions)
		commitClaims(inventory, plan, req.Goal.CharacterKey)
		out.Plans = append(out.Plans, plan)
		for _, a := range plan.Actions {
			out.Ranked = append(out.Ranked, AccountAction{
				Action:       a,
				Goal:         req.Goal.CharacterKey,
				GoalPriority: req.Goal.Priority,
			})
		}
	}

	// One ranked list across goals: free first, blocked last, then per-resin
	// efficiency, and priority only as the tie-break it should be.
	sort.SliceStable(out.Ranked, func(i, j int) bool {
		a, b := out.Ranked[i], out.Ranked[j]
		if (a.BlockedBy == "") != (b.BlockedBy == "") {
			return a.BlockedBy == ""
		}
		if a.Free != b.Free {
			return a.Free
		}
		if a.Efficiency != b.Efficiency {
			return a.Efficiency > b.Efficiency
		}
		if a.GoalPriority != b.GoalPriority {
			return a.GoalPriority > b.GoalPriority
		}
		return a.GainPct > b.GainPct
	})

	return out, nil
}

// contestedItem names what is actually at stake. A re-equip's subject is the
// character being improved, not the gear being taken, so reporting it as the
// contested item would read as "Raiden wants Raiden from Xiangling".
func contestedItem(a Action) string {
	if a.Kind == KindReequip {
		if cfg, ok := a.Detail["config"].(string); ok && cfg != "" {
			return cfg
		}
	}
	return a.Subject
}

// takenFromDetail reads the contention list an action recorded.
func takenFromDetail(a Action) []string {
	if a.Detail == nil {
		return nil
	}
	switch v := a.Detail["takenFrom"].(type) {
	case []string:
		return v
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}
