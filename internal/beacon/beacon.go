// Package beacon sends one anonymous daily ping so the project can see how
// many installs exist and which version they run.
//
// It sends exactly two fields: a random instance id and the version. No UIDs,
// no account data, no inventory, no IP recorded by the receiver, no request
// metadata. That list is short on purpose — the settings page shows the
// literal payload, and a beacon that sends anything the user was not told
// about is how a self-hosted project loses the trust it runs on.
//
// It is on unless the operator turns it off, and off stays off: an upgrade
// must never re-enable it. The same shape as Yggdrasil's — default on, behind
// a disclosure the administrator meets on first sign-in with a one-click
// decline, which is the only thing that makes a default-on ping honest. A
// beacon nobody is told about is telemetry no matter how little it sends.
package beacon

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kristianwind/mimir/internal/db"
)

// Settings keys. They are namespaced so a future setting cannot collide with
// the one flag an operator may have deliberately switched off.
const (
	keyEnabled     = "beacon.enabled"
	keyInstanceID  = "beacon.instance_id"
	keyURL         = "beacon.url"
	keyLastDay     = "beacon.last_day"
	keyLastVersion = "beacon.last_version"
	keyLastError   = "beacon.last_error"
	keyLastErrorAt = "beacon.last_error_at"
)

// DefaultCollector is where an unconfigured instance reports.
//
// It is Mimir's own, and that is the whole point. Copying another project's
// address seemed harmless and was not: pointed at Yggdrasil's, Mimir's pings
// landed in Yggdrasil's install count as phantom panels running a version
// that does not exist. A beacon has to report somewhere that knows what it is
// counting.
//
// An operator can still change it, and pointing it at their own collector is
// a supported thing to do rather than a workaround.
const DefaultCollector = "https://mimir.guide/api/beacon"

// Interval is how often the loop wakes. It acts at most once per day, but
// wakes more often so an instance that is not up around the clock still pings
// roughly daily.
const Interval = 30 * time.Minute

// Payload is the entire contents of a ping. Keep it this small — the UI
// promises the operator that nothing else is sent, and that promise is only
// as good as this struct.
type Payload struct {
	InstanceID string `json:"instance_id"`
	Version    string `json:"version"`
}

// Beacon pings on a schedule.
type Beacon struct {
	DB      *sql.DB
	Version string
	HTTP    *http.Client
	Log     *slog.Logger
	// Now is injectable for tests.
	Now func() time.Time
}

// New returns a beacon.
func New(conn *sql.DB, version string, log *slog.Logger) *Beacon {
	return &Beacon{
		DB:      conn,
		Version: version,
		HTTP:    &http.Client{Timeout: 10 * time.Second},
		Log:     log,
		Now:     time.Now,
	}
}

// Enabled reports whether the beacon may ping.
//
// Unset means off. Only an explicit "1" turns it on, so a new setting, a
// schema change or an upgrade can never flip it by accident.
func (b *Beacon) Enabled(ctx context.Context) bool {
	// An unreleased build is somebody's working copy, not an install worth
	// counting. Without this, every `go run` on a laptop registers as a new
	// installation running a version that was never published — which is the
	// phantom-install problem the default collector exists to avoid, arriving
	// from the other direction. Found by doing it: a local instance started
	// for a screenshot pinged production and was counted.
	if b.Version == "" || b.Version == "dev" {
		return false
	}
	// Unanswered means on. Answered means whatever was answered — so an
	// operator who turned it off stays off through every upgrade, which is
	// the promise that matters more than the default.
	if !b.Chosen(ctx) {
		return b.URL(ctx) != ""
	}
	return db.Setting(ctx, b.DB, keyEnabled) == "1" && b.URL(ctx) != ""
}

// SetEnabled records the operator's choice. Both directions are explicit, so
// "off" is a stored decision rather than an absence of one.
//
// Turning it on requires a collector, because a beacon that does not know
// where it reports either goes nowhere or goes somewhere it was not meant to.
func (b *Beacon) SetEnabled(ctx context.Context, on bool) error {
	if on && b.URL(ctx) == "" {
		return errors.New("beacon: set a collector address first — there is no default, " +
			"and a ping with no recipient ends up either nowhere or somewhere wrong")
	}
	v := "0"
	if on {
		v = "1"
	}
	return db.SetSetting(ctx, b.DB, keyEnabled, v)
}

// Chosen reports whether the operator has answered at all, so the UI knows
// whether to ask.
func (b *Beacon) Chosen(ctx context.Context) bool {
	return db.Setting(ctx, b.DB, keyEnabled) != ""
}

