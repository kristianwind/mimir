package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The collector must not count itself.
//
// An instance can be both sender and receiver, and mimir.guide is exactly
// that. Counting its own ping made "installations: 1" mean "nobody else has
// installed this" while reading as "one person out there is running it" —
// opposite answers to the only question the beacon exists to ask. Measured on
// production: one row, and its instance_id was the instance's own.
func TestTheCollectorDoesNotCountItself(t *testing.T) {
	s, do := newServer(t)

	const self = "c5b20da45882a0f9a32758431662bc0e"
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO settings (key, value) VALUES ('beacon.instance_id', ?)`, []any{self}},
		{`INSERT INTO settings (key, value) VALUES ('beacon.receiver_enabled', '1')`, nil},
		{`INSERT INTO beacon_pings (instance_id, version, first_seen, last_seen)
		    VALUES (?, 'v0.5.2', datetime('now'), datetime('now'))`, []any{self}},
	} {
		if _, err := s.DB.Exec(q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}

	w := do("boss", "GET", "/api/system/beacon/receiver", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}
	var got struct {
		Total     int `json:"total"`
		Active7d  int `json:"active7d"`
		Active30d int `json:"active30d"`
		Versions  []struct {
			Version string `json:"version"`
			Count   int    `json:"count"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 0 || got.Active7d != 0 || got.Active30d != 0 {
		t.Errorf("the collector counted its own ping: total=%d 7d=%d 30d=%d",
			got.Total, got.Active7d, got.Active30d)
	}
	if len(got.Versions) != 0 {
		t.Errorf("its own version appeared in the breakdown: %+v", got.Versions)
	}
}

// And it must still count everybody else, or the fix would be a way of
// reporting zero for ever.
func TestTheCollectorStillCountsOthers(t *testing.T) {
	s, do := newServer(t)
	for _, q := range [][]any{
		{`INSERT INTO settings (key, value) VALUES ('beacon.instance_id', 'mine')`},
		{`INSERT INTO settings (key, value) VALUES ('beacon.receiver_enabled', '1')`},
		{`INSERT INTO beacon_pings (instance_id, version, first_seen, last_seen)
		    VALUES ('mine', 'v0.5.2', datetime('now'), datetime('now'))`},
		{`INSERT INTO beacon_pings (instance_id, version, first_seen, last_seen)
		    VALUES ('somebody-else', 'v0.5.0', datetime('now'), datetime('now'))`},
	} {
		if _, err := s.DB.Exec(q[0].(string)); err != nil {
			t.Fatal(err)
		}
	}

	w := do("boss", "GET", "/api/system/beacon/receiver", "")
	var got struct {
		Total    int `json:"total"`
		Versions []struct {
			Version string `json:"version"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 {
		t.Fatalf("total = %d, want 1 — the other instance", got.Total)
	}
	for _, v := range got.Versions {
		if v.Version == "v0.5.2" {
			t.Error("the collector's own version is still in the breakdown")
		}
	}
}
