// Package mine builds a gamedata snapshot from the public datamines.
//
// It is a separate binary from the server (cmd/mimir-mine) for one reason:
// this code talks to third-party repositories whose shape is not a contract.
// When an upstream mirror desyncs — and one of them currently does, see
// docs/GAMEDATA.md — the miner must fail loudly, on its own, without taking a
// running server down with it.
package mine

import (
	"strings"
	"unicode"
)

// GOODKey converts an English display name into the key the GOOD format uses:
// every non-alphanumeric character dropped, every word capitalised, joined.
//
//	"Crimson Witch of Flames"   -> "CrimsonWitchOfFlames"
//	"Gladiator's Finale"        -> "GladiatorsFinale"
//	"Amos' Bow"                 -> "AmosBow"
//	"A Thousand Floating Dreams" -> "AThousandFloatingDreams"
//
// Matching this exactly is not cosmetic. It is what lets a .good file from
// Inventory Kamera and a showcase from Enka describe the same artifact set,
// and what lets somebody migrate off Genshin Optimizer without remapping
// anything.
func GOODKey(name string) string {
	var b strings.Builder
	newWord := true
	for _, r := range normalise(name) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if newWord {
				b.WriteRune(unicode.ToUpper(r))
				newWord = false
			} else {
				b.WriteRune(r)
			}
		case r == '\'' || r == '\u2019':
			// An apostrophe sits *inside* a word: "Gladiator's Finale" is
			// GladiatorsFinale, not GladiatorSFinale. Drop it and carry on.
		default:
			// Every other separator — space, hyphen, colon, comma — ends a
			// word and is itself dropped.
			newWord = true
		}
	}
	return b.String()
}

// transliterations covers the accented characters that appear in Genshin's
// English names. They are folded rather than dropped, because dropping them
// would turn "Kirara" and "Kīrara" into different keys.
var transliterations = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ä", "a", "ā", "a", "å", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e", "ē", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i", "ī", "i",
	"ó", "o", "ò", "o", "ô", "o", "ö", "o", "ō", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u", "ū", "u",
	"ñ", "n", "ç", "c",
	"Á", "A", "À", "A", "Â", "A", "Ä", "A", "Ā", "A", "Å", "A",
	"É", "E", "È", "E", "Ê", "E", "Ë", "E", "Ē", "E",
	"Í", "I", "Ì", "I", "Î", "I", "Ï", "I", "Ī", "I",
	"Ó", "O", "Ò", "O", "Ô", "O", "Ö", "O", "Ō", "O",
	"Ú", "U", "Ù", "U", "Û", "U", "Ü", "U", "Ū", "U",
	"Ñ", "N", "Ç", "C",
)

func normalise(s string) string {
	// Rich text markup leaks into a few datamined names.
	if i := strings.Index(s, "<"); i >= 0 {
		s = stripTags(s)
	}
	return transliterations.Replace(s)
}

func stripTags(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}
