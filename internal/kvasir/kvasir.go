// Package kvasir is Mimir's AI layer.
//
// Mimir is the head at the well that knows; Kvasir is the one the gods made
// from the mead, who could answer anything asked of him. The division of
// labour is the same here as in the myth: the engine knows the numbers, and
// Kvasir is who you ask what they mean.
//
// The whole package exists under one constraint, which is the third rule in
// ARCHITECTURE.md: the model explains, it never computes. That is enforced
// twice over rather than promised.
//
//   - The model is never given a calculator, an inventory table or a rulebook
//     it could reason a multiplier out of. It is given a fact sheet — see
//     brief.go — assembled by Go from what the engine returned.
//   - Every number in its answer is checked back against that fact sheet — see
//     numbers.go. A point containing a figure the engine never produced is
//     deleted before the player ever sees it, and what was deleted is
//     reported rather than quietly dropped.
//
// This mirrors the rule that makes the hand-written effect library safe: a
// claim only loads if the numbers are in the text it cites. Here the citation
// is the engine's own output, and the claim is a sentence.
//
// With no endpoint configured the package returns ErrNotConfigured and the
// rest of Mimir is unchanged. Nothing in the product depends on a model
// answering, because nothing in the product is calculated by one.
package kvasir

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/kristianwind/mimir/internal/i18n"
	"github.com/kristianwind/mimir/internal/llm"
)

// ErrNotConfigured means no LLM endpoint was set up. Callers hide the AI
// layer rather than reporting a fault: an operator who did not configure a
// model did not fail at anything.
var ErrNotConfigured = llm.ErrNotConfigured

// ErrNoFacts means the engine had nothing to hand over. Asking for an opinion
// on an empty fact sheet is asking for invention.
var ErrNoFacts = errors.New("kvasir: there is nothing calculated to have an opinion about")

// ErrUnsourced means every part of the answer quoted a number the engine never
// produced, twice in a row. Nothing is shown: a plausible sentence with a
// fabricated figure in it is worse than no sentence.
var ErrUnsourced = errors.New("kvasir: the answer used numbers that are not in the calculation")

// Opinion is what Kvasir says.
type Opinion struct {
	// Verdict is the one thing that matters most right now.
	Verdict string `json:"verdict"`
	// Points are the specific recommendations, best first.
	Points []Point `json:"points"`
	// Questions are what Kvasir would need answered to say more. They matter:
	// most of what the engine cannot price is a condition nobody declared,
	// and a model that guesses those instead of asking is the failure this
	// whole layer is arranged to avoid.
	Questions []string `json:"questions,omitempty"`
}

// Point is one recommendation.
type Point struct {
	Headline string `json:"headline"`
	Why      string `json:"why"`
	Do       string `json:"do,omitempty"`
}

func (p Point) text() string { return p.Headline + " " + p.Why + " " + p.Do }

// Dropped is a piece of an answer that was removed because of the numbers in
// it. It is part of the response, not a log line — an opinion that has been
// silently edited is not an opinion the player can weigh.
type Dropped struct {
	Text    string   `json:"text"`
	Numbers []string `json:"numbers"`
}

// Result is one answer with its provenance.
type Result struct {
	Opinion Opinion   `json:"opinion"`
	Dropped []Dropped `json:"dropped,omitempty"`
	Model   string    `json:"model,omitempty"`
	Usage   llm.Usage `json:"usage,omitzero"`
	// Attempts counts completions spent. Two means the first answer failed
	// the number check and was asked again.
	Attempts int `json:"attempts"`
}

// Advisor turns fact sheets into opinions.
type Advisor struct {
	Client *llm.Client
	// MaxTokens bounds one answer. Local models default to something small
	// enough to cut a fourth point in half.
	MaxTokens int
}

// Available reports whether there is a model to ask.
func (a *Advisor) Available() bool { return a != nil && a.Client.Configured() }

// Advise asks for an opinion on one brief.
func (a *Advisor) Advise(ctx context.Context, brief *Brief, lang i18n.Lang) (Result, error) {
	if !a.Available() {
		return Result{}, ErrNotConfigured
	}
	if brief == nil || !brief.Facts() {
		return Result{}, ErrNoFacts
	}

	facts := brief.Text()
	sourced := Collect(facts)

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: systemPrompt(lang)},
		{Role: llm.RoleUser, Content: userPrompt(brief)},
	}

	var out Result
	for attempt := 1; attempt <= 2; attempt++ {
		reply, err := a.Client.Complete(ctx, llm.Request{
			Messages:    messages,
			Temperature: 0.2,
			MaxTokens:   a.maxTokens(),
			JSONObject:  true,
		})
		if err != nil {
			return Result{}, err
		}
		out.Attempts = attempt
		out.Model = a.Client.Model
		out.Usage = reply.Usage

		opinion, err := parseOpinion(reply.Message.Content)
		if err != nil {
			if attempt == 2 {
				return out, err
			}
			messages = append(messages,
				llm.Message{Role: llm.RoleAssistant, Content: reply.Message.Content},
				llm.Message{Role: llm.RoleUser, Content: "That was not a JSON object in the shape asked for. " +
					"Answer again with the JSON object only, no prose around it."})
			continue
		}

		kept, dropped := verify(opinion, sourced)
		if len(dropped) == 0 {
			out.Opinion = kept
			return out, nil
		}
		if attempt == 2 {
			// Second answer, still quoting figures from nowhere. Keep what
			// survived and say what did not; if nothing survived, say that
			// instead of showing an empty card.
			out.Opinion = kept
			out.Dropped = dropped
			if kept.Verdict == "" && len(kept.Points) == 0 {
				return out, ErrUnsourced
			}
			return out, nil
		}
		messages = append(messages,
			llm.Message{Role: llm.RoleAssistant, Content: reply.Message.Content},
			llm.Message{Role: llm.RoleUser, Content: correction(dropped)})
	}
	return out, ErrUnsourced
}

