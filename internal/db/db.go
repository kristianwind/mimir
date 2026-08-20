// Package db owns the SQLite schema and connection.
//
// SQLite, pure-Go (modernc), same as Yggdrasil: Mimir ships as a rune, and a
// rune that needs a separate database server is a rune nobody installs. The
// vector search for character guides is a linear scan over a few thousand
// float32 blobs, which is microseconds at this corpus size and removes the
// last reason to reach for Postgres and pgvector.
//
// Unlike Yggdrasil, primary keys here are INTEGER rather than UUID text: the
// optimizer keys artifacts by id on its hot path and walks hundreds of
// thousands of combinations, where integer keys measurably beat string ones.
package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Open connects to the database at path and applies migrations.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open sqlite: %w", err)
	}
	conn.SetMaxOpenConns(1) // SQLite writer serialization
	if err := migrate(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("db: migrate: %w", err)
	}
	return conn, nil
}

func migrate(conn *sql.DB) error {
	if _, err := conn.Exec(schema); err != nil {
		return err
	}
	return addColumns(conn)
}

// addedColumn is a column introduced after a table shipped.
//
// The schema above is CREATE TABLE IF NOT EXISTS, which does nothing to a
// table that already exists — so a new column has to be added explicitly or
// an upgraded install keeps the old shape and fails at query time. Additive
// columns only: anything that needs data moved deserves a real migration.
type addedColumn struct {
	table  string
	column string
	ddl    string
}

var addedColumns = []addedColumn{
	{"goals", "conditions", `ALTER TABLE goals ADD COLUMN conditions TEXT NOT NULL DEFAULT '{}'`},
}

func addColumns(conn *sql.DB) error {
	for _, c := range addedColumns {
		has, err := hasColumn(conn, c.table, c.column)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := conn.Exec(c.ddl); err != nil {
			return fmt.Errorf("add %s.%s: %w", c.table, c.column, err)
		}
	}
	return nil
}

