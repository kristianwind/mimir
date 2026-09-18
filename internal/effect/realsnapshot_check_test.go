package effect

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/kristianwind/mimir/internal/gamedata"
)

// A bench check for whoever is editing deploy/effects.json.
//
// Validate's whole value is that it compares a rule's numbers against the
// mined in-game wording — and the unit tests necessarily feed it invented
// wording, which proves the checker works and nothing about the file. This
// runs the real file against a real snapshot, which is the only way to find
// out that a rule claims 35% against a text that says 30.
//
// Skipped when there is no snapshot to hand, so CI is unaffected. To use it:
//
//	scp the mined snapshot.json to /tmp, or set MIMIR_SNAPSHOT
//	go test ./internal/effect/ -run RealSnapshot -v
//
// The coverage line it logs is the number the target view shows players, so
// it is also the quickest way to see whether a batch of new rules actually
// moved it.
func TestEffectFileAgainstARealSnapshot(t *testing.T) {
	path := os.Getenv("MIMIR_SNAPSHOT")
	if path == "" {
		path = "/tmp/snapshot.json"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no snapshot at %s to check against; set MIMIR_SNAPSHOT to point at one", path)
	}
	var snap gamedata.Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("%s is not a snapshot: %v", path, err)
	}

	rules, err := Load("../../deploy/effects.json", &snap)
	if err != nil {
		t.Fatalf("effects.json does not load against game data %s:\n%v", snap.Version, err)
	}

	snap.Effects = rules
	var modelled, total int
	for key := range snap.ArtifactSets {
		total++
		if snap.FourPieceModelled(key) {
			modelled++
		}
	}
	wm, wt := snap.WeaponPassiveCoverage()
	t.Logf("%d rules load against game data %s; four-piece coverage %d of %d sets, "+
		"weapon passives %d of %d", len(rules), snap.Version, modelled, total, wm, wt)

	if modelled == 0 {
		t.Error("no artifact set has a four-piece the engine can score; the file is not reaching the snapshot")
	}
}
