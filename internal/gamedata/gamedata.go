// Package gamedata owns every number that changes when HoYoverse ships a
// patch: talent multipliers, ascension curves, weapon substat scaling,
// artifact set bonuses, substat roll values and the transformative-reaction
// level multiplier table.
//
// Design rule (see docs/ARCHITECTURE.md): the damage engine in package calc
// contains formulas only, never game constants. Everything version-dependent
// enters through this package so a patch is a data sync, not a code change —
// and so a wrong constant is one row in a table rather than a bug compiled
// into the engine.
//
// Source of truth: Dimbreath/AnimeGameData (ExcelBinOutput + TextMap), the
// same datamine the established Enka and GOOD tooling builds on. The nightly
// sync job writes a versioned snapshot into data/gamedata/<version>/ and
// flips a symlink, so a bad upstream commit can be rolled back instantly.
package gamedata

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kristianwind/mimir/internal/model"
)

// ErrMissing is returned when a lookup has no synced data behind it. The
// engine surfaces this to the user rather than guessing — a plausible wrong
// multiplier is worse than an honest gap.
var ErrMissing = errors.New("gamedata: not synced")

// Snapshot is one immutable version of the game data.
type Snapshot struct {
	// Version is the game version this snapshot was mined from, e.g. "5.3".
	Version string `json:"version"`
	// Characters is keyed by GOOD character key, e.g. "Neuvillette".
	Characters map[string]Character `json:"characters"`
	// Weapons is keyed by GOOD weapon key.
	Weapons map[string]Weapon `json:"weapons"`
	// ArtifactSets is keyed by GOOD set key, e.g. "MarechausseeHunter".
	ArtifactSets map[string]ArtifactSet `json:"artifactSets"`
	// LevelMultipliers maps character level -> transformative reaction level
	// multiplier. Mined from ReactionLevelExcelConfigData.
	LevelMultipliers map[int]float64 `json:"levelMultipliers"`
	// SubstatRolls maps rarity -> substat -> the four possible roll values.
	SubstatRolls map[int]map[model.Stat][]float64 `json:"substatRolls"`
	// MainStatValues maps rarity -> stat -> value at each level 0..20.
	MainStatValues map[int]map[model.Stat][]float64 `json:"mainStatValues"`
	// ReactionCoefficients maps a transformative reaction name to its damage
	// coefficient. Mined rather than hardcoded: HoYoverse has retuned these.
	ReactionCoefficients map[string]float64 `json:"reactionCoefficients"`
	// AvatarIDs maps Enka's numeric avatarId to the GOOD character key, and
	// WeaponIDs does the same for itemId. Enka speaks datamine ids; the rest
	// of Mimir speaks GOOD keys, and this is the only bridge between them.
	AvatarIDs map[int]string `json:"avatarIds"`
	WeaponIDs map[int]string `json:"weaponIds"`
	// SetIDs maps the datamine's numeric artifact set id to the GOOD set key.
	// Enka reports setId directly on every artifact, so this is the primary
	// bridge; SetNameHashes below is the fallback for payloads that carry
	// only the name hash.
	SetIDs map[int]string `json:"setIds"`
	// SetNameHashes maps a TextMap hash to the GOOD artifact set key.
	SetNameHashes map[string]string `json:"setNameHashes"`
	// TravelerDepots maps a skill-depot id to a GOOD character key. The
	// Traveler is one avatarId per gender with a depot per element, so the
	// avatarId alone cannot say whether a showcase holds TravelerDendro or
	// TravelerGeo — only the depot can.
	TravelerDepots map[int]string `json:"travelerDepots"`
	// Curves holds the shared growth curves, keyed by the datamine's curve
	// name (GROW_CURVE_ATTACK_S4 and friends). Index 0 is level 1.
	//
	// Storing the curves once and the curve *name* on each character keeps
	// the snapshot small: the alternative is 90 floats per stat per
	// character, repeated for the few dozen curves the game actually uses.
	Curves map[string][]float64 `json:"curves"`
	// ResinCosts maps an activity key ("artifact_domain", "talent_domain",
	// "world_boss", "weekly_boss", ...) to its resin price. Mined rather than
	// hardcoded because HoYoverse has repriced world bosses before.
	ResinCosts map[string]float64 `json:"resinCosts"`
	// Domains describes what each domain drops and when it is open, which is
	// what turns a ranked upgrade list into a weekday-aware farm plan.
	Domains map[string]Domain `json:"domains"`
	// DropModel holds the artifact drop distributions the farm simulator
	// samples from.
	DropModel DropModel `json:"dropModel"`
	// Effects are the bonuses that are not stat blocks — conversions and
	// conditionals. They cannot be mined, so each one cites the in-game
	// wording it came from and that citation is checked on load.
	Effects []EffectRule `json:"effects,omitempty"`
}

