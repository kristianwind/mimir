package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/kristianwind/mimir/internal/effect"
	"github.com/kristianwind/mimir/internal/mine"
)

// Mining from the web.
//
// It was the last thing that needed a shell on the host: a container has no
// terminal a browser can reach, so loading game data meant `docker exec` and
// therefore SSH. That is a poor answer for an app whose whole premise is that
// it refuses to work without game data.
//
// It runs as a background job rather than a long request. Not because forty
// seconds would time out — it would not — but because a progress log that
// arrives all at once at the end is not progress, and a browser that hangs
// silently is indistinguishable from one that has broken.

// MineJob is the state of the running or last-finished mine. One at a time:
// two concurrent mines would fight over the same cache directory and produce
// a snapshot neither of them can vouch for.
type MineJob struct {
	mu       sync.Mutex
	running  bool
	started  time.Time
	finished time.Time
	version  string
	lines    []string
	warnings []string
	err      string
}

// MineStatus is what the page polls.
type MineStatus struct {
	Running  bool     `json:"running"`
	Version  string   `json:"version,omitempty"`
	Started  string   `json:"started,omitempty"`
	Finished string   `json:"finished,omitempty"`
	Lines    []string `json:"lines"`
	Warnings []string `json:"warnings"`
	Error    string   `json:"error,omitempty"`
	// Elapsed is seconds, so the page can say how long it has been going
	// rather than leaving the reader to guess whether it is stuck.
	Elapsed int `json:"elapsed"`
}

func (j *MineJob) status() MineStatus {
	j.mu.Lock()
	defer j.mu.Unlock()

	st := MineStatus{
		Running:  j.running,
		Version:  j.version,
		Lines:    append([]string(nil), j.lines...),
		Warnings: append([]string(nil), j.warnings...),
		Error:    j.err,
	}
	if st.Lines == nil {
		st.Lines = []string{}
	}
	if st.Warnings == nil {
		st.Warnings = []string{}
	}
	if !j.started.IsZero() {
		st.Started = j.started.UTC().Format(time.RFC3339)
		end := j.finished
		if j.running {
			end = time.Now()
		}
		st.Elapsed = int(end.Sub(j.started).Seconds())
	}
	if !j.finished.IsZero() {
		st.Finished = j.finished.UTC().Format(time.RFC3339)
	}
	return st
}

func (j *MineJob) logf(format string, args ...any) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.lines = append(j.lines, fmt.Sprintf(format, args...))
}

func (s *Server) handleMineStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, s.Mine.status())
}

func (s *Server) handleStartMine(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "ugyldig forespørgsel", "")
		return
	}
	if body.Version == "" {
		writeError(w, http.StatusBadRequest, "angiv en spilversion",
			"Fx 7.0.0. Den mærker snapshottet, så du kan rulle tilbage til det.")
		return
	}

	j := s.Mine
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		writeError(w, http.StatusConflict, "der kører allerede en synkronisering", "")
		return
	}
	j.running = true
	j.started = time.Now()
	j.finished = time.Time{}
	j.version = body.Version
	j.lines = nil
	j.warnings = nil
	j.err = ""
	j.mu.Unlock()

	s.audit(r, "gamedata.mine", body.Version, nil)

	// Detached from the request: closing the tab must not abandon a mine
	// half-way through and leave a partial snapshot behind.
	go s.runMine(context.WithoutCancel(r.Context()), body.Version)

	writeJSON(w, http.StatusAccepted, j.status())
}

func (s *Server) runMine(ctx context.Context, version string) {
	j := s.Mine
	finish := func(err error) {
		j.mu.Lock()
		j.running = false
		j.finished = time.Now()
		if err != nil {
			j.err = err.Error()
		}
		j.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	cacheDir := filepath.Join(s.Config.DataDir, "mine-cache")
	src := mine.NewSource(cacheDir)
	src.MaxAge = 24 * time.Hour
	src.Log = func(string, ...any) {} // per-file fetches would drown the page

	cfg := mine.DefaultConfig()
	cfg.Version = version

	m := mine.New(src, cfg)
	m.Log = j.logf

	snap, err := m.Run(ctx)
	if err != nil {
		finish(err)
		return
	}

	if s.Config.SupplementsPath != "" {
		if _, statErr := os.Stat(s.Config.SupplementsPath); statErr == nil {
			j.logf("indlæser %s", s.Config.SupplementsPath)
			if err := mine.MergeSupplements(snap, s.Config.SupplementsPath); err != nil {
				finish(err)
				return
			}
		} else {
			j.warnings = append(j.warnings,
				"fandt ikke "+s.Config.SupplementsPath+"; resinpriser og domæner mangler")
		}
	}

	if s.Config.EffectsPath != "" {
		if _, statErr := os.Stat(s.Config.EffectsPath); statErr == nil {
			j.logf("indlæser og verificerer %s", s.Config.EffectsPath)
			rules, err := effect.Load(s.Config.EffectsPath, snap)
			if err != nil {
				// A rule whose numbers do not match the game's own wording
				// stops the whole sync. A snapshot that is quietly missing
				// its conditional bonuses looks exactly like one whose
				// bonuses do nothing.
				finish(err)
				return
			}
			snap.Effects = rules
			j.logf("%d effekt-regler verificeret mod deres egen spiltekst", len(rules))
		} else {
			j.warnings = append(j.warnings,
				"fandt ikke "+s.Config.EffectsPath+"; betingede bonusser mangler")
		}
	}

	report := mine.Validate(snap)
	j.mu.Lock()
	j.warnings = append(j.warnings, report.Warnings...)
	j.mu.Unlock()
	if len(report.Errors) > 0 {
		finish(fmt.Errorf("snapshottet bestod ikke valideringen: %v", report.Errors))
		return
	}

	if err := s.GameData.Save(snap); err != nil {
		finish(err)
		return
	}

	j.logf("aktiverede %s — %d karakterer, %d våben, %d artifact-sæt",
		snap.Version, len(snap.Characters), len(snap.Weapons), len(snap.ArtifactSets))
	finish(nil)
}
