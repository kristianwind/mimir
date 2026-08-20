package selfupdate

import (
	"context"
	"fmt"
	"os"
	"time"
)

// Watchdog undoes an update that does not come up.
//
// It runs from the backup binary — the one already known to work on this
// machine — because a watchdog built from the candidate would share whatever
// is wrong with it. It waits for the health endpoint, and if the new binary
// never answers, it copies itself back over the target.
//
// This is the honest limit of what a process can promise about its own
// replacement: the swap is only committed after the candidate has been run
// and proved to serve, and if it still fails afterwards, something that
// definitely works is already waiting to put it back.
type Watchdog struct {
	// HealthURL is the endpoint the new binary should answer.
	HealthURL string
	// Restore is the path to put this binary back at.
	Restore string
	// Version is the update being watched, for the log.
	Version string
	// LogPath receives a line per outcome. Empty writes to stderr.
	LogPath string
	// Deadline is how long the new binary gets.
	Deadline time.Duration
	// Now is injectable for tests.
	Self func() (string, error)
}

// Run watches, and restores on failure. It returns true if it restored.
func (w Watchdog) Run(ctx context.Context) (bool, error) {
	deadline := w.Deadline
	if deadline <= 0 {
		deadline = 90 * time.Second
	}
	// Give the supervisor a moment to notice the old process left before
	// concluding that nothing is listening.
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-time.After(5 * time.Second):
	}

	err := waitHealthy(ctx, w.HealthURL, deadline)
	if err == nil {
		w.logf("update to %s came up fine", w.Version)
		return false, nil
	}

	self := w.Self
	if self == nil {
		self = os.Executable
	}
	backup, serr := self()
	if serr != nil {
		w.logf("update to %s did not come up (%v) and the backup could not be located: %v",
			w.Version, err, serr)
		return false, serr
	}
	if rerr := copyOver(backup, w.Restore); rerr != nil {
		w.logf("update to %s did not come up (%v) and the rollback failed: %v", w.Version, err, rerr)
		return false, rerr
	}

	w.logf("update to %s did not come up (%v) — rolled back to %s", w.Version, err, backup)
	return true, nil
}

func (w Watchdog) logf(format string, args ...any) {
	line := fmt.Sprintf("%s watchdog: %s\n",
		time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
	if w.LogPath == "" {
		fmt.Fprint(os.Stderr, line)
		return
	}
	f, err := os.OpenFile(w.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		fmt.Fprint(os.Stderr, line)
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}