// Character is the static definition of a playable character.
type Character struct {
	Key        string        `json:"key"`
	Name       string        `json:"name"`
	Element    model.Element `json:"element"`
	WeaponType string        `json:"weaponType"`
	Rarity     int           `json:"rarity"`
	// Art is the suffix the game's own image names are built from —
	// "Shougun" for the Raiden Shogun. It is not a URL and not a file: the
	// server turns it into one when a page asks for the picture, so the
	// snapshot stays a description of the game rather than of a CDN.
	Art string `json:"art,omitempty"`
	// BaseHP/ATK/DEF are the level-1 values; the ascension curve below
	// expands them to any level/ascension pair.
	BaseHP  float64 `json:"baseHp"`
	BaseATK float64 `json:"baseAtk"`
	BaseDEF float64 `json:"baseDef"`
	// CurveHP/ATK/DEF name the growth curve in AvatarCurveExcelConfigData.
	CurveHP  string `json:"curveHp"`
	CurveATK string `json:"curveAtk"`
	CurveDEF string `json:"curveDef"`
	// AscensionStat is the stat gained on ascension (crit rate, EM, ...) and
	// AscensionBonus its value at each of the six ascension phases.
	AscensionStat  model.Stat `json:"ascensionStat"`
	AscensionBonus []float64  `json:"ascensionBonus"`
	// PromoteHP/ATK/DEF are the flat bumps at each ascension phase.
	PromoteHP  []float64 `json:"promoteHp"`
	PromoteATK []float64 `json:"promoteAtk"`
	PromoteDEF []float64 `json:"promoteDef"`
	// Talents holds the per-level tables, keyed TalentAuto, TalentSkill and
	// TalentBurst.
	Talents map[string]Talent `json:"talents"`
	// ProudSkillGroupIDs maps the three talents onto the proud-skill group
	// ids Enka reports constellation level bonuses against. It is what lets
	// an import check Mimir's own derivation against the game's answer.
	ProudSkillGroupIDs SkillIDs `json:"proudSkillGroupIds,omitempty"`
	// SkillIDs maps Enka's skillLevelMap keys onto the three talents. Enka
	// reports levels keyed by datamine skill id, so without this an import
	// cannot tell a level 9 skill from a level 9 burst.
	SkillIDs SkillIDs `json:"skillIds"`
	// Passives are the ascension passives, keyed "passive1".."passive3".
	// Their text is what the effect layer cites.
	Passives map[string]Described `json:"passives,omitempty"`
	// Constellations are C1..C6 in order.
	Constellations []Described `json:"constellations,omitempty"`
	// ConstellationTalentBonus records which talent each constellation
	// upgrades. Which constellation boosts which talent varies — Xiangling's
	// C3 raises her burst and Diluc's raises his skill — so it is mined per
	// character rather than assumed.
	ConstellationTalentBonus map[int]TalentBoost `json:"constellationTalentBonus,omitempty"`
	// AscensionMaterials and TalentMaterials drive the resin planner: which
	// domain, which boss, which day of the week.
	AscensionMaterials []MaterialCost `json:"ascensionMaterials"`
	TalentMaterials    []MaterialCost `json:"talentMaterials"`
}

// The three talent slots.
const (
	TalentAuto  = "auto"
	TalentSkill = "skill"
	TalentBurst = "burst"
)

// SkillIDs names the datamine skill ids for a character's three talents.
type SkillIDs struct {
	Auto  int `json:"auto"`
	Skill int `json:"skill"`
	Burst int `json:"burst"`
}

// TalentBoost is a constellation's upgrade to one talent.
type TalentBoost struct {
	// Slot is "auto", "skill" or "burst".
	Slot string `json:"slot"`
	// Levels is how many levels it adds, always three in practice but read
	// from the text rather than assumed.
	Levels int `json:"levels"`
	// MaxLevel is the ceiling the text states, normally 15.
	MaxLevel int `json:"maxLevel"`
}

// Talent is one ability's scaling table. Multipliers[i] is the value at
// talent level i+1, so a level 9 skill reads Multipliers[8].
type Talent struct {
	Name string `json:"name"`
	// Entries are the individual hits/instances of the ability, e.g.
	// "1-Hit DMG", "Charged Attack DMG", "Skill DMG".
	Entries []TalentEntry `json:"entries"`
}

