package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kristianwind/mimir/internal/model"
)

// Fingerprint hashes an artifact's full current state.
func Fingerprint(a model.Artifact) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%s|%d|%s|%d", a.SetKey, a.SlotKey, a.Rarity, a.MainStat, a.Level)
	for _, s := range sortedSubstats(a) {
		fmt.Fprintf(&b, "|%s=%s", s.Key, strconv.FormatFloat(s.Value, 'f', 4, 64))
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// Identity hashes only the parts of an artifact that upgrading cannot change.
func Identity(a model.Artifact) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%s|%d|%s", a.SetKey, a.SlotKey, a.Rarity, a.MainStat)
	for _, s := range sortedSubstats(a) {
		fmt.Fprintf(&b, "|%s", s.Key)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func sortedSubstats(a model.Artifact) []model.Substat {
	out := append([]model.Substat(nil), a.Substats...)
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// ImportStats reports what an import actually did, so the UI can say
// "1,412 unchanged, 6 upgraded, 3 new" instead of a silent success.
type ImportStats struct {
	Inserted  int `json:"inserted"`
	Upgraded  int `json:"upgraded"`
	Unchanged int `json:"unchanged"`
}

// UpsertArtifacts writes an imported inventory, matching each piece against
// what is already stored.
//
// The three-way match is the difference between a tool you can re-import into
// and one you have to wipe first: an unchanged piece is a no-op, a piece whose
// substats all grew is the same physical artifact levelled up, and anything
// else is genuinely new.
func UpsertArtifacts(tx *sql.Tx, accountID int64, arts []model.Artifact) (ImportStats, error) {
	var stats ImportStats

	for _, a := range arts {
		fp := Fingerprint(a)
		id := Identity(a)
		subs, err := json.Marshal(a.Substats)
		if err != nil {
			return stats, err
		}

		var existing int64
		err = tx.QueryRow(
			`SELECT id FROM artifacts WHERE account_id = ? AND fingerprint = ?`,
			accountID, fp,
		).Scan(&existing)
		if err == nil {
			if _, err := tx.Exec(
				`UPDATE artifacts SET location = ?, locked = ?, source = ?, updated_at = datetime('now') WHERE id = ?`,
				a.Location, a.Lock, a.Source, existing,
			); err != nil {
				return stats, err
			}
			stats.Unchanged++
			continue
		}
		if err != sql.ErrNoRows {
			return stats, err
		}

		upgraded, err := matchUpgrade(tx, accountID, id, a)
		if err != nil {
			return stats, err
		}
		if upgraded != 0 {
			if _, err := tx.Exec(`
				UPDATE artifacts
				SET fingerprint = ?, level = ?, substats = ?, location = ?, locked = ?,
				    crit_value = ?, source = ?, updated_at = datetime('now')
				WHERE id = ?`,
				fp, a.Level, string(subs), a.Location, a.Lock, a.CritValue(), a.Source, upgraded,
			); err != nil {
				return stats, err
			}
			stats.Upgraded++
			continue
		}

		if _, err := tx.Exec(`
			INSERT INTO artifacts
				(account_id, fingerprint, identity, set_key, slot_key, rarity, level,
				 main_stat, substats, location, locked, crit_value, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			accountID, fp, id, a.SetKey, a.SlotKey, a.Rarity, a.Level,
			a.MainStat, string(subs), a.Location, a.Lock, a.CritValue(), a.Source,
		); err != nil {
			return stats, err
		}
		stats.Inserted++
	}

	return stats, nil
}

// matchUpgrade finds the stored row that is the same physical artifact at a
// lower level, or 0 if there is none. Where several rows could match, the
// closest one wins: an account can hold two Deepwood flowers with the same
// substat lines, and picking the nearer of the two keeps their histories from
// swapping places between imports.
func matchUpgrade(tx *sql.Tx, accountID int64, identity string, a model.Artifact) (int64, error) {
	rows, err := tx.Query(
		`SELECT id, level, substats FROM artifacts WHERE account_id = ? AND identity = ? AND level <= ?`,
		accountID, identity, a.Level,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	incoming := make(map[model.Stat]float64, len(a.Substats))
	for _, s := range a.Substats {
		incoming[s.Key] = s.Value
	}

	var (
		bestID   int64
		bestDist = -1.0
	)
	for rows.Next() {
		var (
			id    int64
			level int
			raw   string
		)
		if err := rows.Scan(&id, &level, &raw); err != nil {
			return 0, err
		}
		var stored []model.Substat
		if err := json.Unmarshal([]byte(raw), &stored); err != nil {
			return 0, err
		}
		if len(stored) != len(a.Substats) {
			continue
		}

		dist := 0.0
		ok := true
		for _, s := range stored {
			cur, present := incoming[s.Key]
			// A substat that shrank means this is a different piece: rolls
			// only ever add.
			if !present || cur < s.Value-1e-9 {
				ok = false
				break
			}
			dist += cur - s.Value
		}
		if !ok {
			continue
		}
		if bestDist < 0 || dist < bestDist {
			bestDist, bestID = dist, id
		}
	}
	return bestID, rows.Err()
}

// LoadArtifacts reads an account's inventory.
func LoadArtifacts(conn *sql.DB, accountID int64) ([]model.Artifact, error) {
	rows, err := conn.Query(`
		SELECT id, set_key, slot_key, rarity, level, main_stat, substats, location, locked, source
		FROM artifacts WHERE account_id = ? ORDER BY crit_value DESC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Artifact
	for rows.Next() {
		var (
			a   model.Artifact
			raw string
		)
		a.AccountID = accountID
		if err := rows.Scan(&a.ID, &a.SetKey, &a.SlotKey, &a.Rarity, &a.Level,
			&a.MainStat, &raw, &a.Location, &a.Lock, &a.Source); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &a.Substats); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
