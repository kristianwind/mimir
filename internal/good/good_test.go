package good

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/model"
)

const sample = `{
  "format": "GOOD",
  "version": 2,
  "source": "Inventory Kamera",
  "characters": [
    {"key":"Nahida","level":90,"constellation":2,"ascension":6,
     "talent":{"auto":1,"skill":9,"burst":8}}
  ],
  "weapons": [
    {"key":"AThousandFloatingDreams","level":90,"ascension":6,"refinement":1,
     "location":"Nahida","lock":true}
  ],
  "artifacts": [
    {"setKey":"DeepwoodMemories","slotKey":"circlet","level":20,"rarity":5,
     "mainStatKey":"critRate_","location":"Nahida","lock":true,
     "substats":[
       {"key":"atk_","value":9.9},
       {"key":"eleMas","value":40},
       {"key":"critDMG_","value":21.8},
       {"key":"","value":0}
     ]}
  ]
}`

func TestParseRejectsWrongEnvelope(t *testing.T) {
	if _, err := Parse(strings.NewReader(`{"format":"BAD","version":2}`)); err == nil {
		t.Error("accepted a non-GOOD file")
	}
	// Version 1 predates the current stat keys; importing it would produce an
	// inventory with silently wrong stat names.
	if _, err := Parse(strings.NewReader(`{"format":"GOOD","version":1}`)); err == nil {
		t.Error("accepted a version 1 file")
	}
	if _, err := Parse(strings.NewReader(`not json`)); err == nil {
		t.Error("accepted malformed JSON")
	}
}

func TestPercentDetection(t *testing.T) {
	// The trailing underscore is the whole rule, and it is the single most
	// likely place for a silent factor-of-100 error.
	if !IsPercent(model.ATKPercent) || !IsPercent(model.CritRate) || !IsPercent(model.EnergyRecharge) {
		t.Error("a percent stat was treated as flat")
	}
	if IsPercent(model.ATK) || IsPercent(model.HP) || IsPercent(model.ElementalMastery) {
		t.Error("a flat stat was treated as a percentage")
	}
}

func TestNormalizeRoundTrip(t *testing.T) {
	if got := Normalize(model.ATKPercent, 9.9); got != 0.099 {
		t.Errorf("Normalize(atk_, 9.9) = %v, want 0.099", got)
	}
	if got := Normalize(model.ElementalMastery, 40); got != 40 {
		t.Errorf("Normalize(eleMas, 40) = %v, want 40", got)
	}
	if got := Denormalize(model.CritDMG, 0.218); got < 21.79 || got > 21.81 {
		t.Errorf("Denormalize(critDMG_, 0.218) = %v, want ~21.8", got)
	}
}

func TestImport(t *testing.T) {
	f, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if f.Source != "Inventory Kamera" {
		t.Errorf("source = %q", f.Source)
	}

	chars, weapons, arts := f.Import(7)

	if len(chars) != 1 || chars[0].AccountID != 7 {
		t.Fatalf("characters = %+v", chars)
	}
	if chars[0].TalentSkill != 9 || chars[0].Constellation != 2 {
		t.Errorf("character = %+v", chars[0])
	}

	if len(weapons) != 1 || weapons[0].Location != "Nahida" || weapons[0].Source != "good" {
		t.Errorf("weapons = %+v", weapons)
	}

	if len(arts) != 1 {
		t.Fatalf("artifacts = %+v", arts)
	}
	a := arts[0]
	if a.SlotKey != model.Circlet || a.MainStat != model.CritRate || a.Rarity != 5 {
		t.Errorf("artifact = %+v", a)
	}
	// The padded empty substat scanners emit must be dropped.
	if len(a.Substats) != 3 {
		t.Fatalf("got %d substats, want 3: %+v", len(a.Substats), a.Substats)
	}

	byKey := map[model.Stat]float64{}
	for _, s := range a.Substats {
		byKey[s.Key] = s.Value
	}
	if byKey[model.ATKPercent] != 0.099 {
		t.Errorf("atk_ = %v, want 0.099", byKey[model.ATKPercent])
	}
	if byKey[model.ElementalMastery] != 40 {
		t.Errorf("eleMas = %v, want 40 — flat stats must not be divided", byKey[model.ElementalMastery])
	}

	// CV = 2·CR + CD, in display units.
	if cv := a.CritValue(); cv < 0.217 || cv > 0.219 {
		t.Errorf("crit value = %v, want ~0.218 (no crit rate substat on this piece)", cv)
	}
}

