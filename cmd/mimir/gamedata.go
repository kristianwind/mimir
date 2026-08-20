package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/kristianwind/mimir/internal/config"
	"github.com/kristianwind/mimir/internal/db"
	"github.com/kristianwind/mimir/internal/gamedata"
)

// gamedataCmd manages the version-dependent game data snapshots.
//
// The miner that builds a snapshot from Dimbreath/AnimeGameData is a separate
// tool (see docs/GAMEDATA.md): it is a large, brittle mapping job against
// ExcelBinOutput, and keeping it out of the server binary means a broken
// upstream commit cannot take the server down with it. The server only ever
// imports, lists and activates finished snapshots.
func gamedataCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("gamedata: expected import, list or activate")
	}
	sub, rest := args[0], args[1:]

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	conn, err := db.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer conn.Close()
	store := gamedata.NewStore(conn)

	switch sub {
	case "import":
		return gamedataImport(store, rest)
	case "list":
		return gamedataList(store)
	case "activate":
		return gamedataActivate(store, rest)
	default:
		return fmt.Errorf("gamedata: unknown subcommand %q", sub)
	}
}

func gamedataImport(store *gamedata.Store, args []string) error {
	fs := flag.NewFlagSet("gamedata import", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: mimir gamedata import <snapshot.json>")
	}

	raw, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("gamedata import: %w", err)
	}
	var snap gamedata.Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return fmt.Errorf("gamedata import: %w", err)
	}
	if err := validate(&snap); err != nil {
		return err
	}
	if err := store.Save(&snap); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "imported %s: %d characters, %d weapons, %d artifact sets\n",
		snap.Version, len(snap.Characters), len(snap.Weapons), len(snap.ArtifactSets))
	return nil
}

// validate refuses a snapshot that would make the engine silently wrong.
//
// A half-mined snapshot is worse than no snapshot: the server would happily
// import showcases, skip every unmapped character as a warning, and rank a
// roster that is missing half its members. Better to reject it at the door.
func validate(snap *gamedata.Snapshot) error {
	var missing []string
	if snap.Version == "" {
		missing = append(missing, "version")
	}
	if len(snap.Characters) == 0 {
		missing = append(missing, "characters")
	}
	if len(snap.ArtifactSets) == 0 {
		missing = append(missing, "artifactSets")
	}
	if len(snap.AvatarIDs) == 0 {
		missing = append(missing, "avatarIds (Enka import needs these)")
	}
	// Either bridge will do: setIds is what Enka reports on every artifact
	// today, setNameHashes is the fallback for older payloads.
	if len(snap.SetIDs) == 0 && len(snap.SetNameHashes) == 0 {
		missing = append(missing, "setIds or setNameHashes (Enka import needs one of them)")
	}
	if len(snap.MainStatValues) == 0 {
		missing = append(missing, "mainStatValues (the optimizer needs these)")
	}
	if len(snap.SubstatRolls) == 0 {
		missing = append(missing, "substatRolls (the farm simulator needs these)")
	}
	if len(snap.LevelMultipliers) == 0 {
		missing = append(missing, "levelMultipliers (transformative reactions need these)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("gamedata import: snapshot is incomplete, missing: %v", missing)
	}
	return nil
}

func gamedataList(store *gamedata.Store) error {
	versions, err := store.Versions()
	if err != nil {
		return err
	}
	if len(versions) == 0 {
		fmt.Fprintln(os.Stderr, "no snapshots imported")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "VERSION\tACTIVE\tIMPORTED")
	for _, v := range versions {
		active := ""
		if v.Active {
			active = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", v.Version, active, v.ImportedAt)
	}
	return w.Flush()
}

func gamedataActivate(store *gamedata.Store, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: mimir gamedata activate <version>")
	}
	if err := store.Activate(args[0]); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "activated %s\n", args[0])
	return nil
}
