package kvasir

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// The number check.
//
// This is the mechanism that makes an AI layer safe to put in front of a
// calculation engine. The effect loader already refuses a hand-written rule
// whose numbers are not in the game's own text; this is the same rule pointed
// at the model: every number Kvasir says has to appear in the fact sheet the
// engine gave it.
//
// It is not sentiment analysis and it does not judge reasoning. It answers one
// mechanical question — did this figure come from somewhere — and that is
// enough, because a wrong opinion is an opinion and a fabricated multiplier is
// a lie the player has no way to catch.

// numberToken finds anything that could be read as a number: digit groups
// optionally separated by dots or commas. Which of those is the decimal point
// depends on the language, and Kvasir writes Danish for Danish users, so the
// separator is resolved per token rather than assumed.
var numberToken = regexp.MustCompile(`\d+(?:[.,]\d+)*`)

// smallCount is the one exemption: integers up to ten.
//
// Counting is not calculating. "the top three actions", "all five slots", "you
// are two levels short" are sentences a model has to be able to write, and
// none of them can misstate a damage figure. Anything larger has to be
// sourced — including every gain, every resin cost and every stat total.
const smallCount = 10

// values returns every reading of a numeric token.
//
// "1,446" is one thousand four hundred and forty-six to an English reader and
// one point four four six to a Danish one, and the fact sheet is written in
// one language while the answer is written in the other. Returning both and
// accepting either is deliberate: the alternative is a check that fires on
// correctly quoted figures, and a check that cries wolf gets switched off.
func values(token string) []float64 {
	cleaned := token
	if cleaned == "" {
		return nil
	}

	seps := strings.Count(cleaned, ".") + strings.Count(cleaned, ",")
	if seps == 0 {
		if v, err := strconv.ParseFloat(cleaned, 64); err == nil {
			return []float64{v}
		}
		return nil
	}

	var out []float64
	add := func(s string) {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			out = append(out, v)
		}
	}

	// Grouped digits: strip every separator and read it as one integer.
	// 1.446.853 and 1,446,853 are the same number written for two audiences.
	add(strings.NewReplacer(".", "", ",", "").Replace(cleaned))

	// The last separator as a decimal point, everything before it grouping.
	if i := strings.LastIndexAny(cleaned, ".,"); i > 0 {
		head := strings.NewReplacer(".", "", ",", "").Replace(cleaned[:i])
		add(head + "." + cleaned[i+1:])
	}
	return out
}

// decimals counts the digits after the decimal separator, for the reading that
// treats the last separator as one. It sets how far a quoted figure may be
// rounded: a fact sheet saying 34.53 may be quoted as 35, but one saying 35
// may not be quoted as 34.53.
func decimals(token string) int {
	i := strings.LastIndexAny(token, ".,")
	if i < 0 {
		return 0
	}
	return len(token) - i - 1
}

// Sourced is the set of numbers an answer is allowed to contain: everything
// the engine put in front of the model.
type Sourced struct {
	values []float64
}

// Collect reads every number out of the text the model was given.
func Collect(texts ...string) *Sourced {
	s := &Sourced{}
	for _, text := range texts {
		for _, token := range numberToken.FindAllString(text, -1) {
			s.values = append(s.values, values(token)...)
		}
	}
	return s
}

// Add folds another document's numbers in — a tool result mid-conversation,
// which is as sourced as the opening fact sheet was.
func (s *Sourced) Add(texts ...string) {
	if s == nil {
		return
	}
	s.values = append(s.values, Collect(texts...).values...)
}

// allows reports whether one token is sourced.
func (s *Sourced) allows(token string) bool {
	cands := values(token)
	if len(cands) == 0 {
		return true // not actually a number; nothing to check
	}

	d := decimals(token)
	// Half a unit in the last decimal place quoted, plus a hair for binary
	// floating point: 0.1+0.2 must not be what makes a true figure fail.
	tolerance := 0.5*math.Pow(10, -float64(d)) + 1e-9

	for _, c := range cands {
		if c <= smallCount && c == math.Trunc(c) && c >= 0 {
			return true
		}
		for _, known := range s.values {
			if math.Abs(known-c) <= tolerance {
				return true
			}
		}
	}
	return false
}

// Unsourced returns the numbers in text that the engine never produced.
//
// The result is the list of figures, deduplicated and in the order they
// appear, so a caller can name them: "Kvasir wrote 62.4, which is nowhere in
// the calculation" is actionable, "the answer failed validation" is not.
func (s *Sourced) Unsourced(text string) []string {
	if s == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, token := range numberToken.FindAllString(text, -1) {
		if s.allows(token) || seen[token] {
			continue
		}
		seen[token] = true
		out = append(out, token)
	}
	return out
}
