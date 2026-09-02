package api

// Whatever the operator wants in <head>.
//
// Mimir ships no analytics and will not choose one. This is the seam instead:
// an operator puts their own tag here and the server writes it into every
// page. Kristian's is a self-hosted Plausible, which is a good shape for it —
// no cookie, and the numbers never leave a machine he owns — but nothing here
// knows or cares what the markup is.
//
// It is deliberately NOT in the bundle. web/dist is embedded in the binary, so
// anything written into index.html would ship to every install; injected here,
// an instance that sets nothing serves nothing, and that is every instance
// until somebody decides otherwise.
//
// 🔴 It is also deliberately NOT reachable over HTTP. This value becomes a
// script element on every page of the site, so anybody who could write it
// could run code in every visitor's browser — including an administrator's
// session. A form would make that a privilege escalation one XSS away; an
// environment variable makes it something only whoever can restart the server
// can change, which is the same person who could replace the binary anyway.
// If this ever grows a settings endpoint, it needs a much harder look than a
// role check.

import (
	"strings"
)

// maxHeadHTML bounds the block. Not a security control — the operator is
// trusted by construction — but a mistyped paste of an entire page should fail
// visibly rather than be served to every visitor.
const maxHeadHTML = 8 << 10

// headHTML returns the operator's own markup for <head>, or "" if there is
// none.
//
// Returned verbatim. Escaping it would be security theatre that also breaks
// the feature: a tag that has been escaped is text, not a tag, so the only
// honest options are to insert it as written or not to offer it at all.
func (s *Server) headHTML() string {
	if s.Config == nil {
		return ""
	}
	block := strings.TrimSpace(s.Config.HeadHTML)
	if block == "" {
		return ""
	}
	if len(block) > maxHeadHTML {
		if s.Log != nil {
			s.Log.Error("ignoring the custom <head> block: it is larger than anything a tag should be",
				"bytes", len(block), "limit", maxHeadHTML)
		}
		return ""
	}
	// </head> inside the block would close the head early and put the rest of
	// the document's own tags into the body. That is a typo, not an attack,
	// and it is worth catching because the symptom — a page that renders but
	// loses its metadata — looks like anything except this.
	if strings.Contains(strings.ToLower(block), "</head>") {
		if s.Log != nil {
			s.Log.Error("ignoring the custom <head> block: it contains </head>, which would end the head early")
		}
		return ""
	}
	return block
}