// InstanceID returns the stable anonymous id, generating one on first use.
// It identifies nothing; it only lets the collector count repeat pings once.
func (b *Beacon) InstanceID(ctx context.Context) string {
	if id := db.Setting(ctx, b.DB, keyInstanceID); id != "" {
		return id
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return ""
	}
	id := hex.EncodeToString(raw)
	_ = db.SetSetting(ctx, b.DB, keyInstanceID, id)
	return id
}

// URL returns the configured collector, or empty when none is set.
func (b *Beacon) URL(ctx context.Context) string {
	if u := db.Setting(ctx, b.DB, keyURL); u != "" {
		return u
	}
	return DefaultCollector
}

// SetURL records where pings go.
func (b *Beacon) SetURL(ctx context.Context, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return db.SetSetting(ctx, b.DB, keyURL, "")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("beacon: %q is not an http or https address", raw)
	}
	return db.SetSetting(ctx, b.DB, keyURL, raw)
}

// Status is what the settings page shows, including the exact payload.
type Status struct {
	Enabled bool `json:"enabled"`
	// Chosen is false until the operator has answered, which is when the UI
	// should ask rather than assume.
	Chosen      bool    `json:"chosen"`
	URL         string  `json:"url"`
	Payload     Payload `json:"payload"`
	LastDay     string  `json:"lastDay,omitempty"`
	LastVersion string  `json:"lastVersion,omitempty"`
	LastError   string  `json:"lastError,omitempty"`
	LastErrorAt string  `json:"lastErrorAt,omitempty"`
}

// Status returns the current state and the literal payload that would be sent.
func (b *Beacon) Status(ctx context.Context) Status {
	return Status{
		Enabled:     b.Enabled(ctx),
		Chosen:      b.Chosen(ctx),
		URL:         b.URL(ctx),
		Payload:     Payload{InstanceID: b.InstanceID(ctx), Version: b.Version},
		LastDay:     db.Setting(ctx, b.DB, keyLastDay),
		LastVersion: db.Setting(ctx, b.DB, keyLastVersion),
		LastError:   db.Setting(ctx, b.DB, keyLastError),
		LastErrorAt: db.Setting(ctx, b.DB, keyLastErrorAt),
	}
}

// Run starts the ping loop and returns when ctx is cancelled.
func (b *Beacon) Run(ctx context.Context) {
	// Let startup settle before the first check.
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}

	b.Tick(ctx)
	t := time.NewTicker(Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.Tick(ctx)
		}
	}
}

// Tick pings if a ping is due.
func (b *Beacon) Tick(ctx context.Context) {
	if !b.Enabled(ctx) {
		return
	}

	// Once a day — but also whenever the version changes. Without the second
	// condition an instance that updates after its daily ping keeps reporting
	// the version it was running *before* the update for up to a day, which
	// is stale exactly when adoption of a release is what you want to see.
	today := b.now().UTC().Format("2006-01-02")
	if db.Setting(ctx, b.DB, keyLastDay) == today &&
		db.Setting(ctx, b.DB, keyLastVersion) == b.Version {
		return
	}

	if err := b.Send(ctx); err != nil {
		// A ping that never lands must be visible. Failing silently and
		// retrying forever is how an install goes missing from the count with
		// nobody able to say why.
		_ = db.SetSetting(ctx, b.DB, keyLastError, err.Error())
		_ = db.SetSetting(ctx, b.DB, keyLastErrorAt, b.now().UTC().Format(time.RFC3339))
		if b.Log != nil {
			b.Log.Warn("beacon ping failed", "error", err)
		}
		return
	}

	_ = db.SetSetting(ctx, b.DB, keyLastDay, today)
	_ = db.SetSetting(ctx, b.DB, keyLastVersion, b.Version)
	_ = db.SetSetting(ctx, b.DB, keyLastError, "")
	_ = db.SetSetting(ctx, b.DB, keyLastErrorAt, "")
}

// Send posts the two-field payload, and says why when it cannot.
func (b *Beacon) Send(ctx context.Context) error {
	payload := Payload{InstanceID: b.InstanceID(ctx), Version: b.Version}
	if payload.InstanceID == "" {
		return fmt.Errorf("beacon: could not generate an instance id")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	target := b.URL(ctx)
	if target == "" {
		return errors.New("beacon: no collector address is set")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("beacon: bad collector URL %q: %w", target, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "mimir/"+b.Version)

	resp, err := b.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("beacon: could not reach %s: %w", target, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("beacon: %s returned %s", target, strings.TrimSpace(resp.Status))
	}
	return nil
}

func (b *Beacon) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}
