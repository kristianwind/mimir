package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/db"
)

func TestReceiverIsOffAndUnadvertised(t *testing.T) {
	s := emptyServer(t)

	// 404, not 403: an instance that is not a collector should not announce
	// an endpoint it will only refuse.
	w := post(s, "/api/beacon", `{"instance_id":"aabbccddeeff0011","version":"v1.0.0"}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("a disabled receiver answered %d, want 404", w.Code)
	}
}

func enableReceiver(t *testing.T, s *Server) {
	t.Helper()
	if err := db.SetSetting(t.Context(), s.DB, keyReceiverEnabled, "1"); err != nil {
		t.Fatal(err)
	}
}

func TestReceiverRecordsAPing(t *testing.T) {
	s := emptyServer(t)
	enableReceiver(t, s)

	w := post(s, "/api/beacon", `{"instance_id":"aabbccddeeff0011","version":"v1.0.0"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("ping gave %d: %s", w.Code, w.Body)
	}

	var id, version string
	var count int
	if err := s.DB.QueryRow(
		`SELECT instance_id, version, ping_count FROM beacon_pings`).
		Scan(&id, &version, &count); err != nil {
		t.Fatal(err)
	}
	if id != "aabbccddeeff0011" || version != "v1.0.0" || count != 1 {
		t.Errorf("stored %q %q %d", id, version, count)
	}
}

func TestRepeatPingsCountOnceAsAnInstall(t *testing.T) {
	s := emptyServer(t)
	enableReceiver(t, s)

	for i := 0; i < 3; i++ {
		post(s, "/api/beacon", `{"instance_id":"aabbccddeeff0011","version":"v1.1.0"}`)
	}
	var instances, count int
	s.DB.QueryRow(`SELECT COUNT(*), MAX(ping_count) FROM beacon_pings`).Scan(&instances, &count)
	if instances != 1 {
		t.Errorf("three pings from one instance created %d rows", instances)
	}
	if count != 3 {
		t.Errorf("ping_count = %d, want 3", count)
	}
}

func TestReceiverStoresNothingButIdAndVersion(t *testing.T) {
	s := emptyServer(t)
	enableReceiver(t, s)

	// The sending side promises its operator that only two fields leave the
	// machine. Recording more here would make that promise false from the
	// other end, so the table has no column to put it in — this checks the
	// shape has not quietly grown one.
	rows, err := s.DB.Query(`SELECT name FROM pragma_table_info('beacon_pings')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		rows.Scan(&c)
		cols = append(cols, c)
	}
	want := map[string]bool{
		"instance_id": true, "version": true,
		"first_seen": true, "last_seen": true, "ping_count": true,
	}
	for _, c := range cols {
		if !want[c] {
			t.Errorf("beacon_pings has an unexpected column %q — the receiver stores more than it promised", c)
		}
	}
}

func TestReceiverRejectsRubbish(t *testing.T) {
	s := emptyServer(t)
	enableReceiver(t, s)

	for _, body := range []string{
		`{"instance_id":"","version":"v1"}`,
		`{"instance_id":"ikke-hex","version":"v1"}`,
		`{"instance_id":"aabb","version":"v1"}`,
		`{"instance_id":"aabbccddeeff0011","version":"` + strings.Repeat("x", 40) + `"}`,
		`ikke json`,
	} {
		w := post(s, "/api/beacon", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("accepted %q with %d", body, w.Code)
		}
	}
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM beacon_pings`).Scan(&n)
	if n != 0 {
		t.Errorf("%d rubbish rows landed in the table", n)
	}
}

func TestKnownInstancesKeepReportingPastTheCap(t *testing.T) {
	// A public write endpoint that inserts a row for any invented id is an
	// unbounded table. The cap refuses new ids; instances already counted
	// must keep updating, or the collector would start dropping the very
	// installs it exists to count.
	s := emptyServer(t)
	enableReceiver(t, s)

	known := "aabbccddeeff0011"
	post(s, "/api/beacon", fmt.Sprintf(`{"instance_id":%q,"version":"v1.0.0"}`, known))

	for i := 0; i < maxInstances+1; i++ {
		s.DB.Exec(`INSERT OR IGNORE INTO beacon_pings (instance_id, version) VALUES (?, 'v1')`,
			fmt.Sprintf("%064x", i+1000))
	}

	if w := post(s, "/api/beacon", `{"instance_id":"ffffffffffffffff","version":"v1.0.0"}`); w.Code != http.StatusServiceUnavailable {
		t.Errorf("a new instance past the cap gave %d, want 503", w.Code)
	}
	if w := post(s, "/api/beacon", fmt.Sprintf(`{"instance_id":%q,"version":"v2.0.0"}`, known)); w.Code != http.StatusOK {
		t.Errorf("a known instance was dropped past the cap: %d", w.Code)
	}
	var v string
	s.DB.QueryRow(`SELECT version FROM beacon_pings WHERE instance_id = ?`, known).Scan(&v)
	if v != "v2.0.0" {
		t.Errorf("the known instance's version did not update: %q", v)
	}
}

func TestReceiverStatsCountDistinctInstalls(t *testing.T) {
	s, do := newServer(t)
	enableReceiver(t, s)

	for i := 0; i < 3; i++ {
		post(s, "/api/beacon",
			fmt.Sprintf(`{"instance_id":"%016x","version":"v1.%d.0"}`, i+1, i))
	}
	post(s, "/api/beacon", `{"instance_id":"0000000000000001","version":"v1.0.0"}`)

	w := do("chef", "GET", "/api/system/beacon/receiver", "")
	if w.Code != http.StatusOK {
		t.Fatalf("stats gave %d: %s", w.Code, w.Body)
	}
	var st ReceiverStats
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Total != 3 {
		t.Errorf("total = %d, want 3 distinct installs from 4 pings", st.Total)
	}
	if len(st.Versions) != 3 {
		t.Errorf("version breakdown has %d rows, want 3: %+v", len(st.Versions), st.Versions)
	}
}

func TestOnlyAdminsSeeTheCollector(t *testing.T) {
	_, do := newServer(t)
	for _, c := range []struct{ method, body string }{
		{"GET", ""}, {"PUT", `{"enabled":true}`},
	} {
		w := do("menig", c.method, "/api/system/beacon/receiver", c.body)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s as a plain user gave %d, want 403", c.method, w.Code)
		}
	}
}

