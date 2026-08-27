// Package model holds the domain vocabulary shared by every other package:
// elements, stats, artifact slots and the account-level records we persist.
//
// Nothing in here is version-dependent. Every number that changes when
// HoYoverse ships a patch lives in package gamedata and is synced from
// AnimeGameData — see docs/DATAMODEL.md, "Where the numbers live".
package model

import "strings"

// Element is a Genshin damage type. Physical is included because it shares
// the RES/DMG-bonus machinery even though it is not a reactable element.
type Element string

const (
	Pyro     Element = "pyro"
	Hydro    Element = "hydro"
	Anemo    Element = "anemo"
	Electro  Element = "electro"
	Dendro   Element = "dendro"
	Cryo     Element = "cryo"
	Geo      Element = "geo"
	Physical Element = "physical"
)

// Elements is the theme-selector order: the in-game element wheel.
var Elements = []Element{Pyro, Hydro, Anemo, Electro, Dendro, Cryo, Geo}

// Stat is a character statistic. The string values match the GOOD format's
// stat keys so artifact import needs no translation table.
type Stat string

const (
	HP               Stat = "hp"
	HPPercent        Stat = "hp_"
	ATK              Stat = "atk"
	ATKPercent       Stat = "atk_"
	DEF              Stat = "def"
	DEFPercent       Stat = "def_"
	ElementalMastery Stat = "eleMas"
	EnergyRecharge   Stat = "enerRech_"
	CritRate         Stat = "critRate_"
	CritDMG          Stat = "critDMG_"
	HealingBonus     Stat = "heal_"
	PyroDMG          Stat = "pyro_dmg_"
	HydroDMG         Stat = "hydro_dmg_"
	AnemoDMG         Stat = "anemo_dmg_"
	ElectroDMG       Stat = "electro_dmg_"
	DendroDMG        Stat = "dendro_dmg_"
	CryoDMG          Stat = "cryo_dmg_"
	GeoDMG           Stat = "geo_dmg_"
	PhysicalDMG      Stat = "physical_dmg_"

	// Damage bonuses that apply to a category of attack rather than to an
	// element. Raiden's Emblem set buffs her burst; her Musou Isshin sword
	// swings are normal attacks and do not benefit from it — which is
	// exactly the distinction these keys exist to make.
	AllDMG     Stat = "dmg_"
	NormalDMG  Stat = "normal_dmg_"
	ChargedDMG Stat = "charged_dmg_"
	PlungeDMG  Stat = "plunging_dmg_"
	SkillDMG   Stat = "skill_dmg_"
	BurstDMG   Stat = "burst_dmg_"
)

// Debuffs applied to the enemy rather than to the character. They travel in
// the same stat block because they come from the same places — set bonuses,
// constellations, passives — and separating them would mean two parallel
// effect systems.
//
// Viridescent Venerer's four-piece is the canonical example, and it is one of
// the most consequential bonuses in the game: without somewhere to put it, an
// anemo build's numbers are unreachably wrong.
const (
	TargetDefIgnore    Stat = "target_defIgnore_"
	TargetDefReduction Stat = "target_defRed_"
)

// TargetResShred is the resistance reduction applied to one element.
func TargetResShred(e Element) Stat {
	return Stat("target_res_" + string(e))
}

// Category classifies a damage instance for the purpose of category-specific
// DMG bonuses.
type Category string

const (
	CategoryNormal  Category = "normal"
	CategoryCharged Category = "charged"
	CategoryPlunge  Category = "plunge"
	CategorySkill   Category = "skill"
	CategoryBurst   Category = "burst"
)

// CategoryScoped narrows a stat to one attack category, producing keys like
// "burst_critRate_".
//
// The Catch raises Elemental Burst crit rate and nothing else; charging a
// character's normal attacks with it would be a straightforward lie. Scoped
// keys are how a bonus stays attached to the attacks it actually applies to.
func CategoryScoped(c Category, s Stat) Stat {
	if c == "" || s == "" {
		return ""
	}
	return Stat(string(c) + "_" + string(s))
}