// TalentEntry is one labelled row of a talent's stat table.
//
// Not every entry is damage: cooldowns, durations and particle counts live in
// the same table upstream and are kept, because the rotation editor and the
// energy calculator need them and dropping them would mean re-mining later.
// Unit says which is which.
type TalentEntry struct {
	// Label is the upstream text with its format placeholders removed,
	// e.g. "Press DMG" or "Skill CD".
	Label string `json:"label"`
	// Unit is "percent" for a scaling multiplier, "seconds" for a duration
	// or cooldown, and "flat" for anything else.
	Unit string `json:"unit"`
	// Scaling names the stat the multiplier applies to: ATK, HP or DEF.
	Scaling model.Stat `json:"scaling"`
	// Element is the damage type; empty means it inherits the character's.
	Element model.Element `json:"element"`
	// Category is the attack category this row belongs to, which decides
	// which DMG bonuses apply to it. It is not the same as the talent slot:
	// Raiden's burst table lists her Musou Isshin sword swings, and those
	// are normal attacks that pick up normal-attack bonuses, not burst ones.
	Category model.Category `json:"category"`
	// Multipliers is indexed by talent level - 1.
	Multipliers []float64 `json:"multipliers"`
}

// IsDamage reports whether this entry is a damage multiplier, which is the
// only kind the damage engine can consume.
func (t TalentEntry) IsDamage() bool {
	return t.Unit == "percent" && strings.Contains(strings.ToUpper(t.Label), "DMG")
}

// Multiplier returns the scaling value at a 1-based talent level.
func (t TalentEntry) Multiplier(level int) (float64, error) {
	if level < 1 || level > len(t.Multipliers) {
		return 0, fmt.Errorf("%w: talent %q level %d (have %d levels)",
			ErrMissing, t.Label, level, len(t.Multipliers))
	}
	return t.Multipliers[level-1], nil
}

// Weapon is the static definition of a weapon.
type Weapon struct {
	Key        string     `json:"key"`
	Name       string     `json:"name"`
	Type       string     `json:"weaponType"`
	Rarity     int        `json:"rarity"`
	BaseATK    float64    `json:"baseAtk"`
	CurveATK   string     `json:"curveAtk"`
	PromoteATK []float64  `json:"promoteAtk"`
	SubStat    model.Stat `json:"subStat"`
	SubValue   float64    `json:"subValue"`
	CurveSub   string     `json:"curveSub"`
	// PassiveStats are the refinement-scaled flat/percent bonuses the passive
	// grants unconditionally. Conditional passives live in the effect DSL —
	// see docs/DATAMODEL.md, "Conditional effects".
	PassiveStats []map[model.Stat]float64 `json:"passiveStats"`
	// PassiveName is the passive's in-game name, e.g. "Shanty".
	PassiveName string `json:"passiveName,omitempty"`
	// PassiveTexts is the passive's wording at each refinement, R1 first.
	// A weapon passive's numbers change with refinement, so an effect that
	// cites one has to cite the right one — and be checked against it.
	PassiveTexts []string `json:"passiveTexts,omitempty"`
}

// PassiveText returns the wording at a 1-based refinement level.
func (w Weapon) PassiveText(refinement int) (string, error) {
	i := refinement - 1
	if i < 0 || i >= len(w.PassiveTexts) {
		return "", fmt.Errorf("%w: %s passive text at R%d", ErrMissing, w.Key, refinement)
	}
	return w.PassiveTexts[i], nil
}

// ArtifactSet is the static definition of an artifact set.
type ArtifactSet struct {
	Key       string          `json:"key"`
	Name      string          `json:"name"`
	Rarities  []int           `json:"rarities"`
	TwoPiece  model.StatBlock `json:"twoPiece"`
	FourPiece model.StatBlock `json:"fourPiece"`
	// Conditional names four-piece bonuses that are not stat blocks, for
	// the effect layer to pick up.
	Conditional []string `json:"conditional"`
	// TwoPieceText and FourPieceText are the in-game descriptions. They are
	// mined so an effect can cite the exact wording its numbers come from,
	// and so that citation can be checked mechanically.
	TwoPieceText  string `json:"twoPieceText,omitempty"`
	FourPieceText string `json:"fourPieceText,omitempty"`
}