func TestEnablingTheReceiverOpensTheEndpoint(t *testing.T) {
	s, do := newServer(t)

	if w := post(s, "/api/beacon", `{"instance_id":"aabbccddeeff0011","version":"v1"}`); w.Code != http.StatusNotFound {
		t.Fatalf("closed receiver gave %d", w.Code)
	}
	if w := do("chef", "PUT", "/api/system/beacon/receiver", `{"enabled":true}`); w.Code != http.StatusOK {
		t.Fatalf("enable gave %d: %s", w.Code, w.Body)
	}
	if w := post(s, "/api/beacon", `{"instance_id":"aabbccddeeff0011","version":"v1"}`); w.Code != http.StatusOK {
		t.Errorf("open receiver gave %d", w.Code)
	}
}

func TestOnlyOneMineAtATime(t *testing.T) {
	s, do := newServer(t)

	// Two concurrent mines would fight over the same cache directory and
	// produce a snapshot neither of them can vouch for. The job is marked
	// running directly rather than by starting a real one, so the test
	// exercises the lock without reaching the network.
	s.Mine = &MineJob{running: true}

	w := do("chef", "POST", "/api/system/gamedata/mine", `{"version":"7.0.0"}`)
	if w.Code != http.StatusConflict {
		t.Errorf("a start while one is running gave %d, want 409: %s", w.Code, w.Body)
	}
}

func TestMineNeedsAVersion(t *testing.T) {
	_, do := newServer(t)
	w := do("chef", "POST", "/api/system/gamedata/mine", `{}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("a mine with no version gave %d, want 400", w.Code)
	}
}

func TestOnlyAdminsMine(t *testing.T) {
	_, do := newServer(t)
	for _, c := range []struct{ method, body string }{
		{"GET", ""}, {"POST", `{"version":"7.0.0"}`},
	} {
		w := do("menig", c.method, "/api/system/gamedata/mine", c.body)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s as a plain user gave %d, want 403", c.method, w.Code)
		}
	}
}
