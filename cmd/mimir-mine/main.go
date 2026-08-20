// Command mimir-mine builds a gamedata snapshot from the public datamines.
//
// It is separate from the server on purpose. This is the one part of Mimir
// that depends on third-party repositories whose shape is not a contract:
// mirrors go stale, move, and desync from each other. When that happens the
// miner must fail loudly on its own rather than take a running server with
// it — and because activation is a separate step, the previous snapshot keeps
// serving until a new one validates.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kristianwind/mimir/internal/effect"
	"github.com/kristianwind/mimir/internal/mine"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mimir-mine:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := mine.DefaultConfig()

	out := flag.String("o", "snapshot.json", "where to write the snapshot")
	cache := flag.String("cache", filepath.Join(os.TempDir(), "mimir-mine-cache"), "download cache directory")
	supplements := flag.String("supplements", "", "JSON file with the tables the datamine does not carry")
	effects := flag.String("effects", "", "JSON file of effect rules (conditional set bonuses, conversion passives)")
	quiet := flag.Bool("q", false, "only report errors")
	maxAge := flag.Duration("cache-ttl", 24*time.Hour, "how long a cached download stays fresh (0 = forever)")
	flag.StringVar(&cfg.Version, "version", "", "version label for the snapshot (required)")
	flag.StringVar(&cfg.GameDataRepo, "gamedata-repo", cfg.GameDataRepo, "datamine repository for the numeric tables")
	flag.StringVar(&cfg.GameDataRef, "gamedata-ref", cfg.GameDataRef, "datamine ref")
	flag.StringVar(&cfg.GenshinDBRepo, "names-repo", cfg.GenshinDBRepo, "repository for weapon and artifact names")
	flag.StringVar(&cfg.GenshinDBRef, "names-ref", cfg.GenshinDBRef, "names ref")
	flag.Parse()

	if cfg.Version == "" {
		return errors.New("-version is required; a snapshot without one cannot be rolled back to")
	}

	log := func(format string, args ...any) {
		if !*quiet {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		}
	}

	src := mine.NewSource(*cache)
	src.MaxAge = *maxAge
	src.Log = func(format string, args ...any) {
		if !*quiet {
			fmt.Fprintf(os.Stderr, "  "+format+"\n", args...)
		}
	}

	m := mine.New(src, cfg)
	m.Log = log

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	start := time.Now()
	snap, err := m.Run(ctx)
	if err != nil {
		return err
	}

	if *supplements != "" {
		log("merging supplements from %s", *supplements)
		if err := mine.MergeSupplements(snap, *supplements); err != nil {
			return err
		}
	}

	if *effects != "" {
		log("loading effect rules from %s", *effects)
		// Loading validates every rule against the wording mined above, so
		// a fabricated multiplier stops the build here rather than
		// surfacing as a build that is quietly 20% too strong.
		rules, err := effect.Load(*effects, snap)
		if err != nil {
			return err
		}
		snap.Effects = rules
		log("  %d effect rules verified against their in-game text", len(rules))
	}

	report := mine.Validate(snap)
	for _, line := range report.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", line)
	}
	if len(report.Errors) > 0 {
		for _, line := range report.Errors {
			fmt.Fprintln(os.Stderr, "error:", line)
		}
		return fmt.Errorf("snapshot failed validation with %d error(s); nothing written", len(report.Errors))
	}

	raw, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, raw, 0o644); err != nil {
		return err
	}

	log("wrote %s (%.1f MB) in %s", *out, float64(len(raw))/(1<<20), time.Since(start).Round(time.Second))
	log("  %d characters, %d weapons, %d artifact sets, %d curves",
		len(snap.Characters), len(snap.Weapons), len(snap.ArtifactSets), len(snap.Curves))
	return nil
}
