package mine

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/kristianwind/mimir/internal/gamedata"
)

// levelBoostPattern matches the standard constellation wording:
// "Increases the Level of Pyronado by 3."
var levelBoostPattern = regexp.MustCompile(`(?i)increases the level of (.+?) by (\d+)`)

// maxLevelPattern matches the ceiling the same text states.
var maxLevelPattern = regexp.MustCompile(`(?i)maximum upgrade level is (\d+)`)

// talentBoosts reads which talents a character's constellations upgrade.
//
// Which constellation boosts which talent is not a rule: Xiangling's C3
// raises her burst and her C5 her skill, while Diluc is the other way round.
// The text names the ability, and the character's own mined talent names say
// which slot that is — so the mapping is derived rather than assumed, and
// wrong only if upstream renames an ability mid-sentence.
func talentBoosts(c gamedata.Character) map[int]gamedata.TalentBoost {
	byName := map[string]string{}
	for slot, t := range c.Talents {
		if t.Name != "" {
			byName[normaliseAbilityName(t.Name)] = slot
		}
	}
	if len(byName) == 0 {
		return nil
	}

	out := map[int]gamedata.TalentBoost{}
	for i, con := range c.Constellations {
		m := levelBoostPattern.FindStringSubmatch(con.Description)
		if m == nil {
			continue
		}
		slot, ok := resolveAbility(m[1], byName)
		if !ok {
			continue
		}
		levels, err := strconv.Atoi(m[2])
		if err != nil || levels <= 0 {
			continue
		}

		boost := gamedata.TalentBoost{Slot: slot, Levels: levels, MaxLevel: 15}
		if mm := maxLevelPattern.FindStringSubmatch(con.Description); mm != nil {
			if v, err := strconv.Atoi(mm[1]); err == nil && v > 0 {
				boost.MaxLevel = v
			}
		}
		out[i+1] = boost
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// normaliseAbilityName strips the emphasis markup that newer characters'
// constellation text wraps ability names in — Aino's C3 reads "Increases the
// Level of **Precision Hydronic Cooler** by 3" — so the name still matches
// the talent table it refers to.
func normaliseAbilityName(name string) string {
	name = stripTags(name)
	name = strings.ReplaceAll(name, "*", "")
	name = strings.ReplaceAll(name, "\u201c", "")
	name = strings.ReplaceAll(name, "\u201d", "")
	return strings.ToLower(strings.TrimSpace(name))
}

// talentTypePrefixes are the labels newer constellation text puts in front of
// an ability name: "Increases the Level of Elemental Skill **Eternal Tides**".
var talentTypePrefixes = map[string]string{
	"elemental skill": gamedata.TalentSkill,
	"elemental burst": gamedata.TalentBurst,
	"normal attack":   gamedata.TalentAuto,
}

// resolveAbility maps the text a constellation names onto a talent slot.
//
// Three passes, most specific first: the ability name as written; the name
// with a talent-type label stripped off the front; and finally the label
// alone, which still identifies the slot even if upstream renames the
// ability. Anything else is left alone rather than guessed at.
func resolveAbility(raw string, byName map[string]string) (string, bool) {
	name := normaliseAbilityName(raw)
	if slot, ok := byName[name]; ok {
		return slot, true
	}
	for prefix, slot := range talentTypePrefixes {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(name, prefix))
		if s, ok := byName[rest]; ok {
			return s, true
		}
		return slot, true
	}
	return "", false
}
