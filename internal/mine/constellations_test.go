package mine

import (
	"testing"

	"github.com/kristianwind/mimir/internal/gamedata"
)

func character(talents map[string]string, constellations ...string) gamedata.Character {
	c := gamedata.Character{Talents: map[string]gamedata.Talent{}}
	for slot, name := range talents {
		c.Talents[slot] = gamedata.Talent{Name: name}
	}
	for _, d := range constellations {
		c.Constellations = append(c.Constellations, gamedata.Described{Description: d})
	}
	return c
}

func TestTalentBoostsFindTheRightSlot(t *testing.T) {
	// Xiangling's C3 raises her burst; Diluc's raises his skill. Assuming a
	// fixed mapping would be wrong for one of them whichever way you guess.
	xiangling := character(
		map[string]string{"auto": "Dough-Fu", "skill": "Guoba Attack", "burst": "Pyronado"},
		"", "",
		"Increases the Level of Pyronado by 3.\nMaximum upgrade level is 15.",
		"",
		"Increases the Level of Guoba Attack by 3.\nMaximum upgrade level is 15.",
		"",
	)
	got := talentBoosts(xiangling)
	if got[3].Slot != "burst" || got[3].Levels != 3 || got[3].MaxLevel != 15 {
		t.Errorf("C3 = %+v, want burst +3 capped at 15", got[3])
	}
	if got[5].Slot != "skill" {
		t.Errorf("C5 = %+v, want skill", got[5])
	}

	diluc := character(
		map[string]string{"auto": "Tempered Sword", "skill": "Searing Onslaught", "burst": "Dawn"},
		"", "",
		"Increases the Level of Searing Onslaught by 3.\nMaximum upgrade level is 15.",
		"",
		"Increases the Level of Dawn by 3.\nMaximum upgrade level is 15.",
		"",
	)
	if s := talentBoosts(diluc)[3].Slot; s != "skill" {
		t.Errorf("Diluc C3 boosts %q, want skill", s)
	}
}

func TestTalentBoostsHandleEmphasisMarkup(t *testing.T) {
	// Newer characters wrap the ability name in bold.
	aino := character(
		map[string]string{"skill": "Musecatcher", "burst": "Precision Hydronic Cooler"},
		"", "",
		"Increases the Level of **Precision Hydronic Cooler** by 3.\nMaximum upgrade level is 15.",
		"",
		"Increases the Level of **Musecatcher** by 3.\nMaximum upgrade level is 15.",
		"",
	)
	got := talentBoosts(aino)
	if got[3].Slot != "burst" || got[5].Slot != "skill" {
		t.Errorf("markup broke the name match: %+v", got)
	}
}

func TestTalentBoostsIgnoreCharactersWithoutThem(t *testing.T) {
	// Aloy's constellations upgrade nothing, and inventing a bonus for her
	// would silently inflate her damage.
	aloy := character(
		map[string]string{"skill": "Frozen Wilds", "burst": "Prophecies of Dawn"},
		"Coil: Aloy gains a stack.", "", "", "", "", "",
	)
	if got := talentBoosts(aloy); got != nil {
		t.Errorf("invented a talent boost: %+v", got)
	}
}

func TestTalentBoostsIgnoreAnUnknownAbility(t *testing.T) {
	// A renamed ability upstream must drop the boost, not attach it to the
	// wrong talent.
	odd := character(
		map[string]string{"skill": "Icy Paws", "burst": "Signature Mix"},
		"", "", "Increases the Level of Some Other Thing by 3.", "", "", "",
	)
	if got := talentBoosts(odd); got != nil {
		t.Errorf("matched an ability that is not in the talent table: %+v", got)
	}
}

func TestTalentBoostsReadATalentTypeLabel(t *testing.T) {
	// Columbina's text labels the ability with its type before naming it.
	columbina := character(
		map[string]string{"skill": "Eternal Tides", "burst": "Moonlit Melancholy"},
		"", "",
		"Increases the Level of Elemental Skill **Eternal Tides** by 3. Maximum upgrade level is 15.",
		"",
		"Increases the Level of Elemental Burst **Moonlit Melancholy** by 3. Maximum upgrade level is 15.",
		"",
	)
	got := talentBoosts(columbina)
	if got[3].Slot != "skill" || got[5].Slot != "burst" {
		t.Errorf("type label not understood: %+v", got)
	}
}

func TestTalentBoostsFallBackToTheLabelWhenTheNameMoved(t *testing.T) {
	// If upstream renames the ability but keeps the label, the slot is still
	// unambiguous — better than dropping the three levels silently.
	renamed := character(
		map[string]string{"skill": "Something Else", "burst": "Another Thing"},
		"", "",
		"Increases the Level of Elemental Skill **Old Name** by 3.",
		"", "", "",
	)
	if got := talentBoosts(renamed); got[3].Slot != "skill" {
		t.Errorf("label fallback failed: %+v", got)
	}
}
