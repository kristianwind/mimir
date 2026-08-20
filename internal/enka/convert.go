package enka

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// equipTypes maps the datamine's slot names to Mimir slots.
var equipTypes = map[string]model.Slot{
	"EQUIP_BRACER":   model.Flower,
	"EQUIP_NECKLACE": model.Plume,
	"EQUIP_SHOES":    model.Sands,
	"EQUIP_RING":     model.Goblet,
	"EQUIP_DRESS":    model.Circlet,
}

// Import converts a showcase into domain records.
//
// Unmappable entries are skipped and reported in warnings rather than
// failing the whole import: a single unreleased character in a showcase
// should not stop the other seven from landing.
type ImportResult struct {
	Account    model.Account
	Characters []model.Character
	Weapons    []model.Weapon
	Artifacts  []model.Artifact
	Warnings   []string
}

// Import maps a fetched response onto Mimir's records for one user.
func (r *Response) Import(userID int64, snap *gamedata.Snapshot) ImportResult {
	out := ImportResult{
		Account: model.Account{
			UserID:   userID,
			UID:      r.UID,
			Nickname: r.PlayerInfo.Nickname,
			Region:   Region(r.UID),
			ARLevel:  r.PlayerInfo.Level,
			WLLevel:  r.PlayerInfo.WorldLevel,
		},
	}
	warn := func(format string, args ...any) {
		out.Warnings = append(out.Warnings, fmt.Sprintf(format, args...))
	}

	for _, av := range r.AvatarInfoList {
		key, err := snap.AvatarKey(av.AvatarID, av.SkillDepotID)
		if err != nil {
			warn("skipped avatarId %d: %v", av.AvatarID, err)
			continue
		}
		def, err := snap.Char(key)
		if err != nil {
			warn("skipped %s: %v", key, err)
			continue
		}

		level := intProp(av.PropMap, "4001")
		ascension := intProp(av.PropMap, "1002")

		// Talent levels here are base levels. Constellation bonuses (C3/C5
		// grant +3) are stored separately as the constellation count and
		// applied by the engine, so a re-import never compounds them.
		ch := model.Character{
			Key:           key,
			Level:         level,
			Ascension:     ascension,
			Constellation: len(av.TalentIDList),
			TalentAuto:    skillLevel(av.SkillLevelMap, def.SkillIDs.Auto),
			TalentSkill:   skillLevel(av.SkillLevelMap, def.SkillIDs.Skill),
			TalentBurst:   skillLevel(av.SkillLevelMap, def.SkillIDs.Burst),
			Source:        "enka",
		}
		out.Characters = append(out.Characters, ch)

		// Enka reports the constellation talent bonuses the game actually
		// applied. Mimir derives the same thing from constellation text, so
		// the two can be compared — and a silent divergence in a mining
		// heuristic is exactly the kind of error that otherwise ships.
		for _, warning := range checkTalentBonus(def, ch, av.ProudSkillExtraLevelMap) {
			warn("%s: %s", key, warning)
		}

		for _, eq := range av.EquipList {
			switch {
			case eq.Reliquary != nil:
				a, err := artifact(eq, key, snap)
				if err != nil {
					warn("skipped an artifact on %s: %v", key, err)
					continue
				}
				out.Artifacts = append(out.Artifacts, a)
			case eq.Weapon != nil:
				wKey, err := snap.WeaponKey(eq.ItemID)
				if err != nil {
					warn("skipped a weapon on %s: %v", key, err)
					continue
				}
				out.Weapons = append(out.Weapons, model.Weapon{
					Key:        wKey,
					Level:      eq.Weapon.Level,
					Ascension:  eq.Weapon.PromoteLevel,
					Refinement: refinement(eq.Weapon.AffixMap),
					Location:   key,
					Source:     "enka",
				})
			}
		}
	}
	return out
}

