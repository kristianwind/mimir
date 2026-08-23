package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kristianwind/mimir/internal/db"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v1.2.3", "v1.2.4", true},
		{"v1.2.3", "v1.3.0", true},
		{"v1.2.3", "v2.0.0", true},
		{"v1.2.3", "v1.2.3", false},
		{"v1.2.4", "v1.2.3", false},
		{"v1.10.0", "v1.9.0", false},
		{"v1.9.0", "v1.10.0", true},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestNewerRefusesToCompareUnversionedBuilds(t *testing.T) {
	// Offering to replace a build somebody made on purpose, because it does
	// not look like a tag, is not an upgrade.
	for _, a := range []string{"dev", "", "1.2.3", "v1.2", "v1.2.3-rc1", "v1.2.3+dirty"} {
		if Newer(a, "v9.9.9") {
			t.Errorf("Newer(%q, v9.9.9) = true; an unversioned build is not older than a release", a)
		}
	}
	if Newer("v1.0.0", "v2.0.0-rc1") {
		t.Error("a pre-release was treated as a newer version")
	}
}

// fakeRelease serves a GitHub-shaped release plus its assets.
func fakeRelease(t *testing.T, tag string, binary []byte, withChecksums bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewUnstartedServer(mux)

	sum := sha256Hex(binary)
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) { w.Write(binary) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", sum, AssetName())
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		rel := Release{
			TagName:     tag,
			Body:        "notes for " + tag,
			PublishedAt: time.Now(),
			Assets: []Asset{
				{Name: AssetName(), URL: "http://" + srv.Listener.Addr().String() + "/bin"},
			},
		}
		if withChecksums {
			rel.Assets = append(rel.Assets, Asset{
				Name: ChecksumsName, URL: "http://" + srv.Listener.Addr().String() + "/sums",
			})
		}
		json.NewEncoder(w).Encode(rel)
	})

	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func newUpdater(t *testing.T, version string, srv *httptest.Server) *Updater {
	t.Helper()
	conn, err := db.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	u := New(conn, version, "owner/repo", t.TempDir(), "http://127.0.0.1:1/api/healthz", nil)
	u.InContainer = func() bool { return false }
	u.HTTP = srv.Client()
	u.releaseURL = srv.URL
	return u
}

func TestCheckReportsAnAvailableUpdate(t *testing.T) {
	srv := fakeRelease(t, "v1.0.0", []byte("binary"), true)
	u := newUpdater(t, "v0.9.0", srv)

	exe := filepath.Join(t.TempDir(), "mimir")
	os.WriteFile(exe, []byte("old"), 0o755)
	u.Executable = func() (string, error) { return exe, nil }

	st := u.Check(context.Background(), true)
	if st.Error != "" {
		t.Fatalf("check failed: %s", st.Error)
	}
	if !st.UpdateAvailable || st.Latest != "v1.0.0" {
		t.Errorf("status = %+v", st)
	}
	if !st.CanApply {
		t.Errorf("cannot apply on a writable binary deployment: %q", st.Reason)
	}
	if st.Notes == "" {
		t.Error("release notes were dropped; an update nobody can read is one nobody should click")
	}
}

func TestCheckRefusesInAContainer(t *testing.T) {
	srv := fakeRelease(t, "v1.0.0", []byte("binary"), true)
	u := newUpdater(t, "v0.9.0", srv)
	u.InContainer = func() bool { return true }

	st := u.Check(context.Background(), true)
	if st.Mode != ModeContainer {
		t.Errorf("mode = %q, want container", st.Mode)
	}
	if st.CanApply {
		t.Error("offered to replace an image from inside the container")
	}
	if !strings.Contains(st.Reason, "rune") {
		t.Errorf("reason should name the way out, got %q", st.Reason)
	}
	if !st.UpdateAvailable {
		t.Error("a container should still be told an update exists")
	}
}

func TestCheckRefusesForDevBuilds(t *testing.T) {
	srv := fakeRelease(t, "v1.0.0", []byte("binary"), true)
	u := newUpdater(t, "dev", srv)

	st := u.Check(context.Background(), true)
	if st.Mode != ModeDev {
		t.Errorf("mode = %q, want dev", st.Mode)
	}
	if st.UpdateAvailable {
		t.Error("offered to replace a locally built binary with a release")
	}
}

