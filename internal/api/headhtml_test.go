package api

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/kristianwind/mimir/internal/config"
)

// The tag Kristian is actually going to paste in. It must survive byte for
// byte: a defer attribute that got dropped or a quote that got escaped is a
// tag that silently does not run.
const plausible = `<script defer data-domain="mimir.guide" src="https://plausible.yggdrasilpanel.com/js/script.js"></script>`

func headDoc(t *testing.T, cfg config.Config) string {
	t.Helper()
	web := fstest.MapFS{"index.html": &fstest.MapFile{
		Data: []byte("<!doctype html><html><head><title>Mimir</title>\n  </head><body></body></html>")}}
	s := &Server{Config: &cfg, Web: web}
	doc, _ := s.seoDocument("/somewhere-in-the-app")
	return doc
}

func TestTheOperatorsTagIsServedVerbatim(t *testing.T) {
	doc := headDoc(t, config.Config{HeadHTML: plausible})
	if !strings.Contains(doc, plausible) {
		t.Fatalf("the tag did not survive intact:\n%s", doc)
	}
	if strings.Contains(doc, "&lt;script") || strings.Contains(doc, "&quot;") {
		t.Error("the tag was escaped, which turns it into text on the page")
	}
	if i := strings.Index(doc, plausible); i < 0 || i > strings.Index(doc, "</head>") {
		t.Error("the tag is not inside <head>")
	}
}

// It has to be on the marketing pages too, not only the app shell.
func TestTheTagIsOnEveryPage(t *testing.T) {
	web := fstest.MapFS{"index.html": &fstest.MapFile{
		Data: []byte("<!doctype html><html><head><title>Mimir</title>\n  </head><body></body></html>")}}
	s := &Server{Config: &config.Config{HeadHTML: plausible, Hosted: true, BaseURL: "https://mimir.guide"}, Web: web}
	for _, path := range []string{"/", "/pricing", "/anything-else"} {
		doc, ok := s.seoDocument(path)
		if !ok {
			t.Fatalf("no document for %s", path)
		}
		if !strings.Contains(doc, plausible) {
			t.Errorf("%s has no tag", path)
		}
	}
}

// The default. Nobody gets analytics they did not ask for.
func TestNothingIsAddedByDefault(t *testing.T) {
	doc := headDoc(t, config.Config{})
	if strings.Contains(doc, "<script") {
		t.Fatalf("a document with no configured block still carries a script:\n%s", doc)
	}
}

// </head> in the block would end the head early and spill the document's own
// tags into the body — a typo whose symptom looks like anything but this.
func TestABlockThatClosesTheHeadIsRefused(t *testing.T) {
	var buf bytes.Buffer
	web := fstest.MapFS{"index.html": &fstest.MapFile{
		Data: []byte("<!doctype html><html><head><title>Mimir</title>\n  </head><body></body></html>")}}
	s := &Server{
		Config: &config.Config{HeadHTML: "<script></script></head><body>oops"},
		Web:    web,
		Log:    slog.New(slog.NewTextHandler(&buf, nil)),
	}
	if doc, _ := s.seoDocument("/x"); strings.Contains(doc, "oops") {
		t.Error("a block containing </head> was served")
	}
	if !strings.Contains(buf.String(), "</head>") {
		t.Error("it was refused silently; the operator would never learn why nothing appeared")
	}
}

func TestAnAbsurdlyLargeBlockIsRefused(t *testing.T) {
	var buf bytes.Buffer
	s := &Server{
		Config: &config.Config{HeadHTML: strings.Repeat("a", maxHeadHTML+1)},
		Log:    slog.New(slog.NewTextHandler(&buf, nil)),
	}
	if got := s.headHTML(); got != "" {
		t.Error("an oversized block was accepted")
	}
	if !strings.Contains(buf.String(), "larger than") {
		t.Error("the refusal was silent")
	}
}

func TestHeadHTMLWithoutConfig(t *testing.T) {
	if got := (&Server{}).headHTML(); got != "" {
		t.Fatalf("got %q", got)
	}
}
