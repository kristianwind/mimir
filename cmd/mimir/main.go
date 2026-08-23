// Command mimir is the Genshin Impact build advisor server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	mimir "github.com/kristianwind/mimir"
	"github.com/kristianwind/mimir/internal/api"
	"github.com/kristianwind/mimir/internal/auth"
	"github.com/kristianwind/mimir/internal/beacon"
	"github.com/kristianwind/mimir/internal/config"
	"github.com/kristianwind/mimir/internal/db"
	"github.com/kristianwind/mimir/internal/enka"
	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/kvasir"
	"github.com/kristianwind/mimir/internal/llm"
	"github.com/kristianwind/mimir/internal/selfupdate"
)

// version is stamped at build time:
//
//	go build -ldflags "-X main.version=v0.3.0" ./cmd/mimir
//
// An unstamped build stays "dev", and the updater refuses to replace it —
// throwing away a binary somebody built on purpose is not an upgrade.
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mimir:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "serve"
	if len(args) > 0 && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}

	switch cmd {
	case "serve":
		return serve(args)
	case "useradd":
		return useradd(args)
	case "gamedata":
		return gamedataCmd(args)
	case "version":
		fmt.Println(version)
		return nil
	case "watchdog":
		return watchdogCmd(args)
	case "rollback":
		return rollbackCmd(args)
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `mimir — Genshin Impact build advisor

usage:
  mimir serve              start the HTTP server (default)
  mimir useradd -u NAME    create a user, reading the password from stdin
  mimir gamedata import F  load a game data snapshot and make it active
  mimir gamedata list      list stored snapshots
  mimir gamedata activate V  roll back or forward to a stored snapshot
  mimir version            print the version
  mimir rollback           restore the binary the last update replaced
  mimir watchdog           internal: used by the updater to undo a failed update
  mimir help

configuration is read from the environment; see docs/CONFIGURATION.md
`)
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	conn, err := db.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer conn.Close()

	gd := gamedata.NewStore(conn)
	if err := gd.Load(); err != nil {
		if !errors.Is(err, gamedata.ErrNoSnapshot) {
			return err
		}
		// Not fatal. The server runs, imports are refused with a clear
		// message, and the operator can sync without a restart.
		log.Warn("no game data snapshot; imports and calculations are unavailable until one is synced")
	}

	web, err := mimir.Web()
	if err != nil {
		return err
	}

	srv := &api.Server{
		Config:   cfg,
		DB:       conn,
		Auth:     &auth.Store{DB: conn, Secure: cfg.Secure},
		Enka:     enka.NewCached(cfg.UserAgent),
		GameData: gd,
		Log:      log,
		Web:      web,
		Version:  version,
		Beacon:   beacon.New(conn, version, log),
		Updater: selfupdate.New(conn, version, cfg.Repo, cfg.DataDir,
			healthURL(cfg.Addr), log),
		// The AI layer. With no endpoint configured this is an advisor with a
		// nil client, which reports itself unavailable and is never asked
		// anything — the rest of Mimir does not know the difference, because
		// no number in it comes from a model.
		Kvasir: &kvasir.Advisor{
			Client: llm.New(cfg.LLMBaseURL, cfg.LLMModel, cfg.LLMAPIKey, cfg.LLMTimeout),
		},
	}
	if srv.Kvasir.Available() {
		log.Info("the AI layer is on", "endpoint", cfg.LLMBaseURL, "model", cfg.LLMModel)
	}

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		// Generous: a .good import of a full inventory is a large body over
		// a slow home connection, and the optimizer can run for seconds.
		ReadTimeout:  2 * time.Minute,
		WriteTimeout: 2 * time.Minute,
		IdleTimeout:  2 * time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The updater replaces the binary on disk; nothing happens until this
	// process leaves and the supervisor starts the new one.
	srv.Shutdown = stop

	go srv.Beacon.Run(ctx)

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr, "data", cfg.DataDir)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	}
}

func useradd(args []string) error {
	fs := flag.NewFlagSet("useradd", flag.ContinueOnError)
	username := fs.String("u", "", "username")
	role := fs.String("role", "admin", "role: admin or user")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *username == "" {
		return errors.New("useradd: -u is required")
	}

	// Read the password from stdin so it never lands in shell history or the
	// process list, which is where a -password flag would put it.
	fmt.Fprint(os.Stderr, "password: ")
	var password string
	if _, err := fmt.Fscanln(os.Stdin, &password); err != nil {
		return fmt.Errorf("useradd: read password: %w", err)
	}
	if len(password) < 12 {
		return errors.New("useradd: password must be at least 12 characters")
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

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if _, err := conn.Exec(
		`INSERT INTO users (username, password_hash, role) VALUES (?, ?, ?)`,
		*username, hash, *role); err != nil {
		return fmt.Errorf("useradd: %w", err)
	}
	fmt.Fprintf(os.Stderr, "created %s (%s)\n", *username, *role)
	return nil
}