// Described is a named ability with its in-game text.
type Described struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Domain is a farmable domain.
type Domain struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	// Kind is "artifact", "talent", "weapon" or "boss".
	Kind string `json:"kind"`
	// Sets are the artifact sets an artifact domain drops (always two).
	Sets []string `json:"sets"`
	// Rewards are the material keys a talent or weapon domain drops.
	Rewards []string `json:"rewards"`
	// Days are the weekdays the domain is open, 0 = Sunday. An empty list
	// means every day.
	Days []int `json:"days"`
	// ResinCost is the resin per run.
	ResinCost float64 `json:"resinCost"`
}

// OpenOn reports whether the domain is open on a weekday (0 = Sunday).
func (d Domain) OpenOn(weekday int) bool {
	if len(d.Days) == 0 {
		return true
	}
	for _, w := range d.Days {
		if w == weekday {
			return true
		}
	}
	return false
}

// DropModel is everything the farm simulator needs to sample a realistic
// artifact from a domain run. All of it is mined; none of it is guessed.
type DropModel struct {
	// PiecesPerRun is the expected number of artifacts a domain run yields.
	PiecesPerRun float64 `json:"piecesPerRun"`
	// FiveStarChance is the probability a dropped piece is 5-star.
	FiveStarChance float64 `json:"fiveStarChance"`
	// SlotWeights is the relative chance of each slot. Uniform in practice,
	// but expressed as data so a future change is a sync, not a patch.
	SlotWeights map[model.Slot]float64 `json:"slotWeights"`
	// MainStatWeights maps slot -> stat -> relative weight.
	MainStatWeights map[model.Slot]map[model.Stat]float64 `json:"mainStatWeights"`
	// SubstatWeights is the relative chance of each substat being rolled.
	SubstatWeights map[model.Stat]float64 `json:"substatWeights"`
	// FourSubstatChance is the probability a 5-star drops with four substats
	// rather than three.
	FourSubstatChance float64 `json:"fourSubstatChance"`
	// RollsToMax is how many upgrade rolls a 5-star gains from +0 to +20,
	// including the initial substat count.
	RollsToMax int `json:"rollsToMax"`
}

// MaterialCost is one entry in an upgrade's material bill.
type MaterialCost struct {
	Item     string `json:"item"`
	Count    int    `json:"count"`
	Source   string `json:"source"`   // "domain" | "boss" | "weekly" | "world"
	SourceID string `json:"sourceId"` // domain or boss key
	// Days are the weekdays a talent/weapon domain is open (0 = Sunday).
	Days []int `json:"days"`
}

// LevelMultiplier returns the transformative-reaction level multiplier for a
// character level, or ErrMissing if the table has not been synced.
func (s *Snapshot) LevelMultiplier(level int) (float64, error) {
	v, ok := s.LevelMultipliers[level]
	if !ok {
		return 0, fmt.Errorf("%w: level multiplier for level %d "+
			"(run `mimir sync gamedata`)", ErrMissing, level)
	}
	return v, nil
}

// Char looks up a character definition.
func (s *Snapshot) Char(key string) (Character, error) {
	c, ok := s.Characters[key]
	if !ok {
		return Character{}, fmt.Errorf("%w: character %q", ErrMissing, key)
	}
	return c, nil
}

// AvatarKey maps an Enka avatarId and skill-depot id to a GOOD character key.
//
// The depot is consulted first because it is the only thing that resolves the
// Traveler; for everyone else it is absent from the table and the avatarId
// answers.
func (s *Snapshot) AvatarKey(id, skillDepotID int) (string, error) {
	if k, ok := s.TravelerDepots[skillDepotID]; ok {
		return k, nil
	}
	k, ok := s.AvatarIDs[id]
	if !ok {
		return "", fmt.Errorf("%w: avatarId %d (import a newer game data snapshot)", ErrMissing, id)
	}
	return k, nil
}

// WeaponKey maps an Enka itemId to a GOOD weapon key.
func (s *Snapshot) WeaponKey(id int) (string, error) {
	k, ok := s.WeaponIDs[id]
	if !ok {
		return "", fmt.Errorf("%w: weapon itemId %d", ErrMissing, id)
	}
	return k, nil
}

// SetKeyByID maps a datamine artifact set id to a GOOD set key.
func (s *Snapshot) SetKeyByID(id int) (string, error) {
	k, ok := s.SetIDs[id]
	if !ok {
		return "", fmt.Errorf("%w: artifact set id %d", ErrMissing, id)
	}
	return k, nil
}

// SetKey maps a TextMap name hash to a GOOD artifact set key.
func (s *Snapshot) SetKey(hash string) (string, error) {
	k, ok := s.SetNameHashes[hash]
	if !ok {
		return "", fmt.Errorf("%w: artifact set hash %s", ErrMissing, hash)
	}
	return k, nil
}