func TestCheckRefusesWhenTheBinaryIsNotWritable(t *testing.T) {
	srv := fakeRelease(t, "v1.0.0", []byte("binary"), true)
	u := newUpdater(t, "v0.9.0", srv)

	// Root ignores the mode bits, and the updater's probe is a real write
	// rather than a permission calculation — so as root the binary genuinely
	// can be replaced and there is nothing here to refuse. Both CI runners
	// execute as uid 0, where this test was failing on every commit and
	// saying nothing about the code.
	if os.Geteuid() == 0 {
		t.Skip("running as root: a read-only directory does not stop uid 0")
	}

	dir := t.TempDir()
	exe := filepath.Join(dir, "mimir")
	os.WriteFile(exe, []byte("old"), 0o755)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skip("cannot make the directory read-only here")
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	u.Executable = func() (string, error) { return exe, nil }

	st := u.Check(context.Background(), true)
	if st.CanApply {
		t.Error("offered to replace a binary it cannot write")
	}
	if !strings.Contains(st.Reason, dir) {
		t.Errorf("reason should name the directory, got %q", st.Reason)
	}
}

func TestApplyRefusesWithoutChecksums(t *testing.T) {
	srv := fakeRelease(t, "v1.0.0", []byte("binary"), false)
	u := newUpdater(t, "v0.9.0", srv)
	exe := filepath.Join(t.TempDir(), "mimir")
	os.WriteFile(exe, []byte("old"), 0o755)
	u.Executable = func() (string, error) { return exe, nil }

	_, err := u.Apply(context.Background())
	if err == nil {
		t.Fatal("installed an unverifiable download")
	}
	if !strings.Contains(err.Error(), ChecksumsName) {
		t.Errorf("error should name the missing file, got %q", err)
	}
	if data, _ := os.ReadFile(exe); string(data) != "old" {
		t.Error("the running binary was replaced despite the failure")
	}
}

func TestApplyRefusesOnAChecksumMismatch(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewUnstartedServer(mux)
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("tampered")) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", strings.Repeat("0", 64), AssetName())
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Release{
			TagName: "v1.0.0",
			Assets: []Asset{
				{Name: AssetName(), URL: "http://" + srv.Listener.Addr().String() + "/bin"},
				{Name: ChecksumsName, URL: "http://" + srv.Listener.Addr().String() + "/sums"},
			},
		})
	})
	srv.Start()
	defer srv.Close()

	u := newUpdater(t, "v0.9.0", srv)
	exe := filepath.Join(t.TempDir(), "mimir")
	os.WriteFile(exe, []byte("old"), 0o755)
	u.Executable = func() (string, error) { return exe, nil }

	_, err := u.Apply(context.Background())
	if err == nil {
		t.Fatal("installed a binary whose checksum did not match")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("error = %q", err)
	}
	if data, _ := os.ReadFile(exe); string(data) != "old" {
		t.Error("a tampered binary reached the executable path")
	}
}

func TestRollbackNeedsABackup(t *testing.T) {
	srv := fakeRelease(t, "v1.0.0", []byte("binary"), true)
	u := newUpdater(t, "v0.9.0", srv)
	if _, err := u.Rollback(context.Background()); err == nil {
		t.Error("rolled back to nothing")
	}
}

func TestApplyReplacesTheBinaryAndKeepsABackup(t *testing.T) {
	newBinary := []byte("#!/bin/sh\necho new\n")
	srv := fakeRelease(t, "v1.0.0", newBinary, true)
	u := newUpdater(t, "v0.9.0", srv)

	exe := filepath.Join(t.TempDir(), "mimir")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	u.Executable = func() (string, error) { return exe, nil }
	// The real preflight starts a server; the swap is what this test is about.
	u.Preflight = func(context.Context, string) error { return nil }

	got, err := u.Apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "v1.0.0" {
		t.Errorf("applied %q", got)
	}

	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(newBinary) {
		t.Errorf("executable holds %q after the update", data)
	}
	// The mode has to survive the swap, or the next start fails with
	// permission denied and nobody knows why.
	info, _ := os.Stat(exe)
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("the replacement is not executable: %v", info.Mode())
	}

	backup := filepath.Join(u.DataDir, "updates", "mimir-v0.9.0.bak")
	if data, err := os.ReadFile(backup); err != nil || string(data) != "old" {
		t.Errorf("backup = %q, err = %v", data, err)
	}
}

