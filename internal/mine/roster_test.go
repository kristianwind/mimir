package mine

import (
	"testing"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// The game ships trial copies of characters, and they arrive through the same
// door the real ones do. A copy reuses the original's portrait and is added
// later, so the lowest id per portrait is the character.
func TestTrialCopiesAreNotCharacters(t *testing.T) {
	rows := []avatarRow{
		{ID: 10000046, IconName: "UI_AvatarIcon_Hutao"},
		{ID: 10000902, IconName: "UI_AvatarIcon_Hutao"}, // the trial copy
		{ID: 11000046, IconName: "UI_AvatarIcon_Qin"},   // a test row
		{ID: 10000003, IconName: "UI_AvatarIcon_Qin"},
		{ID: 10000130, IconName: "UI_AvatarIcon_Linnea"},
		{ID: 0, IconName: ""}, // no portrait, no identity
	}

	kept := map[int]bool{}
	for _, av := range playableAvatars(rows) {
		kept[av.ID] = true
	}
	for _, id := range []int{10000046, 10000003, 10000130} {
		if !kept[id] {
			t.Errorf("dropped the real character %d", id)
		}
	}
	for _, id := range []int{10000902, 11000046} {
		if kept[id] {
			t.Errorf("kept %d, which is a copy of a character already in the roster", id)
		}
	}
}

// Enka's store is asked first, because it is the only source that orders the
// three talents.
func TestEnkaIsThePreferredNameSource(t *testing.T) {
	av := avatarRow{ID: 10000046, IconName: "UI_AvatarIcon_Hutao"}
	enka := map[string]enkaCharacter{
		"10000046": {
			Element: "Fire", SideIconName: "UI_AvatarIcon_Side_Hutao",
			NameTextMapHash: 42, QualityType: "QUALITY_ORANGE",
			SkillOrder: []int{1, 2, 3},
		},
	}
	names := map[string]string{"42": "Hu Tao"}
	gdb := map[int]gdbCharacter{10000046: {ID: 10000046, Name: "Wrong Name", ElementText: "Geo"}}

	got, ok := resolveIdentity(av, enka, names, gdb)
	if !ok {
		t.Fatal("no identity resolved")
	}
	if got.name != "Hu Tao" || got.source != "enka" {
		t.Errorf("identity = %+v", got)
	}
	if len(got.skillOrder) != 3 {
		t.Error("the skill order was lost, which is the reason Enka is asked first")
	}
}

// And the whole point of the second source: a character Enka has not caught
// up with is still a character. Linnea was released in April and was still
// missing from the roster in August.
func TestASecondSourceRescuesACharacterEnkaHasNotGot(t *testing.T) {
	av := avatarRow{ID: 10000130, IconName: "UI_AvatarIcon_Linnea"}
	gdb := map[int]gdbCharacter{
		10000130: {ID: 10000130, Name: "Linnea", ElementText: "Geo", Rarity: 5},
	}

	got, ok := resolveIdentity(av, map[string]enkaCharacter{}, nil, gdb)
	if !ok {
		t.Fatal("a character only the second source knows was dropped")
	}
	if got.name != "Linnea" || got.source != "genshin-db" {
		t.Errorf("identity = %+v", got)
	}
	if got.element != model.Geo {
		t.Errorf("element = %q; the two sources spell it differently and both must be read", got.element)
	}
	if got.rarity != 5 {
		t.Errorf("rarity = %d", got.rarity)
	}
	// The portrait comes from the datamine, which names it in readable text
	// whoever else has caught up.
	if got.art != "Linnea" {
		t.Errorf("art = %q", got.art)
	}
}

func TestAnAvatarNobodyCanNameIsNotACharacter(t *testing.T) {
	av := avatarRow{ID: 10000999, IconName: "UI_AvatarIcon_PlayerGirl"}
	if _, ok := resolveIdentity(av, map[string]enkaCharacter{}, nil, map[int]gdbCharacter{}); ok {
		t.Error("an avatar no name source knows was accepted as a character")
	}
}

// The talent bills need the proud-skill groups, and they come from the
// datamine so that a character the name sources are behind on still gets one.
func TestProudSkillGroupsComeFromTheSkillIds(t *testing.T) {
	got := proudSkillGroups(
		gamedata.SkillIDs{Auto: 11301, Skill: 11302, Burst: 11305},
		map[int]int{11301: 1131, 11302: 1132, 11305: 1135},
	)
	if got.Auto != 1131 || got.Skill != 1132 || got.Burst != 1135 {
		t.Errorf("groups = %+v", got)
	}
}
