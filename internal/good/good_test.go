package good

import (
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
