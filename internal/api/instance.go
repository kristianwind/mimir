package api

import "net/http"

// What this instance is, to a browser that has not signed in.
//
// One flag, deliberately. It says which face to show before authentication:
// the sign-in form on every self-hosted Mimir, or the public pages on the one
// instance that is offered as a paid service. Nothing else belongs here —
// this endpoint answers to anyone on the internet, so it must never grow into
// a place that leaks what the instance holds.

func (s *Server) handleInstance(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"hosted": s.Config.Hosted,
		// Whether an account can be created without an administrator. On a
		// self-hosted instance this is off, and the public pages do not
		// exist to offer it anyway.
		"registration": s.Config.Hosted && s.Config.AllowRegistration,
	})
}