// CategoryDMGStat returns the DMG bonus stat for an attack category.
func CategoryDMGStat(c Category) Stat {
	switch c {
	case CategoryNormal:
		return NormalDMG
	case CategoryCharged:
		return ChargedDMG
	case CategoryPlunge:
		return PlungeDMG
	case CategorySkill:
		return SkillDMG
	case CategoryBurst:
		return BurstDMG
	default:
		return ""
	}
}

// Substats are the ten rollable artifact sub-statistics.
var Substats = []Stat{
	HP, HPPercent, ATK, ATKPercent, DEF, DEFPercent,
	ElementalMastery, EnergyRecharge, CritRate, CritDMG,
}

// DMGBonusStat returns the elemental DMG bonus stat for an element.
func DMGBonusStat(e Element) Stat {
	switch e {
	case Pyro:
		return PyroDMG
	case Hydro:
		return HydroDMG
	case Anemo:
		return AnemoDMG
	case Electro:
		return ElectroDMG
	case Dendro:
		return DendroDMG
	case Cryo:
		return CryoDMG
	case Geo:
		return GeoDMG
	default:
		return PhysicalDMG
	}
}

// fightProps maps the datamine's FIGHT_PROP_* names onto Mimir stats.
//
// These are identifier names rather than balance numbers, so they belong in
// code: they have been stable since release, and a rename breaks parsing
// loudly instead of shifting a multiplier quietly.
var fightProps = map[string]Stat{
	"FIGHT_PROP_HP":                HP,
	"FIGHT_PROP_BASE_HP":           HP,
	"FIGHT_PROP_HP_PERCENT":        HPPercent,
	"FIGHT_PROP_ATTACK":            ATK,
	"FIGHT_PROP_BASE_ATTACK":       ATK,
	"FIGHT_PROP_ATTACK_PERCENT":    ATKPercent,
	"FIGHT_PROP_DEFENSE":           DEF,
	"FIGHT_PROP_BASE_DEFENSE":      DEF,
	"FIGHT_PROP_DEFENSE_PERCENT":   DEFPercent,
	"FIGHT_PROP_ELEMENT_MASTERY":   ElementalMastery,
	"FIGHT_PROP_CHARGE_EFFICIENCY": EnergyRecharge,
	"FIGHT_PROP_CRITICAL":          CritRate,
	"FIGHT_PROP_CRITICAL_HURT":     CritDMG,
	"FIGHT_PROP_HEAL_ADD":          HealingBonus,
	"FIGHT_PROP_FIRE_ADD_HURT":     PyroDMG,
	"FIGHT_PROP_WATER_ADD_HURT":    HydroDMG,
	"FIGHT_PROP_WIND_ADD_HURT":     AnemoDMG,
	"FIGHT_PROP_ELEC_ADD_HURT":     ElectroDMG,
	"FIGHT_PROP_GRASS_ADD_HURT":    DendroDMG,
	"FIGHT_PROP_ICE_ADD_HURT":      CryoDMG,
	"FIGHT_PROP_ROCK_ADD_HURT":     GeoDMG,
	"FIGHT_PROP_PHYSICAL_ADD_HURT": PhysicalDMG,
}

// StatFromFightProp resolves a datamine FIGHT_PROP_* name.
//
// The BASE_ variants fold onto the same key as their flat counterparts: a
// character's base ATK and a weapon's flat ATK bonus add into one number, and
// keeping them apart in the stat block would only invite forgetting one.
func StatFromFightProp(name string) (Stat, bool) {
	s, ok := fightProps[name]
	return s, ok
}

// ElementFromDatamine maps the datamine's element names onto Mimir elements.
func ElementFromDatamine(name string) (Element, bool) {
	switch name {
	case "Fire":
		return Pyro, true
	case "Water":
		return Hydro, true
	case "Wind":
		return Anemo, true
	case "Electric":
		return Electro, true
	case "Grass":
		return Dendro, true
	case "Ice":
		return Cryo, true
	case "Rock":
		return Geo, true
	}
	// The second name source writes the element the way the game shows it to
	// a player. Accepting both spellings here rather than translating at the
	// call site keeps one place that knows what an element is called.
	switch Element(strings.ToLower(name)) {
	case Pyro, Hydro, Anemo, Electro, Dendro, Cryo, Geo:
		return Element(strings.ToLower(name)), true
	}
	return "", false
}