func TestApplyLeavesTheBinaryAloneWhenPreflightFails(t *testing.T) {
	srv := fakeRelease(t, "v1.0.0", []byte("broken"), true)
	u := newUpdater(t, "v0.9.0", srv)

	exe := filepath.Join(t.TempDir(), "mimir")
	os.WriteFile(exe, []byte("old"), 0o755)
	u.Executable = func() (string, error) { return exe, nil }
	u.Preflight = func(context.Context, string) error {
		return fmt.Errorf("exec format error")
	}

	_, err := u.Apply(context.Background())
	if err == nil {
		t.Fatal("installed a binary that failed its preflight")
	}
	if !strings.Contains(err.Error(), "exec format error") {
		t.Errorf("error should carry the reason, got %q", err)
	}
	if data, _ := os.ReadFile(exe); string(data) != "old" {
		t.Error("the running binary was replaced anyway")
	}
}

func TestPreflightRejectsABinaryThatDoesNotServe(t *testing.T) {
	// The point of the preflight is that a checksum proves the bytes
	// arrived, not that they run. A candidate that exits immediately must
	// not pass.
	dir := t.TempDir()
	candidate := filepath.Join(dir, "candidate")
	if err := os.WriteFile(candidate, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	u := &Updater{Version: "v0.9.0", PreflightTimeout: 2 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := u.preflight(ctx, candidate); err == nil {
		t.Error("a candidate that never listens passed the preflight")
	}
}

func TestRollbackRestoresTheBackup(t *testing.T) {
	srv := fakeRelease(t, "v1.0.0", []byte("new"), true)
	u := newUpdater(t, "v0.9.0", srv)

	exe := filepath.Join(t.TempDir(), "mimir")
	os.WriteFile(exe, []byte("old"), 0o755)
	u.Executable = func() (string, error) { return exe, nil }
	u.Preflight = func(context.Context, string) error { return nil }

	if _, err := u.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	restored, err := u.Rollback(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if restored != "v0.9.0" {
		t.Errorf("restored %q, want v0.9.0", restored)
	}
	if data, _ := os.ReadFile(exe); string(data) != "old" {
		t.Errorf("executable holds %q after rollback", data)
	}
}

func TestWatchdogRestoresWhenTheUpdateNeverComesUp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "mimir")
	backup := filepath.Join(dir, "mimir.bak")
	os.WriteFile(target, []byte("broken"), 0o755)
	os.WriteFile(backup, []byte("known good"), 0o755)

	w := Watchdog{
		HealthURL: "http://127.0.0.1:1/api/healthz", // nothing listens there
		Restore:   target,
		Version:   "v1.0.0",
		LogPath:   filepath.Join(dir, "watchdog.log"),
		Deadline:  time.Second,
		Self:      func() (string, error) { return backup, nil },
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	restored, err := w.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !restored {
		t.Fatal("the watchdog left a broken update in place")
	}
	if data, _ := os.ReadFile(target); string(data) != "known good" {
		t.Errorf("target holds %q after rollback", data)
	}
	if log, _ := os.ReadFile(w.LogPath); !strings.Contains(string(log), "rolled back") {
		t.Errorf("the rollback was not recorded: %q", log)
	}
}

func TestWatchdogStandsDownWhenTheUpdateIsFine(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthy.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "mimir")
	backup := filepath.Join(dir, "mimir.bak")
	os.WriteFile(target, []byte("new"), 0o755)
	os.WriteFile(backup, []byte("old"), 0o755)

	w := Watchdog{
		HealthURL: healthy.URL,
		Restore:   target,
		LogPath:   filepath.Join(dir, "watchdog.log"),
		Deadline:  5 * time.Second,
		Self:      func() (string, error) { return backup, nil },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	restored, err := w.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if restored {
		t.Error("the watchdog rolled back a working update")
	}
	if data, _ := os.ReadFile(target); string(data) != "new" {
		t.Errorf("target holds %q; a healthy update was undone", data)
	}
}

func TestCheckDoesNotClaimThereAreNoReleasesWhenItCannotSee(t *testing.T) {
	// A private repository answers 404 to the updater's anonymous request
	// even when a release is published. Reporting that as "no releases yet"
	// sends the operator looking for a release they already made.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	u := newUpdater(t, "v0.9.0", srv)
	st := u.Check(context.Background(), true)

	if st.Error == "" {
		t.Fatal("a 404 was reported as success")
	}
	if !strings.Contains(st.Error, "privat") {
		t.Errorf("the error should allow for a private repository, got %q", st.Error)
	}
}
