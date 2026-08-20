package hoyolab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := New(Config{BaseURL: srv.URL, Salt: "testsalt", ClientVersion: "1.5.0"})
	c.Now = func() time.Time { return time.Unix(1700000000, 0) }
	c.Nonce = func() string { return "abcdef" }
	return c
}

func TestDSIsDeterministicForFixedInputs(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {})
	got := c.DS()
	parts := strings.Split(got, ",")
	if len(parts) != 3 {
		t.Fatalf("DS = %q, want three comma-separated parts", got)
	}
	if parts[0] != "1700000000" || parts[1] != "abcdef" {
		t.Errorf("DS prefix = %q,%q", parts[0], parts[1])
	}
	if len(parts[2]) != 32 {
		t.Errorf("DS hash is %d chars, want 32", len(parts[2]))
	}
	if c.DS() != got {
		t.Error("DS is not deterministic for fixed time and nonce")
	}
}

func TestFetchNotesSendsRequiredHeaders(t *testing.T) {
	var seen http.Header
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		json.NewEncoder(w).Encode(map[string]any{
			"retcode": 0,
			"data":    map[string]any{"current_resin": 137, "max_resin": 200, "resin_recovery_time": "480"},
		})
	})

	notes, err := c.FetchNotes(context.Background(), "700000001", "os_euro", "ltoken_v2=x")
	if err != nil {
		t.Fatal(err)
	}
	if notes.CurrentResin != 137 {
		t.Errorf("resin = %d, want 137", notes.CurrentResin)
	}
	for _, h := range []string{"DS", "Cookie", "X-Rpc-App_version", "X-Rpc-Client_type"} {
		if seen.Get(h) == "" {
			t.Errorf("missing header %s", h)
		}
	}
}

func TestFetchNotesTranslatesRetcodes(t *testing.T) {
	cases := map[int]string{
		10001: "log in to HoYoLAB again",
		10102: "enable Real-Time Notes",
		1034:  "open HoYoLAB in a browser",
	}
	for code, want := range cases {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"retcode": code, "message": "nope"})
		})
		_, err := c.FetchNotes(context.Background(), "700000001", "os_euro", "x")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("retcode %d gave %v, want a message containing %q", code, err, want)
		}
	}
}

func TestFetchNotesRefusesWithoutSalt(t *testing.T) {
	c := New(Config{})
	if _, err := c.FetchNotes(context.Background(), "700000001", "os_euro", "x"); err == nil {
		t.Error("expected a clear refusal rather than a request that always fails")
	}
}

func TestResinFullAt(t *testing.T) {
	now := time.Unix(1700000000, 0)
	n := Notes{ResinRecoveryTime: "480"}
	if got := n.ResinFullAt(now); !got.Equal(now.Add(8 * time.Minute)) {
		t.Errorf("ResinFullAt = %v", got)
	}
	if got := (Notes{ResinRecoveryTime: "0"}).ResinFullAt(now); !got.IsZero() {
		t.Errorf("capped resin should report the zero time, got %v", got)
	}
}

func TestServerFromUID(t *testing.T) {
	cases := map[string]string{"600000001": "os_usa", "700000001": "os_euro", "800000001": "os_asia", "900000001": "os_cht"}
	for uid, want := range cases {
		if got := Server(uid); got != want {
			t.Errorf("Server(%q) = %q, want %q", uid, got, want)
		}
	}
}