func hasColumn(conn *sql.DB, table, column string) (bool, error) {
	rows, err := conn.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

const schema = `
-- ---------------------------------------------------------------- identity

CREATE TABLE IF NOT EXISTS users (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	username      TEXT UNIQUE NOT NULL,
	email         TEXT UNIQUE,
	password_hash TEXT NOT NULL,
	role          TEXT NOT NULL DEFAULT 'user',
	theme         TEXT NOT NULL DEFAULT 'anemo',
	theme_mode    TEXT NOT NULL DEFAULT 'system',
	disabled      INTEGER NOT NULL DEFAULT 0,
	created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS sessions (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	token_hash TEXT UNIQUE NOT NULL,
	user_agent TEXT,
	expires_at TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

-- ---------------------------------------------------------------- accounts

-- One row per in-game UID. A user may own several: EU and Asia are separate
-- accounts with separate inventories, and merging them would produce builds
-- out of artifacts that cannot meet.
CREATE TABLE IF NOT EXISTS accounts (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	uid        TEXT NOT NULL,
	nickname   TEXT NOT NULL DEFAULT '',
	region     TEXT NOT NULL DEFAULT '',
	ar_level   INTEGER NOT NULL DEFAULT 0,
	wl_level   INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL DEFAULT (datetime('now')),
	updated_at TEXT NOT NULL DEFAULT (datetime('now')),
	UNIQUE(user_id, uid)
);

-- HoYoLAB session cookies, AES-256-GCM encrypted with a key derived from the
-- machine secret in config. These are full account credentials: they are
-- never logged, never returned by the API, and a failed decrypt disables the
-- integration rather than falling back to plaintext.
CREATE TABLE IF NOT EXISTS account_secrets (
	account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
	kind       TEXT NOT NULL,
	nonce      BLOB NOT NULL,
	ciphertext BLOB NOT NULL,
	updated_at TEXT NOT NULL DEFAULT (datetime('now')),
	PRIMARY KEY (account_id, kind)
);

-- ---------------------------------------------------------------- inventory

CREATE TABLE IF NOT EXISTS characters (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	account_id    INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
	char_key      TEXT NOT NULL,
	level         INTEGER NOT NULL DEFAULT 1,
	ascension     INTEGER NOT NULL DEFAULT 0,
	constellation INTEGER NOT NULL DEFAULT 0,
	talent_auto   INTEGER NOT NULL DEFAULT 1,
	talent_skill  INTEGER NOT NULL DEFAULT 1,
	talent_burst  INTEGER NOT NULL DEFAULT 1,
	source        TEXT NOT NULL DEFAULT 'manual',
	updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
	UNIQUE(account_id, char_key)
);

CREATE TABLE IF NOT EXISTS weapons (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
	weapon_key TEXT NOT NULL,
	level      INTEGER NOT NULL DEFAULT 1,
	ascension  INTEGER NOT NULL DEFAULT 0,
	refinement INTEGER NOT NULL DEFAULT 1,
	location   TEXT NOT NULL DEFAULT '',
	locked     INTEGER NOT NULL DEFAULT 0,
	source     TEXT NOT NULL DEFAULT 'manual',
	updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_weapons_account ON weapons(account_id);

-- Two hashes, because an artifact has two kinds of sameness.
--
-- fingerprint covers the full current state, so re-importing an unchanged
-- .good file updates 1,400 rows instead of duplicating them.
--
-- identity covers only what an upgrade cannot change: set, slot, rarity, main
-- stat and which substat lines the piece rolled. It is how Mimir recognises
-- "the same flower, now +20" rather than filing it as a second flower.
CREATE TABLE IF NOT EXISTS artifacts (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	account_id  INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
	fingerprint TEXT NOT NULL,
	identity    TEXT NOT NULL DEFAULT '',
	set_key     TEXT NOT NULL,
	slot_key    TEXT NOT NULL,
	rarity      INTEGER NOT NULL,
	level       INTEGER NOT NULL DEFAULT 0,
	main_stat   TEXT NOT NULL,
	substats    TEXT NOT NULL DEFAULT '[]',
	location    TEXT NOT NULL DEFAULT '',
	locked      INTEGER NOT NULL DEFAULT 0,
	crit_value  REAL NOT NULL DEFAULT 0,
	source      TEXT NOT NULL DEFAULT 'manual',
	updated_at  TEXT NOT NULL DEFAULT (datetime('now')),
	UNIQUE(account_id, fingerprint)
);
CREATE INDEX IF NOT EXISTS idx_artifacts_slot ON artifacts(account_id, slot_key);
CREATE INDEX IF NOT EXISTS idx_artifacts_set ON artifacts(account_id, set_key);
CREATE INDEX IF NOT EXISTS idx_artifacts_identity ON artifacts(account_id, identity);

CREATE TABLE IF NOT EXISTS materials (
	account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
	item_key   TEXT NOT NULL,
	count      INTEGER NOT NULL DEFAULT 0,
	updated_at TEXT NOT NULL DEFAULT (datetime('now')),
	PRIMARY KEY (account_id, item_key)
);

-- ---------------------------------------------------------------- game data

CREATE TABLE IF NOT EXISTS gamedata_versions (
	version     TEXT PRIMARY KEY,
	payload     BLOB NOT NULL,
	active      INTEGER NOT NULL DEFAULT 0,
	imported_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Enka responses cached until their own TTL expires. Persisting rather than
-- keeping this in memory means a restart does not spend the rate-limit budget
-- again on data that has not changed.
CREATE TABLE IF NOT EXISTS enka_cache (
	uid        TEXT PRIMARY KEY,
	payload    BLOB NOT NULL,
	fetched_at TEXT NOT NULL DEFAULT (datetime('now')),
	expires_at TEXT NOT NULL
);

-- ---------------------------------------------------------------- planning

-- What the player is actually trying to build. Every ranking in Mimir is
-- relative to these goals: "should I level Bennett?" has no answer without
-- knowing which teams he is meant to serve.
CREATE TABLE IF NOT EXISTS goals (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	account_id    INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
	char_key      TEXT NOT NULL,
	priority      INTEGER NOT NULL DEFAULT 0,
	team          TEXT NOT NULL DEFAULT '[]',
	rotation      TEXT NOT NULL DEFAULT '{}',
	target        TEXT NOT NULL DEFAULT '{}',
	-- The player's answers to what the effect layer cannot infer: is
	-- Noblesse up, how many Marechaussee stacks, is the enemy frozen.
	conditions    TEXT NOT NULL DEFAULT '{}',
	notes         TEXT NOT NULL DEFAULT '',
	created_at    TEXT NOT NULL DEFAULT (datetime('now')),
	UNIQUE(account_id, char_key)
);

-- The ranked upgrade plan: one row per candidate action, scored by expected
-- damage gain per resin. This table is the product.
CREATE TABLE IF NOT EXISTS upgrade_actions (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	account_id   INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
	goal_id      INTEGER REFERENCES goals(id) ON DELETE CASCADE,
	kind         TEXT NOT NULL,
	subject      TEXT NOT NULL,
	detail       TEXT NOT NULL DEFAULT '{}',
	gain_pct     REAL NOT NULL DEFAULT 0,
	resin_cost   REAL NOT NULL DEFAULT 0,
	efficiency   REAL NOT NULL DEFAULT 0,
	blocked_by   TEXT NOT NULL DEFAULT '',
	computed_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_actions_rank ON upgrade_actions(account_id, efficiency DESC);

CREATE TABLE IF NOT EXISTS resin_snapshots (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	account_id  INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
	resin       INTEGER NOT NULL,
	max_resin   INTEGER NOT NULL DEFAULT 200,
	recovery_at TEXT,
	captured_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_resin_account ON resin_snapshots(account_id, captured_at DESC);

CREATE TABLE IF NOT EXISTS notifications (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	kind       TEXT NOT NULL,
	title      TEXT NOT NULL,
	body       TEXT NOT NULL DEFAULT '',
	sent_at    TEXT,
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- ---------------------------------------------------------------- guides

-- The RAG corpus behind "why is 4pc Marechaussee better here?". Answers cite
-- a guide; the numbers still come from the engine, never from the model.
CREATE TABLE IF NOT EXISTS guides (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	char_key   TEXT NOT NULL DEFAULT '',
	source     TEXT NOT NULL,
	url        TEXT NOT NULL DEFAULT '',
	title      TEXT NOT NULL,
	body       TEXT NOT NULL,
	fetched_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_guides_char ON guides(char_key);

CREATE TABLE IF NOT EXISTS guide_chunks (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	guide_id  INTEGER NOT NULL REFERENCES guides(id) ON DELETE CASCADE,
	ordinal   INTEGER NOT NULL,
	text      TEXT NOT NULL,
	embedding BLOB
);
CREATE INDEX IF NOT EXISTS idx_chunks_guide ON guide_chunks(guide_id);

-- ---------------------------------------------------------------- settings

-- Instance-wide settings: the beacon's opt-out and anonymous id, the
-- updater's bookkeeping. Deliberately key/value rather than columns, because
-- these are operator switches rather than domain data, and because an unset
-- key has to stay distinguishable from an explicit "0" — an operator who
-- turned the beacon off must not have it turned back on by an upgrade.
CREATE TABLE IF NOT EXISTS settings (
	key        TEXT PRIMARY KEY,
	value      TEXT NOT NULL,
	updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- ---------------------------------------------------------------- audit

CREATE TABLE IF NOT EXISTS audit_log (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
	action   TEXT NOT NULL,
	resource TEXT NOT NULL DEFAULT '',
	detail   TEXT NOT NULL DEFAULT '',
	ts       TEXT NOT NULL DEFAULT (datetime('now'))
);
`
