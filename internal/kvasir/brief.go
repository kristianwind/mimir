package kvasir

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// A brief is everything Kvasir is allowed to know.
//
// It is assembled by the caller from the engine's own output — the ranked
// plan, the build sheet, the measured drop model, the inventory — and it is
// the only thing the model sees about the account. Nothing reaches the model
// that did not come out of a calculation, which is what makes the number check
// in numbers.go meaningful: the set of legitimate figures is exactly the set
// of figures in here.
//
// It is also kept, verbatim, next to the answer it produced. An opinion whose
// evidence has been thrown away cannot be audited later, and "why did Kvasir
// say that?" is a question with a real answer for the same reason the farm
// simulator is seeded.

// Brief is a fact sheet for one surface of the product.
type Brief struct {
	// Surface is which page asked: plan, goal, character, artifacts, roster.
	Surface string `json:"surface"`
	// Subject is the character or set the surface is about, where it has one.
	Subject string `json:"subject,omitempty"`
	Title   string `json:"title"`
	// Question is what the page wants an opinion about, phrased for the
	// model. Different surfaces want different judgment out of the same
	// engine: the plan wants sequencing, the roster wants triage.
	Question string     `json:"question"`
	Sections []*Section `json:"sections"`
}

// Section is one heading and its lines.
type Section struct {
	Heading string   `json:"heading"`
	Lines   []string `json:"lines"`
}

// NewBrief starts a fact sheet.
func NewBrief(surface, subject, title, question string) *Brief {
	return &Brief{Surface: surface, Subject: subject, Title: title, Question: question}
}

// Add opens a section and returns it for filling.
func (b *Brief) Add(heading string) *Section {
	s := &Section{Heading: heading}
	b.Sections = append(b.Sections, s)
	return s
}

// Line appends one fact.
func (s *Section) Line(text string) {
	if s == nil {
		return
	}
	text = strings.TrimSpace(text)
	if text != "" {
		s.Lines = append(s.Lines, text)
	}
}

// Linef appends one formatted fact.
func (s *Section) Linef(format string, args ...any) {
	s.Line(fmt.Sprintf(format, args...))
}

// Empty reports whether a section gathered nothing, so a caller can leave it
// out rather than show a heading with nothing under it.
func (s *Section) Empty() bool { return s == nil || len(s.Lines) == 0 }

// Text renders the brief as the document the model reads.
//
// Markdown-ish and flat: headings and dashes. Every model in the target range
// handles it, and it keeps the fact sheet readable by the person auditing an
// answer, which is half of why it is stored.
func (b *Brief) Text() string {
	var sb strings.Builder
	sb.WriteString("# ")
	sb.WriteString(b.Title)
	sb.WriteString("\n")
	for _, s := range b.Sections {
		if s.Empty() {
			continue
		}
		sb.WriteString("\n## ")
		sb.WriteString(s.Heading)
		sb.WriteString("\n")
		for _, line := range s.Lines {
			sb.WriteString("- ")
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// Facts reports whether the brief contains anything worth an opinion. A brief
// with no sections means the engine had nothing to say, and asking a model to
// comment on an empty page produces exactly the invented filler this layer
// exists to prevent.
func (b *Brief) Facts() bool {
	for _, s := range b.Sections {
		if !s.Empty() {
			return true
		}
	}
	return false
}

// Hash identifies the facts, so an unchanged account does not buy a second
// opinion. It covers the rendered text: two briefs that read identically are
// identical, whatever built them.
func (b *Brief) Hash() string {
	sum := sha256.Sum256([]byte(b.Surface + "\x00" + b.Subject + "\x00" + b.Text()))
	return hex.EncodeToString(sum[:16])
}
