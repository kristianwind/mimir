// Package good reads and writes the GOOD format (Genshin Open Object
// Description) — the JSON interchange used by Genshin Optimizer, Inventory
// Kamera, Amenoma and the rest of the ecosystem.
//
// Supporting GOOD as the primary bulk import is what makes Mimir usable on
// day one: nobody will type in 1,400 artifacts, but everybody already has a
// scanner that emits this file, and anyone coming from Genshin Optimizer can
// migrate in one drag-and-drop.
package good

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/kristianwind/mimir/internal/model"
)

// File is a GOOD document.
type File struct {
	Format     string         `json:"format"`
	Version    int            `json:"version"`
	Source     string         `json:"source"`
	Characters []Character    `json:"characters,omitempty"`
	Artifacts  []Artifact     `json:"artifacts,omitempty"`
	Weapons    []Weapon       `json:"weapons,omitempty"`
	Materials  map[string]int `json:"materials,omitempty"`
}

// Character is a GOOD character entry.
type Character struct {
	Key           string `json:"key"`
	Level         int    `json:"level"`
	Constellation int    `json:"constellation"`
	Ascension     int    `json:"ascension"`
	Talent        struct {
		Auto  int `json:"auto"`
		Skill int `json:"skill"`
		Burst int `json:"burst"`
	} `json:"talent"`
}

// Artifact is a GOOD artifact entry. Percent-valued stats are expressed in
// display units here: 9.9 means 9.9%, not 990%.
type Artifact struct {
	SetKey      string `json:"setKey"`
	SlotKey     string `json:"slotKey"`
	Level       int    `json:"level"`
	Rarity      int    `json:"rarity"`
	MainStatKey string `json:"mainStatKey"`
	Location    string `json:"location"`
	Lock        bool   `json:"lock"`
	Substats    []struct {
		Key   string  `json:"key"`
		Value float64 `json:"value"`
	} `json:"substats"`
}

// Weapon is a GOOD weapon entry.
type Weapon struct {
	Key        string `json:"key"`
	Level      int    `json:"level"`
	Ascension  int    `json:"ascension"`
	Refinement int    `json:"refinement"`
	Location   string `json:"location"`
	Lock       bool   `json:"lock"`
}

// IsPercent reports whether a stat is stored as a percentage in GOOD and must
// be divided by 100 before it reaches the damage engine, which works in
// fractions throughout.
//
// The rule is the trailing underscore in the GOOD key — that convention is
// what the whole ecosystem uses to distinguish flat ATK from ATK%.
func IsPercent(s model.Stat) bool {
	return strings.HasSuffix(string(s), "_")
}

// Normalize converts a GOOD display value into the engine's fraction.
func Normalize(s model.Stat, v float64) float64 {
	if IsPercent(s) {
		return v / 100
	}
	return v
}

// Denormalize is the inverse, for export.
func Denormalize(s model.Stat, v float64) float64 {
	if IsPercent(s) {
		return v * 100
	}
	return v
}

// maxGOODVersion is the newest format version this parser has been read
// against, rather than the newest it can technically decode.
//
// Versions 2 and 3 carry the same records. Genshin Optimizer's own schema
// accepts 1, 2 and 3 through one definition and its importer branches on none
// of them; what 3 added is optional fields on an artifact — initialValue on a
// substat, totalRolls, astralMark, elixirCrafted, unactivatedSubstats — none
// of which change the ones Mimir reads. Go ignores unknown fields, so a v3
// file decodes into the structs below unchanged.
//
// The check stays an equality-with-a-ceiling rather than becoming "anything
// recent enough", because the next version may be the one that renames a stat
// key, and finding that out from a silently wrong inventory is the failure this
// whole file is arranged to avoid.
const maxGOODVersion = 3

// The two ways a version can be wrong. They are separate errors because the
// remedies are opposite: one is "re-export that file", the other is "the file
// is fine, Mimir is behind".
var (
	ErrTooOld = errors.New("good: this file is older than the format in use")
	ErrTooNew = errors.New("good: this file is newer than Mimir knows")
)

// Parse reads a GOOD document and validates the envelope.
func Parse(r io.Reader) (*File, error) {
	var f File
	dec := json.NewDecoder(r)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("good: parse: %w", err)
	}
	if f.Format != "GOOD" {
		return nil, fmt.Errorf("good: not a GOOD file (format = %q)", f.Format)
	}
	switch {
	case f.Version < 2:
		// Version 1 predates the current slot and stat keys. Refusing is
		// kinder than importing an inventory with silently wrong stat names.
		return nil, fmt.Errorf(
			"%w: version %d — its slot and stat keys are not the ones in use today",
			ErrTooOld, f.Version)
	case f.Version > maxGOODVersion:
		// Newer than anything checked. The envelope has been stable, so this
		// is likely to work — but "likely" is not something to decide on the
		// player's behalf about their whole inventory.
		return nil, fmt.Errorf(
			"%w: version %d, and Mimir has been checked against %d",
			ErrTooNew, f.Version, maxGOODVersion)
	}
	return &f, nil
}

// Import converts a parsed GOOD file into domain records for one account.
// Values are normalized to engine units on the way through.
func (f *File) Import(accountID int64) ([]model.Character, []model.Weapon, []model.Artifact) {
	chars := make([]model.Character, 0, len(f.Characters))
	for _, c := range f.Characters {
		chars = append(chars, model.Character{
			AccountID:     accountID,
			Key:           c.Key,
			Level:         c.Level,
			Ascension:     c.Ascension,
			Constellation: c.Constellation,
			TalentAuto:    c.Talent.Auto,
			TalentSkill:   c.Talent.Skill,
			TalentBurst:   c.Talent.Burst,
			Source:        "good",
		})
	}

	weapons := make([]model.Weapon, 0, len(f.Weapons))
	for _, w := range f.Weapons {
		weapons = append(weapons, model.Weapon{
			AccountID:  accountID,
			Key:        w.Key,
			Level:      w.Level,
			Ascension:  w.Ascension,
			Refinement: w.Refinement,
			Location:   w.Location,
			Lock:       w.Lock,
			Source:     "good",
		})
	}

	arts := make([]model.Artifact, 0, len(f.Artifacts))
	for _, a := range f.Artifacts {
		subs := make([]model.Substat, 0, len(a.Substats))
		for _, s := range a.Substats {
			if s.Key == "" || s.Value == 0 {
				// Scanners pad unrolled substats with empty entries.
				continue
			}
			key := model.Stat(s.Key)
			subs = append(subs, model.Substat{Key: key, Value: Normalize(key, s.Value)})
		}
		arts = append(arts, model.Artifact{
			AccountID: accountID,
			SetKey:    a.SetKey,
			SlotKey:   model.Slot(a.SlotKey),
			Rarity:    a.Rarity,
			Level:     a.Level,
			MainStat:  model.Stat(a.MainStatKey),
			Substats:  subs,
			Location:  a.Location,
			Lock:      a.Lock,
			Source:    "good",
		})
	}

	return chars, weapons, arts
}
