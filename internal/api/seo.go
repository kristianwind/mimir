package api

// What a crawler sees.
//
// The frontend is a single page application: every public address is served
// the same index.html and the right page is chosen in the browser. That is
// fine for a person and useless for a machine. Google will run the script
// eventually, but the crawlers that decide what a pasted link looks like —
// Discord, Reddit, Slack, iMessage, Facebook, Bluesky — do not run any
// script at all. They fetch the document, read the head, and give up. A tool
// for a game spreads by being pasted into a Discord channel, so a link that
// unfurls as a bare domain with no title is the difference between a click
// and a scroll past.
//
// So the tags are written into the document by the server, per path, before
// it goes out. Three rules follow from that:
//
//   - Only the marketing pages get described. Everything else is the
//     application, which is behind a login and has nothing to offer a search
//     result, so it is served the default head and told not to index.
//   - Only the hosted instance gets any of it. Somebody running Mimir at home
//     did not ask to appear in Google, and robots.txt there refuses
//     everything.
//   - The text lives here and in site.js both, which is duplication with a
//     reason: one copy has to survive with no JavaScript and the other has to
//     react to state. A test keeps the shared claims from drifting apart.

import (
	"bytes"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// seoPage is what one public address claims to be.
type seoPage struct {
	// Title is the browser tab and the headline of a search result. Google
	// truncates around sixty characters, so the distinguishing words go
	// first and the brand goes last.
	Title string
	// Description is the grey line under the result and the body of a link
	// preview. Around 155 characters before it is cut.
	Description string
	// Priority and Changes feed the sitemap.
	Priority string
	Changes  string
}

// seoPages is every address worth finding. The keys are exactly the paths in
// site.js's PAGES, and a test says so.
var seoPages = map[string]seoPage{
	"/": {
		Title: "Mimir — Genshin Impact build advisor",
		Description: "Mimir ranks every upgrade across your whole Genshin Impact " +
			"account by the damage it actually adds — starting with the gear already " +
			"in your bag. 14 days free.",
		Priority: "1.0",
		Changes:  "weekly",
	},
	"/pricing": {
		Title: "Pricing — Mimir for Genshin Impact",
		Description: "Mimir for Genshin Impact is $4 a month or $40 a year, tax " +
			"included. Fourteen days free and no card to start. Self-hosting is free " +
			"and always will be.",
		Priority: "0.8",
		Changes:  "monthly",
	},
	"/terms": {
		Title:       "Terms of service — Mimir",
		Description: "The terms for a subscription to the hosted Mimir service.",
		Priority:    "0.3",
		Changes:     "yearly",
	},
	"/privacy": {
		Title: "Privacy — Mimir",
		Description: "What Mimir stores, for how long, and what it never does. " +
			"No analytics, no tracking, no advertising.",
		Priority: "0.3",
		Changes:  "yearly",
	},
	"/refunds": {
		Title:       "Refunds — Mimir",
		Description: "How to cancel a Mimir subscription and when a refund applies.",
		Priority:    "0.3",
		Changes:     "yearly",
	},
	"/contact": {
		Title:       "Contact — Mimir",
		Description: "Who runs the hosted Mimir service and how to reach them.",
		Priority:    "0.3",
		Changes:     "yearly",
	},
}

// ogImage is the picture in a link preview, and the only reason it is a
// fixed path rather than per page: one card that reads well small beats six
// that were never looked at.
const ogImage = "/og.jpg"

// seo renders and caches the head of index.html per path.
//
// Cached because the document is embedded in the binary and the substitution
// is the same every time. Built lazily so a Server with no Web filesystem —
// which is most tests — costs nothing.
type seo struct {
	once   sync.Once
	origin string
	pages  map[string]string
	other  string
	err    error
}

// canonicalOrigin trims a configured base URL down to scheme and host.
//
// It is the address every absolute URL on the page is built from, and a
// trailing slash or a stray path would produce "https://mimir.guide//pricing"
// in a canonical tag — which is a different address as far as a crawler is
// concerned, and therefore duplicate content pointing at itself.
func canonicalOrigin(base string) string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// build renders every variant once.
func (s *Server) seoBuild() {
	s.seoCache.pages = map[string]string{}
	if s.Web == nil {
		return
	}
	raw, err := fs.ReadFile(s.Web, "index.html")
	if err != nil {
		s.seoCache.err = err
		return
	}
	doc := string(raw)
	s.seoCache.origin = canonicalOrigin(s.Config.BaseURL)

	// Anything that is not a marketing page is the application. It gets the
	// document untouched apart from a refusal to be indexed: those pages are
	// a login screen to anybody who is not signed in, and a search result
	// leading to one helps nobody.
	s.seoCache.other = inject(doc, `<meta name="robots" content="noindex, follow" />`, "")

	if !s.Config.Hosted {
		return
	}
	for path, page := range seoPages {
		s.seoCache.pages[path] = inject(doc, s.seoHead(path, page), page.Title)
	}
}

// seoHead is the block written into <head> for one public page.
func (s *Server) seoHead(path string, page seoPage) string {
	origin := s.seoCache.origin
	canonical := origin + path
	image := origin + ogImage

	var b strings.Builder
	esc := func(v string) string { return html.EscapeString(v) }

	fmt.Fprintf(&b, `<meta name="description" content="%s" />`+"\n    ", esc(page.Description))
	if origin != "" {
		fmt.Fprintf(&b, `<link rel="canonical" href="%s" />`+"\n    ", esc(canonical))
	}
	b.WriteString(`<meta name="robots" content="index, follow, max-image-preview:large" />` + "\n    ")

	// Open Graph. Read by Discord, Facebook, LinkedIn, Bluesky and most
	// chat clients.
	fmt.Fprintf(&b, `<meta property="og:type" content="website" />`+"\n    ")
	fmt.Fprintf(&b, `<meta property="og:site_name" content="Mimir" />`+"\n    ")
	fmt.Fprintf(&b, `<meta property="og:title" content="%s" />`+"\n    ", esc(page.Title))
	fmt.Fprintf(&b, `<meta property="og:description" content="%s" />`+"\n    ", esc(page.Description))
	if origin != "" {
		fmt.Fprintf(&b, `<meta property="og:url" content="%s" />`+"\n    ", esc(canonical))
		fmt.Fprintf(&b, `<meta property="og:image" content="%s" />`+"\n    ", esc(image))
		b.WriteString(`<meta property="og:image:width" content="1200" />` + "\n    ")
		b.WriteString(`<meta property="og:image:height" content="630" />` + "\n    ")
		fmt.Fprintf(&b, `<meta property="og:image:alt" content="%s" />`+"\n    ",
			esc("Mimir — Genshin Impact build advisor"))
	}

	// Twitter reads its own names and falls back to Open Graph for the rest.
	b.WriteString(`<meta name="twitter:card" content="summary_large_image" />` + "\n    ")
	fmt.Fprintf(&b, `<meta name="twitter:title" content="%s" />`+"\n    ", esc(page.Title))
	fmt.Fprintf(&b, `<meta name="twitter:description" content="%s" />`+"\n    ", esc(page.Description))
	if origin != "" {
		fmt.Fprintf(&b, `<meta name="twitter:image" content="%s" />`+"\n    ", esc(image))
	}

	// Structured data, on the front page only. Repeating it on the terms
	// page would describe the same product twice at two addresses, which is
	// the thing canonical tags exist to prevent.
	if path == "/" && origin != "" {
		b.WriteString(s.structuredData(origin))
	}

	// The title element already exists in the document, so it is replaced
	// rather than added — two titles and the crawler picks, which is not a
	// decision worth handing over.
	return strings.TrimRight(b.String(), " \n")
}

// structuredData describes the product to a search engine in its own words.
//
// SoftwareApplication with an offer is what puts a price and a rating slot
// into a result. No rating is claimed, because there are no reviews and
// inventing them is both against the rules and the sort of thing this
// product is explicitly not.
func (s *Server) structuredData(origin string) string {
	const tmpl = `<script type="application/ld+json">{"@context":"https://schema.org",` +
		`"@type":"SoftwareApplication","name":"Mimir",` +
		`"applicationCategory":"GameApplication",` +
		`"applicationSubCategory":"Genshin Impact build advisor",` +
		`"operatingSystem":"Any modern browser",` +
		`"url":"%s/","image":"%s%s",` +
		`"description":"%s",` +
		`"offers":[{"@type":"Offer","price":"4.00","priceCurrency":"USD",` +
		`"category":"subscription","url":"%s/pricing"},` +
		`{"@type":"Offer","price":"40.00","priceCurrency":"USD",` +
		`"category":"subscription","url":"%s/pricing"}],` +
		`"isAccessibleForFree":false,` +
		`"softwareHelp":{"@type":"CreativeWork","url":"https://github.com/kristianwind/mimir"}}` +
		`</script>`
	return fmt.Sprintf(tmpl, origin, origin, ogImage,
		jsonEscape(seoPages["/"].Description), origin, origin)
}

// jsonEscape makes a description safe inside the JSON-LD literal above.
//
// The angle brackets become \u003c and \u003e rather than staying as
// themselves: a description containing "</script>" would otherwise close the
// tag it lives in, and the rest of the page would be parsed as markup. The
// text is a compile-time constant today, so this is guarding a door nobody is
// currently at — which is the only time to fit a lock.
func jsonEscape(v string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", " ",
		"<", `\u003c`,
		">", `\u003e`,
		"&", `\u0026`,
	)
	return r.Replace(v)
}

