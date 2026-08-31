package api

// What a request leaves behind.
//
// The privacy page has always said this service records "which pages are
// asked for, how often, and what failed". It did not. The only trace a
// request left was whatever the handler chose to say, so the answer to "is
// anything crawling us?" or "did that page 500 for someone?" was: look at the
// database and guess.
//
// The address a request came from is deliberately absent, even though the
// privacy page permits it. It is the one field here that identifies a person,
// it is not needed for any question this log is meant to answer, and a value
// that is never written cannot leak, cannot be subpoenaed and cannot be
// exported by mistake. Failed sign-ins still log their source — that is a
// security record with a reason to know where an attempt came from, and it
// lives in handlers.go where the reasoning is next to the risk.

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// maxAgentLen bounds the user-agent. They are attacker-controlled and some
// are absurd; the useful part — the crawler's name — is at the front.
const maxAgentLen = 120

// requestLog records one line per request.
func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Log == nil {
			next.ServeHTTP(w, r)
			return
		}
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)

		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		if quiet(r.URL.Path, status) {
			return
		}

		// The path only. A query string can carry a token — a password reset
		// link, a checkout session — and a log is exactly where one should
		// not end up.
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"ms", time.Since(start).Milliseconds(),
			"agent", clip(r.UserAgent(), maxAgentLen),
		}
		switch {
		case status >= 500:
			s.Log.Error("request", attrs...)
		case status >= 400:
			s.Log.Warn("request", attrs...)
		default:
			s.Log.Info("request", attrs...)
		}
	})
}

// quiet reports whether a request is not worth a line.
//
// A single page view pulls the HTML and then a dozen hashed assets, so
// logging every one of them buries the request that mattered under its own
// scenery. Only successful asset fetches are dropped: a 404 on an asset is a
// broken build and is exactly what someone would come here looking for.
func quiet(path string, status int) bool {
	if status >= 400 {
		return false
	}
	switch {
	// The SPA's hashed bundles, served from the root.
	case strings.HasPrefix(path, "/assets/"),
		// Character and set art, which lives under /api. One roster view
		// pulls dozens of these, and the key names which characters the
		// household looks at — a fact the route's own comment already
		// treats as worth keeping behind the session.
		strings.HasPrefix(path, "/api/art/"),
		path == "/favicon.ico":
		return true
	}
	return false
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
