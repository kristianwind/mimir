package db

import (
	"testing"

	"github.com/kristianwind/mimir/internal/model"
)

func flower(level float64, atk, cr float64) model.Artifact {
	return model.Artifact{
		SetKey: "DeepwoodMemories", SlotKey: model.Flower, Rarity: 5,
		Level: int(level), MainStat: model.HP,
		Substats: []model.Substat{
			{Key: model.ATKPercent, Value: atk},
			{Key: model.CritRate, Value: cr},
		},
	}
}

func TestIdentityIgnoresUpgrades(t *testing.T) {
	a := flower(0, 0.047, 0.031)
	b := flower(20, 0.152, 0.101)
	if Identity(a) != Identity(b) {
		t.Error("levelling a piece changed its identity")
	}
	if Fingerprint(a) == Fingerprint(b) {
		t.Error("levelling a piece did not change its fingerprint")
	}
}

func TestIdentityIsOrderIndependent(t *testing.T) {
	a := flower(0, 0.047, 0.031)
	b := a
	b.Substats = []model.Substat{a.Substats[1], a.Substats[0]}
	if Identity(a) != Identity(b) {
		t.Error("substat order changed the identity; scanners do not guarantee an order")
	}
	if Fingerprint(a) != Fingerprint(b) {
		t.Error("substat order changed the fingerprint")
	}
}

func TestUpsertClassifiesImports(t *testing.T) {
	conn, err := Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'u', 'x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO accounts (id, user_id, uid) VALUES (1, 1, '700000001')`); err != nil {
		t.Fatal(err)
	}

	apply := func(arts []model.Artifact) ImportStats {
		t.Helper()
		tx, err := conn.Begin()
		if err != nil {
			t.Fatal(err)
		}
		stats, err := UpsertArtifacts(tx, 1, arts)
		if err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return stats
	}

	first := apply([]model.Artifact{flower(0, 0.047, 0.031)})
	if first.Inserted != 1 {
		t.Fatalf("first import: %+v, want one insert", first)
	}

	// Re-importing the identical file must not duplicate the inventory.
	second := apply([]model.Artifact{flower(0, 0.047, 0.031)})
	if second.Unchanged != 1 || second.Inserted != 0 {
		t.Errorf("re-import: %+v, want one unchanged", second)
	}

	// The same piece, now +20: an upgrade, not a new artifact.
	third := apply([]model.Artifact{flower(20, 0.152, 0.101)})
	if third.Upgraded != 1 || third.Inserted != 0 {
		t.Errorf("upgrade import: %+v, want one upgrade", third)
	}

	// A different piece with the same lines but lower values is genuinely
	// new — rolls never shrink.
	fourth := apply([]model.Artifact{flower(4, 0.099, 0.035)})
	if fourth.Inserted != 1 {
		t.Errorf("distinct piece: %+v, want one insert", fourth)
	}

	got, err := LoadArtifacts(conn, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("inventory holds %d artifacts, want 2", len(got))
	}
}

func TestLoadArtifactsRoundTripsSubstats(t *testing.T) {
	conn, err := Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'u', 'x')`)
	conn.Exec(`INSERT INTO accounts (id, user_id, uid) VALUES (1, 1, '700000001')`)

	tx, _ := conn.Begin()
	if _, err := UpsertArtifacts(tx, 1, []model.Artifact{flower(20, 0.152, 0.101)}); err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	got, err := LoadArtifacts(conn, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Substats) != 2 {
		t.Fatalf("round trip lost data: %+v", got)
	}
	if got[0].Substats[0].Value != 0.152 {
		t.Errorf("substat value = %v, want 0.152", got[0].Substats[0].Value)
	}
}

func TestMigrationAddsColumnsToAnExistingDatabase(t *testing.T) {
	path := t.TempDir() + "/upgrade.db"

	// Simulate an install created before the column existed.
	old, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`ALTER TABLE goals DROP COLUMN conditions`); err != nil {
		t.Fatalf("could not simulate the older schema: %v", err)
	}
	old.Close()

	// Reopening must add it back rather than leaving queries to fail later.
	conn, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	has, err := hasColumn(conn, "goals", "conditions")
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Error("reopening an older database did not add the new column")
	}
}
