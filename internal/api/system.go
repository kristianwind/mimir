package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/kristianwind/mimir/internal/auth"
)

// handleSystemStatus reports the version, the update state and exactly what
// the beacon would send.
//
// The beacon's payload is in the response rather than described in the UI
// copy, so the page can show the literal thing that leaves the machine. A
// promise about telemetry is worth what the operator can verify.
func (s *Server) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"version": s.Version}
	if s.Updater != nil {
		out["update"] = s.Updater.Check(r.Context(), false)
	}
	if s.Beacon != nil {
		out["beacon"] = s.Beacon.Status(r.Context())
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCheckUpdate forces a fresh release check, bypassing the cache, so a
// "check now" button reflects a release published a minute ago.
func (s *Server) handleCheckUpdate(w http.ResponseWriter, r *http.Request) {
	if s.Updater == nil {
		writeError(w, http.StatusServiceUnavailable, "updates are not available", "")
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, s.Updater.Check(r.Context(), true))
}

// handleApplyUpdate installs the latest release and asks the process to exit
// so the supervisor starts the new binary.
func (s *Server) handleApplyUpdate(w http.ResponseWriter, r *http.Request) {
	if s.Updater == nil {
		writeError(w, http.StatusServiceUnavailable, "updates are not available", "")
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}

	// Downloading, verifying and running a candidate takes tens of seconds,
	// so it gets its own budget rather than the request's.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Minute)
	defer cancel()

	target, err := s.Updater.Apply(ctx)
	if err != nil {
		// Nothing was replaced — Apply commits only after the candidate has
		// run — so this is a plain failure, not a half-applied update.
		writeError(w, http.StatusBadRequest, err.Error(), "")
		return
	}

	s.audit(r, "system.update", target, map[string]any{"from": s.Version})
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "updated",
		"from":   s.Version,
		"to":     target,
		"note": "Mimir genstarter nu. Kommer den ikke op inden for halvandet minut, " +
			"ruller vagthunden automatisk tilbage til " + s.Version + ".",
	})

	// Exit after the response has been written, so the browser sees the
	// answer rather than a dropped connection.
	if s.Shutdown != nil {
		go func() {
			time.Sleep(500 * time.Millisecond)
			s.Shutdown()
		}()
	}
}

// handleRollback restores the binary the last update replaced.
func (s *Server) handleRollback(w http.ResponseWriter, r *http.Request) {
	if s.Updater == nil {
		writeError(w, http.StatusServiceUnavailable, "updates are not available", "")
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}

	restored, err := s.Updater.Rollback(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "")
		return
	}
	s.audit(r, "system.rollback", restored, nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "rolled back",
		"restored": restored,
		"note":     "Restart Mimir to run the restored version.",
	})

	if s.Shutdown != nil {
		go func() {
			time.Sleep(500 * time.Millisecond)
			s.Shutdown()
		}()
	}
}

// handleSetBeacon records the operator's choice about the daily ping.
func (s *Server) handleSetBeacon(w http.ResponseWriter, r *http.Request) {
	if s.Beacon == nil {
		writeError(w, http.StatusServiceUnavailable, "the beacon is not available", "")
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}

	var body struct {
		Enabled bool    `json:"enabled"`
		URL     *string `json:"url,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request", "")
		return
	}
	if body.URL != nil {
		if err := s.Beacon.SetURL(r.Context(), *body.URL); err != nil {
			writeError(w, http.StatusBadRequest, err.Error(), "")
			return
		}
	}
	if err := s.Beacon.SetEnabled(r.Context(), body.Enabled); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(),
			"Set a collector address, and the beacon can be switched on.")
		return
	}

	// Both directions are audited. Turning telemetry on is the interesting
	// one, but turning it off is the decision that has to survive upgrades,
	// and a record of when it was made is part of that.
	s.audit(r, "system.beacon", boolWord(body.Enabled), nil)

	if body.Enabled {
		// Ping straight away so the operator sees it work — or sees why it
		// did not — instead of wondering for a day.
		go s.Beacon.Tick(context.WithoutCancel(r.Context()))
	}
	writeJSON(w, http.StatusOK, s.Beacon.Status(r.Context()))
}

func boolWord(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}

// requireAdmin gates the operations that change the instance rather than an
// account's data. It reports whether the caller may proceed.
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	u, ok := auth.FromContext(r.Context())
	if !ok || !u.IsAdmin() {
		writeError(w, http.StatusForbidden, "requires administrator rights", "")
		return false
	}
	return true
}

// audit records an instance-level action.
func (s *Server) audit(r *http.Request, action, resource string, detail map[string]any) {
	u, _ := auth.FromContext(r.Context())
	var payload string
	if detail != nil {
		if raw, err := json.Marshal(detail); err == nil {
			payload = string(raw)
		}
	}
	var userID any
	if u.ID != 0 {
		userID = u.ID
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO audit_log (user_id, action, resource, detail) VALUES (?, ?, ?, ?)`,
		userID, action, resource, payload); err != nil && s.Log != nil {
		s.Log.Warn("could not write audit log", "action", action, "error", err)
	}
}
