package gamedata

import "testing"

// Real labels from the mined tables. A talent row that is a hit and a row that
// changes a hit are the same shape — a percentage — so this is the only thing
// standing between "Raiden's burst does 721% of ATK" and "Raiden's burst does
// 30% of ATK", which is what her Eye of Stormy Judgment buff would compute as.
func TestIsDamageSeparatesHitsFromBuffs(t *testing.T) {
	hits := []string{
		"Skill DMG",
		"Musou no Hitotachi Base DMG",
		"1-Hit DMG",
		"Charged Attack DMG",
		"Tri-Karma Purification DMG · del 2",
		"Fiery Rain DMG Per Wave",
		"DMG Per Stack",
		"Phantasm Performance 1-Hit DMG (Shades)",
	}
	for _, label := range hits {
		e := TalentEntry{Label: label, Unit: "percent"}
		if !e.IsDamage() {
			t.Errorf("%q was not treated as damage", label)
		}
	}

	buffs := []string{
		"Elemental Burst DMG Bonus",
		"Pyro: DMG Bonus",
		"Normal Attack DMG Bonus",
		"DMG Reduction",
		"DMG Bonus on Hit Taken",
		"Bloom, Hyperbloom, and Burgeon DMG Increase",
		"Fanfare to DMG Increase Conversion Ratio",
		"0/1/2/3 Void Rift Absorption DMG Bonus · del 1",
	}
	for _, label := range buffs {
		e := TalentEntry{Label: label, Unit: "percent"}
		if e.IsDamage() {
			t.Errorf("%q was treated as a damage instance", label)
		}
	}
}

// Everything that is not a percentage is not a hit, whatever it is called.
func TestIsDamageIgnoresOtherUnits(t *testing.T) {
	for _, unit := range []string{"seconds", "flat", ""} {
		e := TalentEntry{Label: "Skill DMG", Unit: unit}
		if e.IsDamage() {
			t.Errorf("a %q row was treated as damage", unit)
		}
	}
}
