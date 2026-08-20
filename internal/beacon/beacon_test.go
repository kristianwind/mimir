package beacon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kristianwind/mimir/internal/db"
)

func newBeacon(t *testing.T) (*Beacon, *[]Payload, func(string)) {
	t.Helper()
	conn, err := db.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	var got []Payload
	status := "ok"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p Payload
		_ = json.Unmarshal(body, &p)
		got = append(got, p)
		if status != "ok" {
			http.Error(w, status, http.StatusInternalServerError)
			return
		}
		// Nothing beyond the two fields may arrive.
		var raw map[string]any
		_ = json.Unmarshal(body, &raw)
		if len(raw) != 2 {
			t.Errorf("payload carries %d fields, want exactly 2: %s", len(raw), body)
		}
	}))
	t.Cleanup(srv.Close)

	b := New(conn, "v1.2.3", nil)
	clock := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	b.Now = func() time.Time { return clock }
	if err := b.SetURL(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}

	return b, &got, func(s string) { status = s }
}

func TestBeaconIsOffUntilChosen(t *testing.T) {
	b, got, _ := newBeacon(t)
	ctx := context.Background()

	if b.Enabled(ctx) {
		t.Error("the beacon is on before anyone said so")
	}
	if b.Chosen(ctx) {
		t.Error("an unanswered beacon reports itself as chosen")
	}

	b.Tick(ctx)
	if len(*got) != 0 {
		t.Errorf("pinged without consent: %+v", *got)
	}
}

func TestBeaconOffStaysOff(t *testing.T) {
	b, got, _ := newBeacon(t)
	ctx := context.Background()

	if err := b.SetEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	if !b.Chosen(ctx) {
		t.Error("an explicit no is still an answer and must be recorded")
	}

	// Simulate an upgrade: a fresh Beacon over the same database.
	upgraded := New(b.DB, "v2.0.0", nil)
	upgraded.Now = b.Now
	if upgraded.Enabled(ctx) {
		t.Error("an upgrade re-enabled a beacon the operator switched off")
	}
	upgraded.Tick(ctx)
	if len(*got) != 0 {
		t.Errorf("pinged after being switched off: %+v", *got)
	}
}

func TestBeaconSendsOnlyIdAndVersion(t *testing.T) {
	b, got, _ := newBeacon(t)
	ctx := context.Background()
	if err := b.SetEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}

	b.Tick(ctx)
	if len(*got) != 1 {
		t.Fatalf("got %d pings, want 1", len(*got))
	}
	if (*got)[0].Version != "v1.2.3" {
		t.Errorf("version = %q", (*got)[0].Version)
	}
	if len((*got)[0].InstanceID) != 32 {
		t.Errorf("instance id = %q, want 32 hex characters", (*got)[0].InstanceID)
	}
}

func TestBeaconPingsOnceADay(t *testing.T) {
	b, got, _ := newBeacon(t)
	ctx := context.Background()
	_ = b.SetEnabled(ctx, true)

	b.Tick(ctx)
	b.Tick(ctx)
	b.Tick(ctx)
	if len(*got) != 1 {
		t.Errorf("sent %d pings in one day, want 1", len(*got))
	}

	// Next day.
	day := b.Now().Add(24 * time.Hour)
	b.Now = func() time.Time { return day }
	b.Tick(ctx)
	if len(*got) != 2 {
		t.Errorf("sent %d pings across two days, want 2", len(*got))
	}
}

func TestBeaconRepingsWhenTheVersionChanges(t *testing.T) {
	b, got, _ := newBeacon(t)
	ctx := context.Background()
	_ = b.SetEnabled(ctx, true)
	b.Tick(ctx)

	// An update lands the same day. Without this, the collector would keep
	// seeing the old version for up to 24 hours — stale exactly when release
	// adoption is what you want to look at.
	b.Version = "v1.3.0"
	b.Tick(ctx)

	if len(*got) != 2 {
		t.Fatalf("got %d pings, want 2", len(*got))
	}
	if (*got)[1].Version != "v1.3.0" {
		t.Errorf("second ping reported %q", (*got)[1].Version)
	}
}

func TestBeaconInstanceIDIsStable(t *testing.T) {
	b, _, _ := newBeacon(t)
	ctx := context.Background()
	first := b.InstanceID(ctx)
	if first != b.InstanceID(ctx) {
		t.Error("the instance id changed between calls")
	}
	// A new Beacon over the same database keeps the id, or the collector
	// would count one install as many.
	if New(b.DB, "v1.2.3", nil).InstanceID(ctx) != first {
		t.Error("the instance id did not survive a restart")
	}
}

func TestBeaconRecordsWhyAPingFailed(t *testing.T) {
	b, _, fail := newBeacon(t)
	ctx := context.Background()
	_ = b.SetEnabled(ctx, true)
	fail("collector is down")

	b.Tick(ctx)
	st := b.Status(ctx)
	if st.LastError == "" {
		t.Fatal("a failed ping left no trace; it would retry forever in silence")
	}
	if !strings.Contains(st.LastError, "500") {
		t.Errorf("error should say what happened, got %q", st.LastError)
	}
	if st.LastDay != "" {
		t.Error("a failed ping was recorded as delivered")
	}
}

func TestStatusShowsTheLiteralPayload(t *testing.T) {
	b, _, _ := newBeacon(t)
	st := b.Status(context.Background())
	// The settings page promises the operator this is everything that gets
	// sent, so the status has to carry the payload itself rather than a
	// description of it.
	if st.Payload.Version != "v1.2.3" || st.Payload.InstanceID == "" {
		t.Errorf("status payload = %+v", st.Payload)
	}
}

func TestBeaconRefusesToEnableWithoutACollector(t *testing.T) {
	conn, err := db.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	b := New(conn, "v1.2.3", nil)
	ctx := context.Background()

	// There is no default collector on purpose: borrowing another project's
	// address made Mimir's pings land in that project's install count.
	if err := b.SetEnabled(ctx, true); err == nil {
		t.Fatal("enabled a beacon with nowhere to report to")
	}
	if b.Enabled(ctx) {
		t.Error("the beacon considers itself on without a collector")
	}
}

func TestBeaconStopsIfTheCollectorIsCleared(t *testing.T) {
	b, got, _ := newBeacon(t)
	ctx := context.Background()
	if err := b.SetEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}

	if err := b.SetURL(ctx, ""); err != nil {
		t.Fatal(err)
	}
	b.Tick(ctx)
	if len(*got) != 0 {
		t.Errorf("pinged after the collector was removed: %+v", *got)
	}
}

func TestSetURLRejectsNonsense(t *testing.T) {
	b, _, _ := newBeacon(t)
	ctx := context.Background()
	for _, bad := range []string{"not a url", "ftp://host/x", "//host/x", "beacon.example.com"} {
		if err := b.SetURL(ctx, bad); err == nil {
			t.Errorf("accepted %q as a collector address", bad)
		}
	}
	if err := b.SetURL(ctx, "https://beacon.example.com/api/beacon"); err != nil {
		t.Errorf("rejected a valid address: %v", err)
	}
}
