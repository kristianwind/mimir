package selfupdate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kristianwind/mimir/internal/db"
)

// Settings keys for the updater's bookkeeping.
const (
	keyBackupPath    = "update.backup_path"
	keyBackupVersion = "update.backup_version"
	keyAppliedAt     = "update.applied_at"
	keyAppliedTo     = "update.applied_to"
)

// Mode is how this instance is deployed, which decides what an update means.
type Mode string

const (
	// ModeBinary is a binary on a host under a supervisor that restarts it.
	ModeBinary Mode = "binary"
	// ModeContainer is a container, whose image is immutable from inside.
	ModeContainer Mode = "container"
	// ModeDev is an unreleased build.
	ModeDev Mode = "dev"
)

// Updater checks for and applies releases.
type Updater struct {
	DB      *sql.DB
	Version string
	Repo    string
	DataDir string
	// HealthURL is what the watchdog probes after a swap.
	HealthURL string
	HTTP      *http.Client
	Log       *slog.Logger

	// Executable is injectable for tests; defaults to os.Executable.
	Executable func() (string, error)
	// InContainer is injectable for tests.
	InContainer func() bool
	// Preflight proves a candidate runs here. Injectable for tests; the
	// default actually starts it and waits for a health check.
	Preflight func(ctx context.Context, candidate string) error
	// PreflightTimeout is how long the candidate gets to answer. Zero uses
	// the default.
	PreflightTimeout time.Duration

	// releaseURL overrides the GitHub API base, for tests.
	releaseURL string

	mu     sync.Mutex
	cached Release
	at     time.Time
}

// New returns an updater.
func New(conn *sql.DB, version, repo, dataDir, healthURL string, log *slog.Logger) *Updater {
	return &Updater{
		DB:          conn,
		Version:     version,
		Repo:        repo,
		DataDir:     dataDir,
		HealthURL:   healthURL,
		HTTP:        &http.Client{Timeout: 2 * time.Minute},
		Log:         log,
		Executable:  os.Executable,
		InContainer: inContainer,
	}
}

func (u *Updater) client() *http.Client {
	if u.HTTP != nil {
		return u.HTTP
	}
	return http.DefaultClient
}

// inContainer reports whether this process is running inside a container.
func inContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if v := os.Getenv("MIMIR_DEPLOY"); strings.EqualFold(v, "container") {
		return true
	}
	// The cgroup path names the runtime on Linux; absent elsewhere.
	if raw, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		s := string(raw)
		if strings.Contains(s, "docker") || strings.Contains(s, "containerd") ||
			strings.Contains(s, "kubepods") {
			return true
		}
	}
	return false
}

// Mode reports how this instance is deployed.
func (u *Updater) Mode() Mode {
	if u.InContainer != nil && u.InContainer() {
		return ModeContainer
	}
	if _, ok := parseVersion(u.Version); !ok {
		return ModeDev
	}
	return ModeBinary
}

