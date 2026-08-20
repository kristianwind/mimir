package api

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/kristianwind/mimir/internal/beacon"
	"github.com/kristianwind/mimir/internal/db"
)

// The collector side.
//
// The same binary can be the receiver: turn it on for one instance and point
// the others at it. Counting is just distinct instance ids over a window.
//
// It is off by default and answers 404 when off, so an instance that is not a
// collector does not advertise an endpoint it will refuse anyway.
const (
	keyReceiverEnabled = "beacon.receiver_enabled"

	// maxInstances bounds what a public write endpoint can grow to. Yggdrasil
	// leaves this open; a POST that inserts a row for any id anyone invents
	// is an unbounded table behind an unauthenticated route. Existing
	// instances keep updating past the cap — only new ids are refused — so
	// hitting it degrades the count rather than the service.
	maxInstances = 50000

	maxInstanceIDLen = 64
	maxVersionLen    = 32
)

// instanceIDPattern is what the sender generates: 32 hex characters. Being
// strict costs nothing and keeps the table free of whatever a scanner posts.
var instanceIDPattern = regexp.MustCompile(`^[0-9a-f]{16,64}$`)

func (s *Server) receiverEnabled(ctx context.Context) bool {
	return db.Setting(ctx, s.DB, keyReceiverEnabled) == "1"
}

// handleBeaconPing records an incoming beacon.
func (s *Server) handleBeaconPing(w http.ResponseWriter, r *http.Request) {
	if !s.receiverEnabled(r.Context()) {
		http.NotFound(w, r)
		return
	}

	var p beacon.Payload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "ugyldig forespørgsel", "")
		return
	}
	p.InstanceID = strings.TrimSpace(p.InstanceID)
	p.Version = strings.TrimSpace(p.Version)

	if !instanceIDPattern.MatchString(p.InstanceID) || len(p.Version) > maxVersionLen {
		writeError(w, http.StatusBadRequest, "ugyldig payload", "")
		return
	}

	// Update first, insert only if the id is new — that way the cap never
	// blocks an instance that is already counted.
	res, err := s.DB.ExecContext(r.Context(), `
		UPDATE beacon_pings
		SET version = ?, last_seen = datetime('now'), ping_count = ping_count + 1
		WHERE instance_id = ?`, p.Version, p.InstanceID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var total int
		if err := s.DB.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM beacon_pings`).Scan(&total); err != nil {
			writeDomainError(w, err)
			return
		}
		if total >= maxInstances {
			// Say so rather than pretending: a sender that is being dropped
			// deserves to see it in its own error field.
			writeError(w, http.StatusServiceUnavailable,
				"collectoren har nået sin grænse for antal instanser", "")
			return
		}
		if _, err := s.DB.ExecContext(r.Context(),
			`INSERT INTO beacon_pings (instance_id, version) VALUES (?, ?)`,
			p.InstanceID, p.Version); err != nil {
			writeDomainError(w, err)
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ReceiverStats is what the collector knows.
type ReceiverStats struct {
	Enabled bool `json:"enabled"`
	// Endpoint is the address other instances should be pointed at.
	Endpoint  string         `json:"endpoint"`
	Total     int            `json:"total"`
	Active7d  int            `json:"active7d"`
	Active30d int            `json:"active30d"`
	Versions  []VersionCount `json:"versions"`
}

// VersionCount is one row of the active-30-day version breakdown.
type VersionCount struct {
	Version string `json:"version"`
	Count   int    `json:"count"`
}

func (s *Server) handleReceiverStats(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	ctx := r.Context()

	st := ReceiverStats{
		Enabled:  s.receiverEnabled(ctx),
		Endpoint: s.beaconEndpoint(),
		Versions: []VersionCount{},
	}
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM beacon_pings`).Scan(&st.Total)
	s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM beacon_pings WHERE last_seen >= datetime('now','-7 days')`).
		Scan(&st.Active7d)
	s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM beacon_pings WHERE last_seen >= datetime('now','-30 days')`).
		Scan(&st.Active30d)

	rows, err := s.DB.QueryContext(ctx, `
		SELECT COALESCE(NULLIF(version, ''), 'ukendt') v, COUNT(*) c
		FROM beacon_pings WHERE last_seen >= datetime('now','-30 days')
		GROUP BY v ORDER BY c DESC, v`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var vc VersionCount
			if rows.Scan(&vc.Version, &vc.Count) == nil {
				st.Versions = append(st.Versions, vc)
			}
		}
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleSetReceiver(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "ugyldig forespørgsel", "")
		return
	}
	v := "0"
	if body.Enabled {
		v = "1"
	}
	if err := db.SetSetting(r.Context(), s.DB, keyReceiverEnabled, v); err != nil {
		writeDomainError(w, err)
		return
	}
	s.audit(r, "system.beacon_receiver", boolWord(body.Enabled), nil)
	s.handleReceiverStats(w, r)
}

// beaconEndpoint is the address other instances point at.
//
// Config is always set in a running server, but a handler that panics on a
// nil field takes the request down with a 500 and an empty body — which is
// exactly how this was found. A missing base URL is a gap in what can be
// displayed, not a reason to fail the page.
func (s *Server) beaconEndpoint() string {
	if s.Config == nil || s.Config.BaseURL == "" {
		return "/api/beacon"
	}
	return strings.TrimRight(s.Config.BaseURL, "/") + "/api/beacon"
}