func artifact(eq Equip, location string, snap *gamedata.Snapshot) (model.Artifact, error) {
	slot, ok := equipTypes[eq.Flat.EquipType]
	if !ok {
		return model.Artifact{}, fmt.Errorf("unknown equip type %q", eq.Flat.EquipType)
	}
	if eq.Flat.ReliquaryMainstat == nil {
		return model.Artifact{}, fmt.Errorf("artifact in slot %s has no main stat", slot)
	}
	main, ok := model.StatFromFightProp(eq.Flat.ReliquaryMainstat.MainPropID)
	if !ok {
		return model.Artifact{}, fmt.Errorf("unknown main stat %q", eq.Flat.ReliquaryMainstat.MainPropID)
	}
	// Enka reports the numeric set id alongside the name hash. The id is the
	// better bridge: it survives a rename and it is what the datamine keys
	// sets by, so the hash is only a fallback for older payloads.
	setKey, err := snap.SetKeyByID(eq.Flat.SetID)
	if err != nil {
		setKey, err = snap.SetKey(eq.Flat.SetNameTextMapHash)
		if err != nil {
			return model.Artifact{}, err
		}
	}

	subs := make([]model.Substat, 0, 4)
	for _, s := range eq.Flat.ReliquarySubstats {
		key, ok := model.StatFromFightProp(s.AppendPropID)
		if !ok {
			return model.Artifact{}, fmt.Errorf("unknown substat %q", s.AppendPropID)
		}
		subs = append(subs, model.Substat{Key: key, Value: normalize(key, s.StatValue)})
	}

	// Enka reports artifact level 1-based: a fully upgraded +20 reads 21.
	level := eq.Reliquary.Level - 1
	if level < 0 {
		level = 0
	}

	return model.Artifact{
		SetKey:   setKey,
		SlotKey:  slot,
		Rarity:   eq.Flat.RankLevel,
		Level:    level,
		MainStat: main,
		Substats: subs,
		Location: location,
		Lock:     true, // equipped pieces are effectively locked
		Source:   "enka",
	}, nil
}

// normalize converts Enka's display units into engine fractions, using the
// same trailing-underscore rule as the GOOD format.
func normalize(s model.Stat, v float64) float64 {
	if strings.HasSuffix(string(s), "_") {
		return v / 100
	}
	return v
}

func intProp(props map[string]Prop, key string) int {
	p, ok := props[key]
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(p.Val)
	if err != nil {
		return 0
	}
	return n
}

func skillLevel(levels map[string]int, id int) int {
	if id == 0 {
		return 0
	}
	return levels[strconv.Itoa(id)]
}

// refinement converts Enka's zero-based affix level to R1-R5.
func refinement(affix map[string]int) int {
	for _, v := range affix {
		return v + 1
	}
	return 1
}

// checkTalentBonus compares Mimir's derived constellation talent bonuses
// against the ones the game reports for this character.
func checkTalentBonus(def gamedata.Character, ch model.Character, extras map[string]int) []string {
	if len(extras) == 0 && len(def.ConstellationTalentBonus) == 0 {
		return nil
	}

	derived := map[string]int{}
	for constellation, boost := range def.ConstellationTalentBonus {
		if ch.Constellation >= constellation {
			derived[boost.Slot] += boost.Levels
		}
	}

	reported := map[string]int{}
	for slot, group := range map[string]int{
		gamedata.TalentAuto:  def.ProudSkillGroupIDs.Auto,
		gamedata.TalentSkill: def.ProudSkillGroupIDs.Skill,
		gamedata.TalentBurst: def.ProudSkillGroupIDs.Burst,
	} {
		if group == 0 {
			continue
		}
		if extra, ok := extras[strconv.Itoa(group)]; ok {
			reported[slot] = extra
		}
	}
	if len(reported) == 0 {
		// Nothing to compare against — the character has no bonuses active,
		// or the group ids were not mined.
		return nil
	}

	var out []string
	for _, slot := range []string{gamedata.TalentAuto, gamedata.TalentSkill, gamedata.TalentBurst} {
		if derived[slot] != reported[slot] {
			out = append(out, fmt.Sprintf(
				"constellation bonus for %s reads +%d from the game but +%d from the game data; "+
					"talent multipliers for this character may be off",
				slot, reported[slot], derived[slot]))
		}
	}
	return out
}
