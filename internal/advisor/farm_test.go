package advisor

import (
	"context"
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// Farming advice names a place to go. If two places that drop entirely
// different sets are worth the same, then the answer is not about the place
// and the list of domains is decoration on a single number.
func TestTwoDomainsWithDifferentSetsAreNotWorthTheSame(t *testing.T) {
	req := planRequest(t)
	snap := req.Snapshot

	// One set that does nothing for this goal, and one that doubles the
	// damage bonus. No sampling noise can make these equal.
	snap.ArtifactSets["Inert"] = gamedata.ArtifactSet{Key: "Inert", Name: "Inert"}
	snap.ArtifactSets["Potent"] = gamedata.ArtifactSet{
		Key: "Potent", Name: "Potent",
		TwoPiece: model.StatBlock{model.PyroDMG: 1.5},
	}
	snap.Domains["inertdomain"] = gamedata.Domain{
		Key: "inertdomain", Name: "Inert Domain", Kind: "artifact",
		Sets: []string{"Inert"}, ResinCost: 20,
	}
	snap.Domains["potentdomain"] = gamedata.Domain{
		Key: "potentdomain", Name: "Potent Domain", Kind: "artifact",
		Sets: []string{"Potent"}, ResinCost: 20,
	}

	sim := &FarmSim{Snapshot: snap, Trials: 60, Seed: 5}
	eval := EngineEvaluator{Snapshot: snap}
	state, err := Assemble(snap, req.Loadout)
	if err != nil {
		t.Fatal(err)
	}

	inert, err := sim.EstimatePieces(context.Background(), req.Goal, state,
		snap.Domains["inertdomain"], 40, eval)
	if err != nil {
		t.Fatal(err)
	}
	potent, err := sim.EstimatePieces(context.Background(), req.Goal, state,
		snap.Domains["potentdomain"], 40, eval)
	if err != nil {
		t.Fatal(err)
	}

	if inert.MeanGain == potent.MeanGain {
		t.Fatalf("both domains are worth %v; the set a domain drops is not "+
			"reaching the simulation, so every domain scores identically",
			inert.MeanGain)
	}
	if potent.MeanGain <= inert.MeanGain {
		t.Errorf("the domain dropping a +150%% pyro set is worth %v against "+
			"the inert one's %v", potent.MeanGain, inert.MeanGain)
	}
}

// Six domains printed with the same number is six times the confidence and
// none of the information. When Mimir cannot tell them apart it has to say
// so once, not repeat itself.
func TestDomainsThatScoreIdenticallyAreOneRow(t *testing.T) {
	snap := planSnapshot()
	tied := []Action{
		{Kind: KindFarm, Subject: "a", Headline: "Farm A (100 5★ pieces)", GainPct: 0.5,
			Detail: map[string]any{"domain": "Domain A", "sets": []string{"A"}}},
		{Kind: KindFarm, Subject: "b", Headline: "Farm B (100 5★ pieces)", GainPct: 0.5,
			Detail: map[string]any{"domain": "Domain B", "sets": []string{"B"}}},
		{Kind: KindFarm, Subject: "c", Headline: "Farm C (100 5★ pieces)", GainPct: 0.25,
			Detail: map[string]any{"domain": "Domain C", "sets": []string{"A"}}},
	}

	got := collapseTiedFarms(tied, snap)
	if len(got) != 2 {
		t.Fatalf("got %d actions, want the two tied folded into one and the third left alone", len(got))
	}
	if !strings.Contains(got[0].Note, "Domain A") || !strings.Contains(got[0].Note, "Domain B") {
		t.Errorf("the merged row does not name what tied: %q", got[0].Note)
	}
	// Neither A nor B has a four-piece with numbers in the test snapshot, so
	// the reason for the tie has to travel with it.
	if !strings.Contains(got[0].Note, "four-piece") {
		t.Errorf("the merged row does not say why they tied: %q", got[0].Note)
	}
	if got[1].Headline != "Farm C (100 5★ pieces)" {
		t.Errorf("the untied action was rewritten: %q", got[1].Headline)
	}
}

// The reason domains tie: almost no artifact set has a four-piece bonus the
// engine can score. A build sheet that cannot tell a modelled set from an
// unmodelled one will present substat noise as a set recommendation.
func TestAnUnmodelledFourPieceIsNotMistakenForAModelledOne(t *testing.T) {
	snap := planSnapshot()
	snap.ArtifactSets["Flat"] = gamedata.ArtifactSet{
		Key: "Flat", FourPiece: model.StatBlock{model.ATKPercent: 0.35},
	}
	snap.ArtifactSets["Ruled"] = gamedata.ArtifactSet{Key: "Ruled"}
	snap.ArtifactSets["Prose"] = gamedata.ArtifactSet{
		Key: "Prose", FourPieceText: "After using an Elemental Burst, something happens.",
	}
	snap.Effects = append(snap.Effects, gamedata.EffectRule{
		Key: "Ruled", Kind: gamedata.EffectKindArtifactSet, Trigger: "4pc",
	})
	// A two-piece rule is not a four-piece, however tempting the key match.
	snap.Effects = append(snap.Effects, gamedata.EffectRule{
		Key: "Prose", Kind: gamedata.EffectKindArtifactSet, Trigger: "2pc",
	})

	for key, want := range map[string]bool{"Flat": true, "Ruled": true, "Prose": false} {
		if got := snap.FourPieceModelled(key); got != want {
			t.Errorf("FourPieceModelled(%q) = %v, want %v", key, got, want)
		}
	}
}