// titleTag matches the one already in the document.
var titleTag = regexp.MustCompile(`(?s)<title>.*?</title>`)

// inject puts a block just before </head> and, where a title is given,
// replaces the existing one rather than adding a second. Two titles means the
// crawler chooses, and that is not a decision worth handing over.
func inject(doc, block, title string) string {
	if title != "" {
		doc = titleTag.ReplaceAllLiteralString(doc, "<title>"+html.EscapeString(title)+"</title>")
	}
	const closer = "</head>"
	i := strings.Index(doc, closer)
	if i < 0 {
		return doc
	}
	// The indentation before </head> is trimmed first, or it lands on top of
	// the block's own and the first tag sits two spaces further in than the
	// rest.
	return strings.TrimRight(doc[:i], " \t") + "    " + block + "\n  " + doc[i:]
}

// seoDocument returns the HTML for a path, and whether there is any.
func (s *Server) seoDocument(path string) (string, bool) {
	s.seoCache.once.Do(s.seoBuild)
	if s.seoCache.err != nil {
		return "", false
	}
	if doc, ok := s.seoCache.pages[path]; ok {
		return doc, true
	}
	if s.seoCache.other != "" {
		return s.seoCache.other, true
	}
	return "", false
}

// handleRobots tells crawlers what they may read.
//
// A self-hosted instance refuses everything. Somebody who runs this on a box
// at home to look at their own account did not ask to be in a search index,
// and the fact that it is reachable is not consent to be listed.
func (s *Server) handleRobots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	seoCaching(w)
	if !s.Config.Hosted {
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
		return
	}

	var b strings.Builder
	b.WriteString("User-agent: *\n")
	b.WriteString("Allow: /$\n")
	for _, p := range sortedSEOPaths() {
		if p != "/" {
			fmt.Fprintf(&b, "Allow: %s\n", p)
		}
	}
	// The API, and the form. Nothing else is listed, because nothing else is
	// an address: once signed in the application swaps its whole view in
	// place and never touches the URL, so "/plan" and the rest are not
	// pages a crawler could reach even in principle. Every path that is not
	// a marketing page is served the document with "noindex" in its head,
	// which is the part that actually does the work — this file only saves
	// the crawler the fetch.
	b.WriteString("Disallow: /api/\n")
	b.WriteString("Disallow: /signup\n")
	if origin := s.seoOrigin(); origin != "" {
		fmt.Fprintf(&b, "\nSitemap: %s/sitemap.xml\n", origin)
	}
	_, _ = w.Write([]byte(b.String()))
}