// ReactionBonus returns the stat key holding the additive reaction-damage
// bonus for a named reaction, e.g. Crimson Witch's +40% to vaporize. Reaction
// bonuses are per-reaction in game (Crimson Witch buffs vaporize and melt but
// not bloom), so they cannot share one key.
func ReactionBonus(reaction string) Stat {
	return Stat("react_" + reaction + "_")
}

// Slot is an artifact slot. Values match GOOD's slotKey.
type Slot string

const (
	Flower  Slot = "flower"
	Plume   Slot = "plume"
	Sands   Slot = "sands"
	Goblet  Slot = "goblet"
	Circlet Slot = "circlet"
)

// Slots is the canonical slot order used by the optimizer and the UI.
var Slots = []Slot{Flower, Plume, Sands, Goblet, Circlet}

// StatBlock is a sparse bag of stats. Absent keys are zero.
// It is the currency between the artifact layer and the damage engine.
type StatBlock map[Stat]float64

// Add returns a new block with the sum of a and b.
func (s StatBlock) Add(other StatBlock) StatBlock {
	out := make(StatBlock, len(s)+len(other))
	for k, v := range s {
		out[k] = v
	}
	for k, v := range other {
		out[k] += v
	}
	return out
}

// AddInto mutates s in place. Used on the optimizer's hot path to avoid
// allocating a map per candidate combination.
func (s StatBlock) AddInto(other StatBlock) {
	for k, v := range other {
		s[k] += v
	}
}

// Clone returns an independent copy.
func (s StatBlock) Clone() StatBlock {
	out := make(StatBlock, len(s))
	for k, v := range s {
		out[k] = v
	}
	return out
}

// Substat is one rolled sub-statistic on an artifact.
type Substat struct {
	Key   Stat    `json:"key"`
	Value float64 `json:"value"`
}

// Artifact is one owned piece. IDs are ours; GOODID preserves the id from an
// imported .good file so re-imports update rather than duplicate.
type Artifact struct {
	ID        int64     `json:"id"`
	AccountID int64     `json:"accountId"`
	SetKey    string    `json:"setKey"`
	SlotKey   Slot      `json:"slotKey"`
	Rarity    int       `json:"rarity"`
	Level     int       `json:"level"`
	MainStat  Stat      `json:"mainStatKey"`
	Substats  []Substat `json:"substats"`
	Location  string    `json:"location"` // character key, or "" if unequipped
	Lock      bool      `json:"lock"`
	Source    string    `json:"source"` // "good" | "enka" | "manual"
}

// CritValue is the standard artifact quality heuristic: CV = 2·CR% + CD%.
// It ignores everything except crit, so it is a triage tool, not a verdict —
// the optimizer is the verdict.
func (a Artifact) CritValue() float64 {
	var cv float64
	for _, s := range a.Substats {
		switch s.Key {
		case CritRate:
			cv += 2 * s.Value
		case CritDMG:
			cv += s.Value
		}
	}
	return cv
}

// Weapon is one owned weapon.
type Weapon struct {
	ID         int64  `json:"id"`
	AccountID  int64  `json:"accountId"`
	Key        string `json:"key"`
	Level      int    `json:"level"`
	Ascension  int    `json:"ascension"`
	Refinement int    `json:"refinement"`
	Location   string `json:"location"`
	Lock       bool   `json:"lock"`
	Source     string `json:"source"`
}

// Character is one owned character's progression state.
type Character struct {
	ID            int64  `json:"id"`
	AccountID     int64  `json:"accountId"`
	Key           string `json:"key"`
	Level         int    `json:"level"`
	Ascension     int    `json:"ascension"`
	Constellation int    `json:"constellation"`
	TalentAuto    int    `json:"talentAuto"`
	TalentSkill   int    `json:"talentSkill"`
	TalentBurst   int    `json:"talentBurst"`
	Source        string `json:"source"`
}

// Account is one game account (UID). A user may own several — EU and Asia
// servers are separate accounts with separate inventories.
type Account struct {
	ID       int64  `json:"id"`
	UserID   int64  `json:"userId"`
	UID      string `json:"uid"`
	Nickname string `json:"nickname"`
	Region   string `json:"region"`
	ARLevel  int    `json:"arLevel"`
	WLLevel  int    `json:"wlLevel"`
}
