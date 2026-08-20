package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kristianwind/mimir/internal/config"
	"github.com/kristianwind/mimir/internal/db"
	"github.com/kristianwind/mimir/internal/selfupdate"
)

// healthURL turns a listen address into something the watchdog can probe.
// A wildcard bind is reachable on loopback, which is where a watchdog on the
// same host should knock.
func healthURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://127.0.0.1:8080/api/healthz"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s/api/healthz", net.JoinHostPort(host, port))
}

// watchdogCmd is spawned by the updater from the *backup* binary just before
// the swap. It is not meant to be run by hand, but it is documented rather
// than hidden: a process that can silently replace the running binary should
// be inspectable.
func watchdogCmd(args []string) error {
	fs := flag.NewFlagSet("watchdog", flag.ContinueOnError)
	url := fs.String("url", "", "health endpoint the new binary should answer")
	restore := fs.String("restore", "", "path to restore this binary to on failure")
	target := fs.String("version", "", "version being watched, for the log")
	logPath := fs.String("log", "", "file to append outcomes to")
	deadline := fs.Duration("deadline", 90*time.Second, "how long the new binary gets")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *url == "" || *restore == "" {
		return errors.New("watchdog: -url and -restore are required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	w := selfupdate.Watchdog{
		HealthURL: *url,
		Restore:   *restore,
		Version:   *target,
		LogPath:   *logPath,
		Deadline:  *deadline,
	}
	restored, err := w.Run(ctx)
	if err != nil {
		return err
	}
	if restored {
		fmt.Fprintln(os.Stderr, "watchdog: the update did not come up; the previous binary was restored")
	}
	return nil
}

// rollbackCmd restores the binary the last update replaced.
func rollbackCmd(args []string) error {
	fs := flag.NewFlagSet("rollback", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	conn, err := db.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer conn.Close()

	u := selfupdate.New(conn, version, cfg.Repo, cfg.DataDir, healthURL(cfg.Addr), nil)
	restored, err := u.Rollback(context.Background())
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "restored %s — restart Mimir to run it\n", strings.TrimSpace(restored))
	return nil
}
