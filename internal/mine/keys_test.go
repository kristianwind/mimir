package mine

import "testing"

func TestGOODKey(t *testing.T) {
	cases := map[string]string{
		"Crimson Witch of Flames":    "CrimsonWitchOfFlames",
		"Gladiator's Finale":         "GladiatorsFinale",
		"Amos' Bow":                  "AmosBow",
		"A Thousand Floating Dreams": "AThousandFloatingDreams",
		"Kamisato Ayaka":             "KamisatoAyaka",
		"Raiden Shogun":              "RaidenShogun",
		"Hu Tao":                     "HuTao",
		"Song of Broken Pines":       "SongOfBrokenPines",
		"Deepwood Memories":          "DeepwoodMemories",
		"Marechaussee Hunter":        "MarechausseeHunter",
		"Wanderer's Troupe":          "WanderersTroupe",
		"Noblesse Oblige":            "NoblesseOblige",
		"Maiden Beloved":             "MaidenBeloved",
		"Prayers to Springtime":      "PrayersToSpringtime",
		"Favonius Warbow":            "FavoniusWarbow",
		"Prototype Archaic":          "PrototypeArchaic",
		"Sacrificial Sword":          "SacrificialSword",
		"Cool Steel":                 "CoolSteel",
		"Apprentice's Notes":         "ApprenticesNotes",
		"The Bell":                   "TheBell",
		"Mitternachts Waltz":         "MitternachtsWaltz",
		"Wine and Song":              "WineAndSong",
		"Thundering Pulse":           "ThunderingPulse",
	}
	for in, want := range cases {
		if got := GOODKey(in); got != want {
			t.Errorf("GOODKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGOODKeyFoldsAccents(t *testing.T) {
	// Dropping the accent instead of folding it would split one entity into
	// two keys the moment upstream changes its mind about the diacritic.
	if got := GOODKey("Kīrara"); got != "Kirara" {
		t.Errorf("GOODKey(Kīrara) = %q, want Kirara", got)
	}
	if got := GOODKey("Café Mécanique"); got != "CafeMecanique" {
		t.Errorf("got %q", got)
	}
}

func TestGOODKeyStripsMarkup(t *testing.T) {
	if got := GOODKey("<color=#FFD780FF>Ushi</color>"); got != "Ushi" {
		t.Errorf("GOODKey with markup = %q, want Ushi", got)
	}
}

func TestGOODKeyEdgeCases(t *testing.T) {
	if got := GOODKey(""); got != "" {
		t.Errorf("empty name gave %q", got)
	}
	if got := GOODKey("   "); got != "" {
		t.Errorf("blank name gave %q", got)
	}
	if got := GOODKey("4-Star"); got != "4Star" {
		t.Errorf("got %q, want 4Star", got)
	}
}