// Set looks up an artifact set definition.
func (s *Snapshot) Set(key string) (ArtifactSet, error) {
	a, ok := s.ArtifactSets[key]
	if !ok {
		return ArtifactSet{}, fmt.Errorf("%w: artifact set %q", ErrMissing, key)
	}
	return a, nil
}

// CurveValue returns a growth curve's multiplier at a 1-based level.
func (s *Snapshot) CurveValue(name string, level int) (float64, error) {
	curve, ok := s.Curves[name]
	if !ok {
		return 0, fmt.Errorf("%w: growth curve %q", ErrMissing, name)
	}
	if level < 1 || level > len(curve) {
		return 0, fmt.Errorf("%w: curve %q at level %d (have %d levels)",
			ErrMissing, name, level, len(curve))
	}
	return curve[level-1], nil
}

// BaseStats returns a character's white HP, ATK and DEF at a level and
// ascension phase.
//
//	stat = base × curve(level) + promote bonus at this ascension
//
// Level and ascension are separate arguments on purpose: a character at
// level 80 can be either pre- or post-ascension, and the two differ by the
// whole ascension bump. Deriving ascension from level would quietly report
// the wrong stats for every character sitting on an ascension boundary.
func (s *Snapshot) BaseStats(c Character, level, ascension int) (hp, atk, def float64, err error) {
	if ascension < 0 || ascension > 6 {
		return 0, 0, 0, fmt.Errorf("gamedata: ascension %d is out of range", ascension)
	}

	curveHP, err := s.CurveValue(c.CurveHP, level)
	if err != nil {
		return 0, 0, 0, err
	}
	curveATK, err := s.CurveValue(c.CurveATK, level)
	if err != nil {
		return 0, 0, 0, err
	}
	curveDEF, err := s.CurveValue(c.CurveDEF, level)
	if err != nil {
		return 0, 0, 0, err
	}

	return c.BaseHP*curveHP + at(c.PromoteHP, ascension),
		c.BaseATK*curveATK + at(c.PromoteATK, ascension),
		c.BaseDEF*curveDEF + at(c.PromoteDEF, ascension),
		nil
}

// AscensionStatValue returns the ascension-granted stat (crit rate, EM, ...)
// at an ascension phase.
func (c Character) AscensionStatValue(ascension int) float64 {
	return at(c.AscensionBonus, ascension)
}

// WeaponBaseATK returns a weapon's base attack at a level and ascension.
func (s *Snapshot) WeaponBaseATK(w Weapon, level, ascension int) (float64, error) {
	curve, err := s.CurveValue(w.CurveATK, level)
	if err != nil {
		return 0, err
	}
	return w.BaseATK*curve + at(w.PromoteATK, ascension), nil
}

// WeaponSubValue returns a weapon's secondary stat at a level.
func (s *Snapshot) WeaponSubValue(w Weapon, level int) (float64, error) {
	if w.SubStat == "" {
		return 0, nil
	}
	curve, err := s.CurveValue(w.CurveSub, level)
	if err != nil {
		return 0, err
	}
	return w.SubValue * curve, nil
}

// at reads a promote table defensively: tables are indexed by ascension
// phase, and a snapshot mined mid-patch can be one entry short.
func at(table []float64, i int) float64 {
	if i < 0 || i >= len(table) {
		return 0
	}
	return table[i]
}

// TalentEntry finds one labelled row of a character's talent by slot and
// label. Matching is case-insensitive and accepts a prefix, because upstream
// labels are long and a rotation should be able to say "Press DMG" rather
// than repeating the whole string.
func (c Character) TalentEntry(slot, label string) (TalentEntry, error) {
	t, ok := c.Talents[slot]
	if !ok {
		return TalentEntry{}, fmt.Errorf("%w: %s has no %q talent table", ErrMissing, c.Key, slot)
	}
	want := strings.ToLower(strings.TrimSpace(label))
	for _, e := range t.Entries {
		if strings.EqualFold(e.Label, label) {
			return e, nil
		}
	}
	for _, e := range t.Entries {
		if strings.HasPrefix(strings.ToLower(e.Label), want) {
			return e, nil
		}
	}
	have := make([]string, 0, len(t.Entries))
	for _, e := range t.Entries {
		have = append(have, e.Label)
	}
	return TalentEntry{}, fmt.Errorf("%s %s has no entry %q; it has %v", c.Key, slot, label, have)
}
