package api

import (
	"log/slog"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/kristianwind/mimir/internal/config"
)

// indexDoc is the shape the real bundle has: one title, one head.
const indexDoc = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <title>Mimir — Genshin Impact build advisor</title>
  </head>
  <body><div id="app"></div></body>
</html>
`

func seoServer(t *testing.T, hosted bool, base string) *Server {
	t.Helper()
	return &Server{
		Config: &config.Config{Hosted: hosted, BaseURL: base},
		Log:    slog.New(slog.DiscardHandler),
		Web:    fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte(indexDoc)}},
	}
}

// The whole point of the exercise: a crawler that runs no JavaScript still
// gets a title, a description and a picture. Discord and Reddit decide what a
// pasted link looks like without executing a line of script, and a link that
// unfurls as a bare domain is one nobody clicks.
func TestACrawlerWithNoJavaScriptStillGetsAPage(t *testing.T) {
	s := seoServer(t, true, "https://mimir.guide")

	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, httptest.NewRequest("GET", "/pricing", nil))
	body := w.Body.String()

	for _, want := range []string{
		`<title>Pricing — Mimir for Genshin Impact</title>`,
		`<meta name="description"`,
		`<link rel="canonical" href="https://mimir.guide/pricing" />`,
		`<meta property="og:title" content="Pricing — Mimir for Genshin Impact" />`,
		`<meta property="og:url" content="https://mimir.guide/pricing" />`,
		`<meta property="og:image" content="https://mimir.guide/og.jpg" />`,
		`<meta name="twitter:card" content="summary_large_image" />`,
		`index, follow`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the document is missing %s", want)
		}
	}

	// And exactly one title, or the crawler picks which to believe.
	if n := strings.Count(body, "<title>"); n != 1 {
		t.Errorf("title count = %d, want 1", n)
	}
	// The old one is gone rather than left further up the head.
	if strings.Contains(body, "<title>Mimir — Genshin Impact build advisor</title>") {
		t.Error("the default title survived on a page with its own")
	}
}

// The front page carries the structured data, and only the front page.
// Repeating it would describe one product at six addresses, which is the
// thing a canonical tag exists to prevent.
func TestOnlyTheFrontPageDescribesTheProduct(t *testing.T) {
	s := seoServer(t, true, "https://mimir.guide")

	home := httptest.NewRecorder()
	s.Router().ServeHTTP(home, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(home.Body.String(), `"@type":"SoftwareApplication"`) {
		t.Error("the front page carries no structured data")
	}
	// The price in the markup has to be the price on the page, or a search
	// result advertises a number nobody honours.
	if !strings.Contains(home.Body.String(), `"price":"4.00"`) {
		t.Error("the structured data does not carry the monthly price")
	}

	terms := httptest.NewRecorder()
	s.Router().ServeHTTP(terms, httptest.NewRequest("GET", "/terms", nil))
	if strings.Contains(terms.Body.String(), `"@type":"SoftwareApplication"`) {
		t.Error("the terms page describes the product a second time")
	}
}

// A self-hosted Mimir is somebody's own machine. Being reachable is not the
// same as asking to be listed.
func TestASelfHostedInstanceIsNotAdvertised(t *testing.T) {
	s := seoServer(t, false, "https://mimir.example.com")

	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	if strings.Contains(body, "og:title") || strings.Contains(body, "canonical") {
		t.Errorf("a self-hosted instance advertised itself: %s", body)
	}
	if !strings.Contains(body, "noindex") {
		t.Error("a self-hosted instance did not ask to be left out")
	}

	robots := httptest.NewRecorder()
	s.Router().ServeHTTP(robots, httptest.NewRequest("GET", "/robots.txt", nil))
	if got := robots.Body.String(); !strings.Contains(got, "Disallow: /\n") || strings.Contains(got, "Allow:") {
		t.Errorf("robots.txt does not refuse everything:\n%s", got)
	}

	// And there is no map of a site that is not public.
	sitemap := httptest.NewRecorder()
	s.Router().ServeHTTP(sitemap, httptest.NewRequest("GET", "/sitemap.xml", nil))
	if sitemap.Code != 404 {
		t.Errorf("sitemap status = %d, want 404 on a self-hosted instance", sitemap.Code)
	}
}

// The application is behind a login. A crawler spending its budget on pages
// that answer "sign in" is budget not spent on the pages that sell.
func TestTheApplicationAsksNotToBeIndexed(t *testing.T) {
	s := seoServer(t, true, "https://mimir.guide")

	for _, path := range []string{"/plan", "/characters", "/settings"} {
		w := httptest.NewRecorder()
		s.Router().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if !strings.Contains(w.Body.String(), `content="noindex, follow"`) {
			t.Errorf("%s does not ask to be left out of the index", path)
		}
	}

	robots := httptest.NewRecorder()
	s.Router().ServeHTTP(robots, httptest.NewRequest("GET", "/robots.txt", nil))
	got := robots.Body.String()
	for _, want := range []string{"Disallow: /api/", "Disallow: /plan", "Allow: /pricing",
		"Sitemap: https://mimir.guide/sitemap.xml"} {
		if !strings.Contains(got, want) {
			t.Errorf("robots.txt is missing %q:\n%s", want, got)
		}
	}
}

// A base URL with a trailing slash would produce https://mimir.guide//pricing
// in the canonical tag — a different address as far as a crawler is
// concerned, and so a page declaring itself a duplicate of something that
// does not exist.
func TestACanonicalURLSurvivesAnUntidyBaseURL(t *testing.T) {
	for _, base := range []string{
		"https://mimir.guide",
		"https://mimir.guide/",
		"  https://mimir.guide/  ",
	} {
		s := seoServer(t, true, base)
		w := httptest.NewRecorder()
		s.Router().ServeHTTP(w, httptest.NewRequest("GET", "/pricing", nil))
		if !strings.Contains(w.Body.String(), `href="https://mimir.guide/pricing"`) {
			t.Errorf("base %q produced a bad canonical", base)
		}
	}
}

// An instance with no usable base URL still serves pages; it just claims no
// absolute addresses, because a canonical tag pointing at localhost is worse
// than none.
func TestNoBaseURLMeansNoAbsoluteClaims(t *testing.T) {
	s := seoServer(t, true, "not a url")
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, httptest.NewRequest("GET", "/pricing", nil))
	body := w.Body.String()
	if strings.Contains(body, "canonical") || strings.Contains(body, "og:url") {
		t.Errorf("an unusable base URL still produced absolute claims: %s", body)
	}
	if !strings.Contains(body, `<meta name="description"`) {
		t.Error("the page lost its description along with its origin")
	}
}

// The sitemap lists the public pages and nothing else.
func TestTheSitemapListsThePublicPages(t *testing.T) {
	s := seoServer(t, true, "https://mimir.guide")
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, httptest.NewRequest("GET", "/sitemap.xml", nil))

	got := w.Body.String()
	if !strings.HasPrefix(got, `<?xml`) {
		t.Errorf("not XML:\n%s", got)
	}
	for path := range seoPages {
		if !strings.Contains(got, "<loc>https://mimir.guide"+path+"</loc>") {
			t.Errorf("the sitemap omits %s", path)
		}
	}
	if strings.Contains(got, "/plan") || strings.Contains(got, "/api") {
		t.Errorf("the sitemap lists the application:\n%s", got)
	}
	// The front page first, because the order is by priority and a shuffled
	// file makes every diff unreadable.
	if !strings.HasPrefix(sortedSEOPaths()[0], "/") || sortedSEOPaths()[0] != "/" {
		t.Errorf("first path = %q, want /", sortedSEOPaths()[0])
	}
}

// The list of pages lives in two places — Go, for the crawler, and site.js,
// for the person. Duplication with a reason, but duplication, so it is
// checked rather than trusted: a page added to the footer and forgotten here
// is a page no search engine ever hears about.
func TestEveryPublicPageIsDescribed(t *testing.T) {
	raw, err := os.ReadFile("../../web/src/lib/public/site.js")
	if err != nil {
		t.Skipf("no frontend source to compare against: %v", err)
	}

	block := regexp.MustCompile(`(?s)export const PAGES = \[(.*?)\]`).FindSubmatch(raw)
	if block == nil {
		t.Fatal("could not find PAGES in site.js")
	}
	var inSite []string
	for _, m := range regexp.MustCompile(`path:\s*'([^']+)'`).FindAllSubmatch(block[1], -1) {
		inSite = append(inSite, string(m[1]))
	}
	if len(inSite) == 0 {
		t.Fatal("PAGES parsed as empty")
	}

	inGo := sortedSEOPaths()
	sort.Strings(inGo)
	sort.Strings(inSite)
	if strings.Join(inGo, " ") != strings.Join(inSite, " ") {
		t.Errorf("the two lists have drifted apart\n  seo.go:  %v\n  site.js: %v", inGo, inSite)
	}
}

// Titles and descriptions are cut off in a search result at roughly these
// lengths. A description that ends mid-word is a description somebody wrote
// and nobody measured.
func TestDescriptionsFitInASearchResult(t *testing.T) {
	for path, page := range seoPages {
		if n := len([]rune(page.Title)); n > 60 {
			t.Errorf("%s: title is %d characters, over 60: %q", path, n, page.Title)
		}
		if n := len([]rune(page.Description)); n < 50 || n > 160 {
			t.Errorf("%s: description is %d characters, want 50–160: %q", path, n, page.Description)
		}
		// The words people search for. Not on every page — a refund policy
		// is not a landing page — but on the two that sell.
		if (path == "/" || path == "/pricing") &&
			!strings.Contains(page.Description, "Genshin Impact") {
			t.Errorf("%s: the description never names the game", path)
		}
	}
}