// Status is what the settings page shows.
type Status struct {
	Version         string `json:"version"`
	Latest          string `json:"latest,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Mode            Mode   `json:"mode"`
	// CanApply says whether Mimir can install the update itself.
	CanApply bool `json:"canApply"`
	// Reason explains a false CanApply, in words that name the next step.
	Reason string `json:"reason,omitempty"`
	Notes  string `json:"notes,omitempty"`
	// PublishedAt is a pointer so "no release" serialises as absent rather
	// than as the year 1, which a UI would faithfully render.
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	// Backup is the version an update could be rolled back to.
	Backup string `json:"backup,omitempty"`
	// AppliedAt records the last successful update.
	AppliedAt string `json:"appliedAt,omitempty"`
	AppliedTo string `json:"appliedTo,omitempty"`
	// Error carries a failed release check without failing the whole page.
	Error string `json:"error,omitempty"`
}

// setReason records why an update cannot be applied.
func (s *Status) setReason(format string, args ...any) {
	s.Reason = fmt.Sprintf(format, args...)
}

// Check returns the current update state.
func (u *Updater) Check(ctx context.Context, force bool) Status {
	st := Status{
		Version:   u.Version,
		Mode:      u.Mode(),
		Backup:    db.Setting(ctx, u.DB, keyBackupVersion),
		AppliedAt: db.Setting(ctx, u.DB, keyAppliedAt),
		AppliedTo: db.Setting(ctx, u.DB, keyAppliedTo),
	}

	rel, err := u.release(ctx, force)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Latest = rel.TagName
	st.Notes = rel.Body
	if !rel.PublishedAt.IsZero() {
		published := rel.PublishedAt
		st.PublishedAt = &published
	}
	st.UpdateAvailable = Newer(u.Version, rel.TagName)

	if !st.UpdateAvailable {
		return st
	}

	switch st.Mode {
	case ModeContainer:
		st.setReason("Mimir runs in a container, and a container cannot replace its own image. " +
			"Update the rune in Yggdrasil: that pulls the new image and recreates the container.")
	case ModeDev:
		st.setReason("This binary was built locally (%s), not from a release. "+
			"Updating would throw away a build somebody made on purpose.", u.Version)
	default:
		if _, ok := rel.Find(AssetName()); !ok {
			st.setReason("The release has no binary for %s.", AssetName())
			return st
		}
		if err := u.writable(); err != nil {
			st.setReason("%s", err.Error())
			return st
		}
		st.CanApply = true
	}
	return st
}

// release fetches the newest release, cached for six hours unless forced.
func (u *Updater) release(ctx context.Context, force bool) (Release, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !force && u.cached.TagName != "" && time.Since(u.at) < 6*time.Hour {
		return u.cached, nil
	}
	rel, err := u.latest(ctx)
	if err != nil {
		if u.cached.TagName != "" {
			return u.cached, nil
		}
		return Release{}, err
	}
	u.cached, u.at = rel, time.Now()
	return rel, nil
}

// writable checks that the running binary can actually be replaced, before
// anything is downloaded.
func (u *Updater) writable() error {
	path, err := u.Executable()
	if err != nil {
		return fmt.Errorf("could not locate the running binary: %w", err)
	}
	dir := filepath.Dir(path)
	probe, err := os.CreateTemp(dir, ".mimir-write-probe-*")
	if err != nil {
		return fmt.Errorf("cannot write in %s, so the binary cannot be replaced from here", dir)
	}
	name := probe.Name()
	probe.Close()
	_ = os.Remove(name)
	return nil
}

// Apply downloads, verifies, proves and installs the latest release.
//
// It returns the version installed. The caller is expected to exit so the
// supervisor starts the new binary; a watchdog is already running by then.
func (u *Updater) Apply(ctx context.Context) (string, error) {
	st := u.Check(ctx, true)
	if st.Error != "" {
		return "", errors.New(st.Error)
	}
	if !st.UpdateAvailable {
		return "", fmt.Errorf("selfupdate: %s is already the newest version", u.Version)
	}
	if !st.CanApply {
		return "", errors.New(st.Reason)
	}

	rel, err := u.release(ctx, false)
	if err != nil {
		return "", err
	}
	asset, ok := rel.Find(AssetName())
	if !ok {
		return "", fmt.Errorf("selfupdate: the release has no %s", AssetName())
	}

	dir := filepath.Join(u.DataDir, "updates")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}

	candidate := filepath.Join(dir, "mimir-"+rel.TagName)
	if err := u.download(ctx, asset.URL, candidate); err != nil {
		return "", err
	}

	// Verify before running: an executable that is about to be launched is
	// exactly the wrong thing to take on trust from the network.
	if err := u.verify(ctx, rel, candidate, asset.Name); err != nil {
		_ = os.Remove(candidate)
		return "", err
	}
	if err := os.Chmod(candidate, 0o755); err != nil {
		return "", err
	}

	// Prove it runs here. A checksum says the bytes arrived intact; it says
	// nothing about whether they execute on this kernel and libc.
	preflight := u.Preflight
	if preflight == nil {
		preflight = u.preflight
	}
	if err := preflight(ctx, candidate); err != nil {
		_ = os.Remove(candidate)
		return "", fmt.Errorf("selfupdate: the new binary failed its start-up check, so nothing was replaced: %w", err)
	}

	current, err := u.Executable()
	if err != nil {
		return "", err
	}
	backup := filepath.Join(dir, "mimir-"+u.Version+".bak")
	if err := copyFile(current, backup, 0o755); err != nil {
		return "", fmt.Errorf("selfupdate: could not back up the current binary: %w", err)
	}

	// The watchdog is spawned from the backup — the binary already known to
	// work on this machine — so a broken update cannot also break the thing
	// that would undo it.
	if err := u.startWatchdog(backup, current, rel.TagName); err != nil {
		if u.Log != nil {
			u.Log.Warn("could not start the update watchdog; rollback will have to be manual", "error", err)
		}
	}

	if err := replaceFile(candidate, current); err != nil {
		return "", fmt.Errorf("selfupdate: could not replace the binary: %w", err)
	}

	_ = db.SetSetting(ctx, u.DB, keyBackupPath, backup)
	_ = db.SetSetting(ctx, u.DB, keyBackupVersion, u.Version)
	_ = db.SetSetting(ctx, u.DB, keyAppliedAt, time.Now().UTC().Format(time.RFC3339))
	_ = db.SetSetting(ctx, u.DB, keyAppliedTo, rel.TagName)

	return rel.TagName, nil
}

// Rollback restores the backup taken by the last update.
func (u *Updater) Rollback(ctx context.Context) (string, error) {
	backup := db.Setting(ctx, u.DB, keyBackupPath)
	version := db.Setting(ctx, u.DB, keyBackupVersion)
	if backup == "" {
		return "", errors.New("selfupdate: there is no backup to roll back to")
	}
	if _, err := os.Stat(backup); err != nil {
		return "", fmt.Errorf("selfupdate: the backup %s no longer exists", backup)
	}
	current, err := u.Executable()
	if err != nil {
		return "", err
	}
	if err := copyOver(backup, current); err != nil {
		return "", err
	}
	return version, nil
}

func (u *Updater) download(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "mimir/"+u.Version)
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := u.client().Do(req)
	if err != nil {
		return fmt.Errorf("selfupdate: hentning mislykkedes: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("selfupdate: hentning gav %s", strings.TrimSpace(resp.Status))
	}

	tmp := dest + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// Rename last: an interrupted download must not leave something that
	// looks like a finished binary.
	return os.Rename(tmp, dest)
}

// verify checks the download against the release's checksums file.
func (u *Updater) verify(ctx context.Context, rel Release, path, assetName string) error {
	sums, ok := rel.Find(ChecksumsName)
	if !ok {
		return fmt.Errorf("selfupdate: the release has no %s, so the download cannot be verified", ChecksumsName)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sums.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "mimir/"+u.Version)
	resp, err := u.client().Do(req)
	if err != nil {
		return fmt.Errorf("selfupdate: could not download %s: %w", ChecksumsName, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}

	want := ""
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == assetName {
			want = strings.ToLower(fields[0])
		}
	}
	if want == "" {
		return fmt.Errorf("selfupdate: %s does not mention %s", ChecksumsName, assetName)
	}

	got, err := hashFile(path)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("selfupdate: checksum does not match (got %s, expected %s)", got[:16], want[:16])
	}
	return nil
}

// preflight runs the candidate and waits for it to serve a health check.
//
// It gets its own empty data directory, so a binary that turns out to migrate
// the schema in a way this version cannot read never touches the live
// database. What it proves is narrow and worth having: this file executes on
// this machine and answers HTTP.
func (u *Updater) preflight(ctx context.Context, candidate string) error {
	port, err := freePort()
	if err != nil {
		return err
	}
	sandbox, err := os.MkdirTemp("", "mimir-preflight-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(sandbox)

	timeout := u.PreflightTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout+15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, candidate, "serve")
	cmd.Env = append(os.Environ(),
		"MIMIR_DATA_DIR="+sandbox,
		fmt.Sprintf("MIMIR_ADDR=127.0.0.1:%d", port),
	)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start: %w", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d/api/healthz", port)
	if err := waitHealthy(runCtx, url, timeout); err != nil {
		return err
	}
	return nil
}

// startWatchdog launches the backup binary to undo a failed update.
func (u *Updater) startWatchdog(backup, target, version string) error {
	if u.HealthURL == "" {
		return errors.New("no health URL configured")
	}
	cmd := exec.Command(backup, "watchdog",
		"-url", u.HealthURL,
		"-restore", target,
		"-version", version,
		"-log", filepath.Join(u.DataDir, "updates", "watchdog.log"),
	)
	cmd.Stdout = nil
	cmd.Stderr = nil
	detach(cmd)
	return cmd.Start()
}
