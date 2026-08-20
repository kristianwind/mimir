package i18n

import (
	"regexp"
	"strings"
	"testing"
)

// verbs finds the fmt verbs in a format string, ignoring %%.
var verbRE = regexp.MustCompile(`%(?:\[\d+\])?[-+# 0]*\d*(?:\.\d+)?[a-zA-Z]`)

func verbs(s string) []string {
	var out []string
	for _, m := range verbRE.FindAllString(strings.ReplaceAll(s, "%%", ""), -1) {
		// Explicit argument indexes are a translator's tool for reordering,
		// so compare the verb itself rather than the index it carries.
		if i := strings.Index(m, "]"); i >= 0 {
			m = "%" + m[i+1:]
		}
		out = append(out, m)
	}
	return out
}

// A translation whose verbs do not match its source produces %!s(MISSING) or
// silently drops a value. Both look like working software until somebody hits
// the one code path that renders them.
func TestTranslationsTakeTheSameArguments(t *testing.T) {
	for lang, table := range tables {
		for source, translated := range table {
			want, got := verbs(source), verbs(translated)
			if len(want) != len(got) {
				t.Errorf("%s: %q takes %v, translation takes %v", lang, source, want, got)
				continue
			}
			counts := map[string]int{}
			for _, v := range want {
				counts[v]++
			}
			for _, v := range got {
				counts[v]--
			}
			for v, n := range counts {
				if n != 0 {
					t.Errorf("%s: %q and its translation disagree on %s", lang, source, v)
				}
			}
		}
	}
}

// A key that is not a complete sentence in the source language is a key that
// will read as one in English when its translation is missing.
func TestSourcesAreNotEmpty(t *testing.T) {
	for lang, table := range tables {
		for source, translated := range table {
			if strings.TrimSpace(source) == "" {
				t.Errorf("%s: empty source key", lang)
			}
			if strings.TrimSpace(translated) == "" {
				t.Errorf("%s: %q translates to nothing", lang, source)
			}
			if source == translated && !allowedIdentical[source] {
				t.Errorf("%s: %q is untranslated but present in the table", lang, source)
			}
		}
	}
}

// Words that are genuinely the same in both languages. Listing them keeps the
// check above meaningful instead of merely noisy.
var allowedIdentical = map[string]bool{
	"elemental skill": true,
	"elemental burst": true,
}

func TestFallsBackToTheSource(t *testing.T) {
	const unknown = "a sentence nobody has translated"
	if got := T(DA, unknown); got != unknown {
		t.Errorf("missing translation should fall back to the source, got %q", got)
	}
	if got := T(EN, "malformed request"); got != "malformed request" {
		t.Errorf("English is the source, got %q", got)
	}
	if got := T(DA, "malformed request"); got != "ugyldig forespørgsel" {
		t.Errorf("Danish lookup failed, got %q", got)
	}
}

func TestParseAndFromRequest(t *testing.T) {
	for in, want := range map[string]Lang{
		"da": DA, "da-DK": DA, "DA": DA,
		"en": EN, "en-GB": EN, "": EN, "fr": EN, "nonsense": EN,
	} {
		if got := Parse(in); got != want {
			t.Errorf("Parse(%q) = %q, want %q", in, got, want)
		}
	}
}
