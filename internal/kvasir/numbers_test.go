package kvasir

import (
	"reflect"
	"testing"
)

const facts = `# The resin plan
## The ranked plan
- 1. [RaidenShogun] Switch to 4pc EmblemOfSeveredFate · +34.53 % · free
- 2. [Xiangling] Elemental skill 9 → 10 · +1.09 % · 20 resin
## The goals
- RaidenShogun: baseline 70206 damage per rotation
`

func TestQuotedFiguresPass(t *testing.T) {
	s := Collect(facts)
	for _, answer := range []string{
		"Do the Emblem swap first: +34.53 % for nothing.",
		"Raiden sits at 70206 damage per rotation.",
		"The talent costs 20 resin for +1.09 %.",
	} {
		if bad := s.Unsourced(answer); len(bad) != 0 {
			t.Errorf("%q was rejected over %v", answer, bad)
		}
	}
}

// The whole point of the layer. A model that writes a plausible number nobody
// calculated is the failure mode every other rule in Mimir exists to prevent.
func TestInventedFiguresAreCaught(t *testing.T) {
	s := Collect(facts)
	got := s.Unsourced("Emblem gives about 34.53 %, and the crown adds another 12.7 % on top.")
	if !reflect.DeepEqual(got, []string{"12.7"}) {
		t.Fatalf("expected 12.7 to be caught, got %v", got)
	}
}

// A model trained on European text writes 34,53 whatever it was asked for. A
// check that fired on correctly quoted figures would be switched off within a
// week.
func TestAContinentalDecimalCommaIsTheSameNumber(t *testing.T) {
	s := Collect(facts)
	if bad := s.Unsourced("Swap to Emblem first: +34,53 % for nothing."); len(bad) != 0 {
		t.Fatalf("a decimal comma was rejected: %v", bad)
	}
}

func TestRoundingIsAllowedButPrecisionIsNot(t *testing.T) {
	s := Collect("- the gain is 34.53 %")

	if bad := s.Unsourced("about 35 %"); len(bad) != 0 {
		t.Errorf("rounding 34.53 to 35 was rejected: %v", bad)
	}
	if bad := s.Unsourced("about 34.5 %"); len(bad) != 0 {
		t.Errorf("rounding 34.53 to 34.5 was rejected: %v", bad)
	}
	// The other direction invents precision that was never calculated.
	if bad := s.Unsourced("exactly 34.5312 %"); len(bad) == 0 {
		t.Error("34.5312 was accepted against a fact sheet that says 34.53")
	}
}

// Counting is not calculating: a model has to be able to say "the top three"
// without the fact sheet containing a three.
func TestSmallCountsAreAllowed(t *testing.T) {
	s := Collect("- nothing numeric here")
	if bad := s.Unsourced("Do the first two things and ignore the other 3."); len(bad) != 0 {
		t.Fatalf("small counts were rejected: %v", bad)
	}
	if bad := s.Unsourced("It is worth 11 % more."); len(bad) == 0 {
		t.Fatal("11 was accepted; the exemption is meant to stop at ten")
	}
}

func TestToolResultsBecomeSourced(t *testing.T) {
	s := Collect(facts)
	if bad := s.Unsourced("Her crit rate is 71.4 %."); len(bad) == 0 {
		t.Fatal("a figure from nowhere was accepted")
	}
	s.Add("## Resolved stats\n- Crit Rate: 71.4 %\n")
	if bad := s.Unsourced("Her crit rate is 71.4 %."); len(bad) != 0 {
		t.Fatalf("a figure a tool returned was still rejected: %v", bad)
	}
}

func TestUnsourcedNamesEachFigureOnce(t *testing.T) {
	s := Collect(facts)
	got := s.Unsourced("55 % here, 55 % again, and 61 % over there")
	if !reflect.DeepEqual(got, []string{"55", "61"}) {
		t.Fatalf("got %v", got)
	}
}

func TestThousandsSeparatorsReadBothWays(t *testing.T) {
	s := Collect("- baseline 70206 damage")
	for _, written := range []string{"70,206", "70.206"} {
		if bad := s.Unsourced("a baseline of " + written + " damage"); len(bad) != 0 {
			t.Errorf("%s was rejected: %v", written, bad)
		}
	}
}