// handleSitemap lists the public pages. Absent where there is no public site.
func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	origin := s.seoOrigin()
	if !s.Config.Hosted || origin == "" {
		http.NotFound(w, r)
		return
	}
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, p := range sortedSEOPaths() {
		page := seoPages[p]
		fmt.Fprintf(&b, "  <url><loc>%s%s</loc><changefreq>%s</changefreq><priority>%s</priority></url>\n",
			html.EscapeString(origin), html.EscapeString(p), page.Changes, page.Priority)
	}
	b.WriteString("</urlset>\n")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	seoCaching(w)
	_, _ = w.Write(b.Bytes())
}

// seoCaching bounds how long a wrong answer can outlive the deploy that fixed
// it.
//
// Learned from mimir.guide: Cloudflare caches by file extension, and .txt is
// on its default list, so the robots.txt served *before* this feature existed
// — which was the SPA's index.html, because the route did not exist yet —
// was held at the edge for four hours after the deploy that added the real
// one. The origin was correct and every crawler saw HTML.
//
// Ten minutes is long enough that these two are not fetched from Go on every
// crawl, and short enough that a mistake in either is measured in minutes
// rather than in whatever the CDN felt like.
func seoCaching(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "public, max-age=600")
}

func (s *Server) seoOrigin() string {
	s.seoCache.once.Do(s.seoBuild)
	if s.seoCache.origin != "" {
		return s.seoCache.origin
	}
	return canonicalOrigin(s.Config.BaseURL)
}

// sortedSEOPaths keeps robots.txt and the sitemap in a stable order, so a
// diff of either shows a change rather than a shuffle.
func sortedSEOPaths() []string {
	paths := make([]string, 0, len(seoPages))
	for p := range seoPages {
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool {
		if seoPages[paths[i]].Priority != seoPages[paths[j]].Priority {
			return seoPages[paths[i]].Priority > seoPages[paths[j]].Priority
		}
		return paths[i] < paths[j]
	})
	return paths
}