func TestImportHandlesEmptySections(t *testing.T) {
	f, err := Parse(strings.NewReader(`{"format":"GOOD","version":2}`))
	if err != nil {
		t.Fatal(err)
	}
	chars, weapons, arts := f.Import(1)
	if len(chars) != 0 || len(weapons) != 0 || len(arts) != 0 {
		t.Error("an empty file produced records")
	}
}

// A current Genshin Optimizer export says version 3, and carries fields that
// did not exist in 2. None of them change what Mimir reads, so the same
// inventory has to arrive whichever number is on the envelope.
func TestVersionThreeImportsLikeTwo(t *testing.T) {
	const body = `{
	  "format": "GOOD",
	  "source": "Genshin Optimizer",
	  "version": %d,
	  "characters": [
	    {"key": "Nahida", "level": 90, "constellation": 1, "ascension": 6,
	     "talent": {"auto": 9, "skill": 10, "burst": 9}}
	  ],
	  "artifacts": [
	    {"setKey": "DeepwoodMemories", "slotKey": "sands", "level": 20, "rarity": 5,
	     "mainStatKey": "atk_", "location": "Nahida", "lock": true,
	     "substats": [
	       {"key": "critRate_", "value": 7.8, "initialValue": 3.9},
	       {"key": "eleMas", "value": 40}
	     ],
	     "totalRolls": 9, "astralMark": false, "elixirCrafted": true,
	     "unactivatedSubstats": [{"key": "critDMG_", "value": 0}]}
	  ],
	  "weapons": [
	    {"key": "SacrificialFragments", "level": 90, "ascension": 6,
	     "refinement": 1, "location": "Nahida", "lock": true}
	  ]
	}`

	parse := func(version int) *File {
		t.Helper()
		f, err := Parse(strings.NewReader(fmt.Sprintf(body, version)))
		if err != nil {
			t.Fatalf("version %d: %v", version, err)
		}
		return f
	}

	two, three := parse(2), parse(3)
	c2, w2, a2 := two.Import(1)
	c3, w3, a3 := three.Import(1)

	if !reflect.DeepEqual(c2, c3) {
		t.Errorf("characters differ between versions:\n 2: %+v\n 3: %+v", c2, c3)
	}
	if !reflect.DeepEqual(w2, w3) {
		t.Errorf("weapons differ between versions:\n 2: %+v\n 3: %+v", w2, w3)
	}
	if !reflect.DeepEqual(a2, a3) {
		t.Errorf("artifacts differ between versions:\n 2: %+v\n 3: %+v", a2, a3)
	}

	// And the fields version 3 added are not mistaken for substats.
	if got := len(a3[0].Substats); got != 2 {
		t.Errorf("artifact has %d substats, want the 2 that are on it — "+
			"unactivatedSubstats are rolls it has not got yet", got)
	}
}

// The old format really is different, and the newest one is unknown. Both are
// refused, and the two messages say different things because the answers are
// different: one is "re-export", the other is "Mimir needs updating".
func TestVersionsOutsideTheCheckedRangeAreRefused(t *testing.T) {
	envelope := `{"format":"GOOD","source":"t","version":%d}`

	_, err := Parse(strings.NewReader(fmt.Sprintf(envelope, 1)))
	if !errors.Is(err, ErrTooOld) {
		t.Errorf("version 1: %v, want ErrTooOld", err)
	}

	_, err = Parse(strings.NewReader(fmt.Sprintf(envelope, maxGOODVersion+1)))
	if !errors.Is(err, ErrTooNew) {
		t.Errorf("a future version: %v, want ErrTooNew", err)
	}
	// The two must stay distinguishable, or the handler cannot tell somebody
	// whether to re-export or to update.
	if errors.Is(err, ErrTooOld) {
		t.Error("a future version also matched ErrTooOld")
	}
}
