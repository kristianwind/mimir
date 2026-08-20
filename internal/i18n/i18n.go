// Package i18n translates the prose the server sends to a person.
//
// The design mirrors the frontend's: the English sentence is both the source
// and the lookup key. A missing translation therefore degrades to a readable
// English sentence rather than to a bare identifier, and a string nobody has
// translated yet is still a string the user can act on.
//
// Only text a human reads goes through here. Wrapped errors that exist for the
// log, and messages that never leave the process, stay in English — translating
// those would make the logs harder to search without helping anyone.
package i18n

import (
	"fmt"
	"net/http"
	"strings"
)

type Lang string

const (
	EN Lang = "en"
	DA Lang = "da"
)

// Parse maps a language code onto a language the server has a table for.
// Anything unrecognised is English, which is the source.
func Parse(s string) Lang {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(s)), "da") {
		return DA
	}
	return EN
}

// FromRequest reads the language off Accept-Language.
//
// The header rather than the stored user preference, because errors have to be
// translatable on requests that have no session yet — the login and first-run
// screens — and because it costs no database round trip on the error path. The
// bundled frontend sets it from the language the user picked; a plain curl gets
// English.
func FromRequest(r *http.Request) Lang {
	if r == nil {
		return EN
	}
	// Only the first tag is considered. This is a two-language table, not a
	// content negotiator, and a full q-value parse would be more machinery
	// than the choice deserves.
	head, _, _ := strings.Cut(r.Header.Get("Accept-Language"), ",")
	return Parse(head)
}

// T renders an English source string in lang.
//
// Args are applied with fmt after translation, so a translation is free to
// reorder them with explicit indexes (%[2]s) where the target language needs a
// different word order.
func T(lang Lang, source string, args ...any) string {
	s := source
	if lang != EN {
		if table, ok := tables[lang]; ok {
			if t, found := table[source]; found {
				s = t
			}
		}
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

var tables = map[Lang]map[string]string{DA: da}
