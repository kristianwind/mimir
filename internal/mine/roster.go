package mine

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// Who counts as a character.
//
// The roster used to be whatever Enka's store listed, on the reasoning that a
// character you cannot showcase is not one you can build. That reasoning was
// sound and the consequence was not: Enka's store is community-maintained and
// runs months behind the game, so a character released in spring was still
// missing from Mimir in late summer. Not merely absent from a dropdown —
// absent from the engine, so an account that had her showed her stats and
// then could say nothing at all about them.
//
// It also failed at the job it was chosen for. Enka's store carries the
// game's trial copies, so "PyroArchonTest" and "HuTaoTrial" arrived as
// playable characters while Linnea did not.
//
// So the datamine is the roster now, and the two name sources are asked in
// turn: Enka first, because its skill ordering is worth having, then
// genshin-db, which is keyed by the same avatar id. Both are only consulted
// for what the datamine cannot be read for. What separates a character from a
// trial copy of one is that the copy reuses the original's portrait: one icon
// is one character, and where several ids share an icon the lowest is the
// real one — a trial is always added to the game after the character it
// duplicates.

type gdbCharacter struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	WeaponType  string `json:"weaponType"`
	ElementText string `json:"elementText"`
	Rarity      int    `json:"rarity"`
}

// characterNames fetches genshin-db's roster, keyed by avatar id.
func (m *Miner) characterNames(ctx context.Context) (map[int]gdbCharacter, error) {
	listURL := fmt.Sprintf("https://api.github.com/repos/%s/contents/src/data/English/characters?ref=%s",
		m.Cfg.GenshinDBRepo, m.Cfg.GenshinDBRef)

	var listing []struct {
		Name        string `json:"name"`
		DownloadURL string `json:"download_url"`
	}
	if err := m.Src.GetJSON(ctx, listURL, &listing); err != nil {
		return nil, fmt.Errorf("mine: list characters: %w", err)
	}

	urls := make([]string, 0, len(listing))
	for _, f := range listing {
		if strings.HasSuffix(f.Name, ".json") && f.DownloadURL != "" {
			urls = append(urls, f.DownloadURL)
		}
	}

	out := map[int]gdbCharacter{}
	err := m.Src.GetManyJSON(ctx, urls, func(url string, raw []byte) error {
		var c gdbCharacter
		if err := json.Unmarshal(raw, &c); err != nil {
			return fmt.Errorf("mine: decode character %s: %w", url, err)
		}
		if c.ID == 0 || c.Name == "" {
			return nil
		}
		out[c.ID] = c
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// identity is what a name source can tell us about an avatar id.
type identity struct {
	name       string
	element    model.Element
	rarity     int
	art        string
	skillOrder []int
	proudMap   map[string]int
	// source names where this came from, for the miner's log.
	source string
}

// resolveIdentity asks each name source in turn.
//
// Enka is asked first because it carries the skill ordering, which the depot
// alone is ambiguous about for the characters that have alternate sprints and
// stance skills. genshin-db carries no skill ordering, so a character known
// only to it gets its skills from the depot and its talent levels cannot be
// costed — reported by the validator rather than passed off as complete.
func resolveIdentity(
	av avatarRow,
	enka map[string]enkaCharacter,
	names map[string]string,
	gdb map[int]gdbCharacter,
) (identity, bool) {
	if ec, ok := enka[strconv.Itoa(av.ID)]; ok {
		if name := names[strconv.FormatInt(ec.NameTextMapHash, 10)]; name != "" {
			element, _ := model.ElementFromDatamine(ec.Element)
			return identity{
				name:       name,
				element:    element,
				rarity:     rarityFromQuality(ec.QualityType),
				art:        artBase(ec.SideIconName),
				skillOrder: ec.SkillOrder,
				proudMap:   ec.ProudMap,
				source:     "enka",
			}, true
		}
	}

	if c, ok := gdb[av.ID]; ok {
		element, _ := model.ElementFromDatamine(c.ElementText)
		return identity{
			name:    c.Name,
			element: element,
			rarity:  c.Rarity,
			// The portrait suffix follows the icon, which the datamine does
			// carry in readable text: UI_AvatarIcon_Linnea is Linnea's,
			// whoever gets round to naming her.
			art:    strings.TrimPrefix(av.IconName, "UI_AvatarIcon_"),
			source: "genshin-db",
		}, true
	}

	return identity{}, false
}

// playableAvatars picks the avatar rows that are characters, one per portrait.
//
// A trial copy of a character reuses that character's icon and is added to the
// game later, so it always carries a higher id. Keeping the lowest id per icon
// therefore keeps the character and drops every copy of her, without a list of
// names to exclude and without a range of ids to treat as magic.
func playableAvatars(rows []avatarRow) []avatarRow {
	lowest := map[string]avatarRow{}
	for _, av := range rows {
		if av.IconName == "" {
			continue
		}
		if seen, ok := lowest[av.IconName]; !ok || av.ID < seen.ID {
			lowest[av.IconName] = av
		}
	}

	out := make([]avatarRow, 0, len(lowest))
	for _, av := range rows {
		if kept, ok := lowest[av.IconName]; ok && kept.ID == av.ID {
			out = append(out, av)
		}
	}
	return out
}

// reportRoster records what each name source contributed, because a source
// falling behind is invisible until somebody misses a character by name.
func (m *Miner) reportRoster(snap *gamedata.Snapshot, sources map[string]int) {
	m.Log("characters: %d (%d named by Enka, %d by genshin-db)",
		len(snap.Characters), sources["enka"], sources["genshin-db"])
}

// proudSkillGroups maps a character's three talents onto the proud-skill
// groups that hold their level tables and level-up costs.
//
// Read from the datamine rather than from Enka's ProudMap, because Enka only
// has it for the characters it has caught up with — and a character with no
// group has no talent bill, which is a gap that shows up as an upgrade Mimir
// cannot price rather than as a missing character.
func proudSkillGroups(skills gamedata.SkillIDs, group map[int]int) gamedata.SkillIDs {
	return gamedata.SkillIDs{
		Auto:  group[skills.Auto],
		Skill: group[skills.Skill],
		Burst: group[skills.Burst],
	}
}
