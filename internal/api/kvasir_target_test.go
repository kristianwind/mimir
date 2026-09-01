package api

import (
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/kvasir"
)

// The regression this exists for.
//
// Asked "which artifact substats should Sandrone have?", Kvasir answered that
// it could not run the damage engine because the character had no goal. That
// was an honest report of the tools it had and a refusal of a question Mimir
// answers on its own Target page without any goal at all — the tool simply was
// not wired in.
func TestKvasirCanReachTheTarget(t *testing.T) {
	var names []string
	for _, tl := range kvasir.Tools() {
		names = append(names, tl.Function.Name)
	}
	var found bool
	for _, n := range names {
		if n == "target" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Kvasir cannot ask what a character should aim for; tools are %v", names)
	}
}

// The description is what decides whether the model reaches for it, so the two
// facts that route the question correctly have to be in there: that it needs no
// goal, and that it answers "what should X have".
func TestTargetToolSaysItNeedsNoGoal(t *testing.T) {
	for _, tl := range kvasir.Tools() {
		if tl.Function.Name != "target" {
			continue
		}
		d := strings.ToLower(tl.Function.Description)
		for _, want := range []string{"no goal", "substat", "main stat"} {
			if !strings.Contains(d, want) {
				t.Errorf("the description never mentions %q, so the model has no reason to pick it: %s",
					want, tl.Function.Description)
			}
		}
		return
	}
	t.Fatal("no target tool")
}