func (a *Advisor) maxTokens() int {
	if a.MaxTokens > 0 {
		return a.MaxTokens
	}
	return 1200
}

// verify strips the parts of an opinion that quote unsourced numbers.
//
// Per point rather than per answer: one bad figure in the third bullet is no
// reason to throw away two good ones, and a check that discards everything on
// any violation is a check that gets relaxed the first time it fires.
func verify(o Opinion, sourced *Sourced) (Opinion, []Dropped) {
	var dropped []Dropped
	kept := Opinion{}

	if bad := sourced.Unsourced(o.Verdict); len(bad) > 0 {
		dropped = append(dropped, Dropped{Text: o.Verdict, Numbers: bad})
	} else {
		kept.Verdict = o.Verdict
	}

	for _, p := range o.Points {
		if bad := sourced.Unsourced(p.text()); len(bad) > 0 {
			dropped = append(dropped, Dropped{Text: p.Headline, Numbers: bad})
			continue
		}
		kept.Points = append(kept.Points, p)
	}

	for _, q := range o.Questions {
		if bad := sourced.Unsourced(q); len(bad) > 0 {
			dropped = append(dropped, Dropped{Text: q, Numbers: bad})
			continue
		}
		kept.Questions = append(kept.Questions, q)
	}
	return kept, dropped
}

func correction(dropped []Dropped) string {
	var nums []string
	for _, d := range dropped {
		nums = append(nums, d.Numbers...)
	}
	return fmt.Sprintf(
		"These numbers are not in the fact sheet: %s. They were not calculated, so they cannot be said. "+
			"Answer again. Use only figures that appear in the fact sheet, or make the point without a figure.",
		strings.Join(nums, ", "))
}

// systemPrompt is the whole contract with the model.
func systemPrompt(lang i18n.Lang) string {
	language := "in English"
	if lang == i18n.DA {
		language = "in Danish"
	}
	return `You are Kvasir, the advisor inside Mimir — a Genshin Impact account optimiser.

Mimir's damage engine has already done every calculation. What you are given is
its output: ranked upgrades, resolved stats, measured drop rates, contested
gear, and the things the engine refused to price. Your job is the judgment a
ranking cannot make.

Say what to do first and why, what the ranking does not make obvious, what is
actually holding this account back, and what the player has not told Mimir that
would change the answer.

Hard rules:
1. Every number you write must appear in the fact sheet. Rounding is fine;
   inventing, estimating or extrapolating is not. This is checked
   automatically, and anything with an unsourced number in it is deleted
   before the player sees it.
2. Never name a character, weapon, artifact set or domain that is not in the
   fact sheet. You do not know what else this account owns.
3. Where the fact sheet does not settle something, say so and put what you
   need into "questions". Do not fill the gap with what is usually true.
4. Do not restate the ranking. The player is looking at it. Say what it does
   not say.
5. Write ` + language + `. Be direct. No greeting, no preamble, no summary of
   what you are about to say.

Answer with one JSON object and nothing else:

{"verdict": "one or two sentences: the single most important thing right now",
 "points": [{"headline": "a short imperative",
             "why": "one or two sentences of reasoning",
             "do": "the concrete next step (optional)"}],
 "questions": ["what you need the player to answer (optional)"]}

Two to four points. Fewer good points beat more padded ones.`
}

func userPrompt(b *Brief) string {
	return b.Question + "\n\n" + b.Text()
}

// parseOpinion reads the model's JSON, leniently.
//
// Leniently because response_format is advisory on half the endpoints this
// has to work with: llama.cpp ignores it in some builds, and several models
// wrap a perfectly good object in a fenced code block whatever they are told.
// Failing on that would make the layer look broken when it is one substring
// away from working.
func parseOpinion(content string) (Opinion, error) {
	raw := strings.TrimSpace(content)
	if fence := strings.Index(raw, "```"); fence >= 0 {
		rest := raw[fence+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			rest = rest[:end]
		}
		raw = strings.TrimSpace(rest)
	}
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return Opinion{}, fmt.Errorf("kvasir: the model did not answer with JSON: %s", short(content))
	}

	var o Opinion
	if err := json.Unmarshal([]byte(raw[start:end+1]), &o); err != nil {
		return Opinion{}, fmt.Errorf("kvasir: the model's JSON could not be read: %w", err)
	}
	if strings.TrimSpace(o.Verdict) == "" && len(o.Points) == 0 {
		return Opinion{}, fmt.Errorf("kvasir: the model answered with an empty opinion")
	}
	return o, nil
}

func short(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
