package mine

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// Report is the outcome of validating a mined snapshot.
//
// The split matters: an error means the snapshot would make Mimir silently
// wrong and must not be written; a warning means a feature will refuse to run
// and say why. Shipping a snapshot that is merely incomplete is fine, because
// the engine errors on a missing table rather than guessing. Shipping one
// that is subtly *wrong* is not recoverable.
type Report struct {
	Errors   []string
	Warnings []string
}

func (r *Report) errf(format string, args ...any) {
	r.Errors = append(r.Errors, fmt.Sprintf(format, args...))
}

func (r *Report) warnf(format string, args ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

// Validate checks a snapshot for the failures that experience says actually
// happen: a desynced name source, a curve table that did not expand, an
// artifact stat table that came back empty.
func Validate(snap *gamedata.Snapshot) Report {
	var r Report

	if snap.Version == "" {
		r.errf("snapshot has no version")
	}
	if len(snap.Characters) < 50 {
		r.errf("only %d characters resolved; the name source has desynced from the datamine",
			len(snap.Characters))
	}
	if len(snap.Weapons) < 100 {
		r.errf("only %d weapons resolved; the name source has desynced from the datamine",
			len(snap.Weapons))
	}
	if len(snap.ArtifactSets) < 20 {
		r.errf("only %d artifact sets resolved", len(snap.ArtifactSets))
	}

	// Every mapped id must point at a character that actually exists, or an
	// Enka import will resolve a key and then fail to find its definition.
	for id, key := range snap.AvatarIDs {
		if _, ok := snap.Characters[key]; !ok {
			r.errf("avatarId %d maps to %q, which has no character definition", id, key)
		}
	}
	for id, key := range snap.SetIDs {
		if _, ok := snap.ArtifactSets[key]; !ok {
			r.errf("artifact set id %d maps to %q, which has no set definition", id, key)
		}
	}

	// A character whose curve is missing produces zero base stats, which
	// looks like a very weak character rather than an error.
	var noCurve, noSkills, noAscension int
	for key, c := range snap.Characters {
		for _, name := range []string{c.CurveHP, c.CurveATK, c.CurveDEF} {
			if name == "" || len(snap.Curves[name]) == 0 {
				noCurve++
				r.errf("character %q references growth curve %q, which is missing", key, name)
				break
			}
		}
		if c.BaseHP <= 0 || c.BaseATK <= 0 || c.BaseDEF <= 0 {
			r.errf("character %q has a zero base stat (%v/%v/%v)", key, c.BaseHP, c.BaseATK, c.BaseDEF)
		}
		if c.SkillIDs.Auto == 0 || c.SkillIDs.Skill == 0 || c.SkillIDs.Burst == 0 {
			noSkills++
		}
		if c.AscensionStat == "" {
			noAscension++
		}
		if c.Element == "" {
			r.warnf("character %q has no element", key)
		}
	}
	if noSkills > 0 {
		r.warnf("%d characters have incomplete skill ids; their talent levels will import as zero", noSkills)
	}
	if noAscension > 0 {
		r.warnf("%d characters have no ascension stat", noAscension)
	}

	// Artifact stat tables: the optimizer and the farm simulator both stop
	// dead without these, so check the shape rather than just the presence.
	for _, rarity := range []int{4, 5} {
		byStat, ok := snap.MainStatValues[rarity]
		if !ok || len(byStat) == 0 {
			r.errf("no main stat values for %d★ artifacts", rarity)
			continue
		}
		maxLevel := 20
		if rarity == 4 {
			maxLevel = 16
		}
		for stat, curve := range byStat {
			if len(curve) <= maxLevel {
				r.errf("%d★ main stat %q has %d levels, expected at least %d",
					rarity, stat, len(curve), maxLevel+1)
			}
		}
	}
	if rolls, ok := snap.SubstatRolls[5]; !ok || len(rolls) < len(model.Substats) {
		r.errf("5★ substat rolls cover %d of %d substats", len(rolls), len(model.Substats))
	} else {
		for _, stat := range model.Substats {
			if n := len(rolls[stat]); n != 4 {
				r.warnf("5★ substat %q has %d roll values, expected 4", stat, n)
			}
		}
	}

	if _, err := snap.LevelMultiplier(90); err != nil {
		r.errf("no reaction level multiplier at level 90")
	}

	// The tables the datamine does not carry. These are warnings, not
	// errors: the features that need them refuse to run and say so.
	if len(snap.ReactionCoefficients) == 0 {
		r.warnf("no reaction coefficients; transformative reaction damage is unavailable")
	}
	if snap.DropModel.PiecesPerRun <= 0 {
		r.warnf("no artifact drop model; the farm simulator is unavailable")
	}
	if len(snap.Domains) == 0 {
		r.warnf("no domains; the resin planner is unavailable")
	}
	if len(snap.Effects) == 0 {
		r.warnf("no effect rules; conditional set bonuses and conversion passives " +
			"are missing, which understates every build that relies on them")
	}
	if len(snap.ResinCosts) == 0 {
		r.warnf("no resin costs; upgrades cannot be ranked per resin")
	}
	if len(snap.ArtifactRolls) == 0 {
		r.warnf("no artifact roll counts; a target build cannot be described, " +
			"because there would be no consistent substat treatment to compare candidates under")
	}
	if len(snap.Materials) == 0 {
		r.warnf("no material catalogue; upgrade bills cannot be read and " +
			"every ascension and talent level is a cost with no description")
	} else {
		// A bill is only useful if every material in it can be placed. One
		// that cannot is reported by name, because it is a gap in a second
		// source rather than a mistake in the numbers.
		unplaced := map[string]bool{}
		for _, c := range snap.Characters {
			bills := append([]gamedata.Bill{}, c.AscensionBills...)
			for _, perSlot := range c.TalentBills {
				bills = append(bills, perSlot...)
			}
			for _, b := range bills {
				for _, item := range b.Items {
					mat, ok := snap.Materials[item.ID]
					if !ok {
						unplaced[fmt.Sprintf("item %d", item.ID)] = true
					} else if mat.Source == gamedata.SourceUnknown {
						unplaced[mat.Name] = true
					}
				}
			}
		}
		if len(unplaced) > 0 {
			names := make([]string, 0, len(unplaced))
			for n := range unplaced {
				names = append(names, n)
			}
			sort.Strings(names)
			if len(names) > 6 {
				names = append(names[:6], fmt.Sprintf("and %d more", len(unplaced)-6))
			}
			r.warnf("%d materials in the upgrade bills have no known source (%s); "+
				"those upgrades cannot say where to go",
				len(unplaced), strings.Join(names, ", "))
		}
	}
	withBills := 0
	for _, c := range snap.Characters {
		if len(c.TalentBills) > 0 {
			withBills++
		}
	}
	if withBills < len(snap.Characters) {
		r.warnf("%d of %d characters have no talent bill; their talent levels "+
			"cannot be costed", len(snap.Characters)-withBills, len(snap.Characters))
	}

	return r
}

// Supplements holds the tables that are not in the datamine in any usable
// form and have to be supplied alongside it.
//
// Keeping them in a separate, hand-maintained file rather than inventing
// plausible numbers in code is the whole point: a value here has a stated
// provenance, and a missing one makes its feature refuse to run instead of
// producing a confident wrong answer.
type Supplements struct {
	ReactionCoefficients map[string]float64         `json:"reactionCoefficients"`
	ResinCosts           map[string]float64         `json:"resinCosts"`
	ArtifactRolls        map[int]int                `json:"artifactRolls"`
	Domains              map[string]gamedata.Domain `json:"domains"`
	DropModel            *gamedata.DropModel        `json:"dropModel"`
}

// MergeSupplements loads a supplements file into a snapshot.
func MergeSupplements(snap *gamedata.Snapshot, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("mine: read supplements: %w", err)
	}
	var s Supplements
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("mine: parse supplements: %w", err)
	}

	if len(s.ReactionCoefficients) > 0 {
		snap.ReactionCoefficients = s.ReactionCoefficients
	}
	if len(s.ResinCosts) > 0 {
		snap.ResinCosts = s.ResinCosts
	}
	if len(s.ArtifactRolls) > 0 {
		snap.ArtifactRolls = s.ArtifactRolls
	}
	if len(s.Domains) > 0 {
		snap.Domains = s.Domains
	}
	if s.DropModel != nil {
		snap.DropModel = *s.DropModel
	}
	applyResinCosts(snap)

	// A supplement naming a set that does not exist is a typo in the
	// hand-maintained file, and it would silently produce a domain that
	// drops nothing.
	for key, d := range snap.Domains {
		for _, set := range d.Sets {
			if _, ok := snap.ArtifactSets[set]; !ok {
				return fmt.Errorf("mine: domain %q drops unknown artifact set %q", key, set)
			}
		}
	}
	return nil
}
