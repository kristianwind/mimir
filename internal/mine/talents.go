package mine

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// Talent tables come from genshin-db rather than straight from ExcelBinOutput.
//
// The raw datamine holds talent scaling as an unlabelled paramList per level;
// the labels that say which param is "Press DMG" and which is "Press CD" live
// in TextMap, and TextMap is exactly the part of the datamine that does not
// resolve (docs/GAMEDATA.md). genshin-db has already paired the two, keyed by
// the same character slugs, so this reads a solved problem instead of
// re-solving it against a broken hash table.

// labelParam matches genshin-db's placeholder syntax: {param3:F1P}.
var labelParam = regexp.MustCompile(`\{param(\d+):([A-Za-z0-9]*)\}`)

type gdbTalentFile struct {
	Combat1  gdbTalent `json:"combat1"`
	Combat2  gdbTalent `json:"combat2"`
	Combat3  gdbTalent `json:"combat3"`
	Passive1 gdbTalent `json:"passive1"`
	Passive2 gdbTalent `json:"passive2"`
	Passive3 gdbTalent `json:"passive3"`
}

type gdbTalent struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Attributes  struct {
		Labels []string `json:"labels"`
	} `json:"attributes"`
}

// gdbTalentStats is one character's entry in stats/talents.json: talent ->
// param name -> value per talent level.
type gdbTalentStats map[string]map[string][]float64

func (m *Miner) mineTalents(ctx context.Context, snap *gamedata.Snapshot) error {
	statsURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/src/data/stats/talents.json",
		m.Cfg.GenshinDBRepo, m.Cfg.GenshinDBRef)

	var stats map[string]gdbTalentStats
	if err := m.Src.GetJSON(ctx, statsURL, &stats); err != nil {
		return fmt.Errorf("mine: talent stats: %w", err)
	}

	listURL := fmt.Sprintf("https://api.github.com/repos/%s/contents/src/data/English/talents?ref=%s",
		m.Cfg.GenshinDBRepo, m.Cfg.GenshinDBRef)
	var listing []struct {
		Name        string `json:"name"`
		DownloadURL string `json:"download_url"`
	}
	if err := m.Src.GetJSON(ctx, listURL, &listing); err != nil {
		return fmt.Errorf("mine: list talents: %w", err)
	}

	// genshin-db slugs are the lowercased GOOD key, which is what lets the
	// two halves be joined without a hand-written alias table.
	byslug := map[string]string{}
	for key := range snap.Characters {
		byslug[strings.ToLower(key)] = key
	}

	urls := make([]string, 0, len(listing))
	slugOf := map[string]string{}
	for _, f := range listing {
		if !strings.HasSuffix(f.Name, ".json") || f.DownloadURL == "" {
			continue
		}
		slug := strings.TrimSuffix(f.Name, ".json")
		if _, ok := byslug[slug]; !ok {
			continue
		}
		urls = append(urls, f.DownloadURL)
		slugOf[f.DownloadURL] = slug
	}

	var matched int
	err := m.Src.GetManyJSON(ctx, urls, func(url string, raw []byte) error {
		slug := slugOf[url]
		key := byslug[slug]
		var file gdbTalentFile
		if err := json.Unmarshal(raw, &file); err != nil {
			return fmt.Errorf("mine: decode talents for %s: %w", key, err)
		}
		values, ok := stats[slug]
		if !ok {
			return nil
		}

		c := snap.Characters[key]
		if c.Talents == nil {
			c.Talents = map[string]gamedata.Talent{}
		}
		for _, pair := range []struct {
			slot   string
			talent gdbTalent
			source string
		}{
			{gamedata.TalentAuto, file.Combat1, "combat1"},
			{gamedata.TalentSkill, file.Combat2, "combat2"},
			{gamedata.TalentBurst, file.Combat3, "combat3"},
		} {
			t := buildTalent(pair.talent, values[pair.source], c.Element, pair.slot)
			if len(t.Entries) > 0 {
				c.Talents[pair.slot] = t
			}
		}

		// Passive text is mined so effects can cite the exact wording their
		// numbers come from — see internal/effect.
		c.Passives = map[string]gamedata.Described{}
		for name, p := range map[string]gdbTalent{
			"passive1": file.Passive1, "passive2": file.Passive2, "passive3": file.Passive3,
		} {
			if p.Name == "" && p.Description == "" {
				continue
			}
			c.Passives[name] = gamedata.Described{Name: p.Name, Description: p.Description}
		}
		if len(c.Talents) > 0 {
			matched++
		}
		snap.Characters[key] = c
		return nil
	})
	if err != nil {
		return err
	}

	m.Log("talent tables: %d of %d characters", matched, len(snap.Characters))
	return nil
}

// buildTalent pairs each label with the parameter arrays it references.
func buildTalent(t gdbTalent, params map[string][]float64, element model.Element, slot string) gamedata.Talent {
	out := gamedata.Talent{Name: t.Name}
	for _, raw := range t.Attributes.Labels {
		matches := labelParam.FindAllStringSubmatch(raw, -1)
		if len(matches) == 0 {
			continue
		}
		// A label may reference several params ("... {param3:F1P} ATK +
		// {param4:F1P} Elemental Mastery"). The first is the primary
		// scaling; the rest become their own entries so nothing is lost.
		spans := labelParam.FindAllStringIndex(raw, -1)
		for i, mm := range matches {
			name := "param" + mm[1]
			values, ok := params[name]
			if !ok || len(values) == 0 {
				continue
			}
			label := cleanLabel(raw)
			if len(matches) > 1 {
				// A label with several placeholders is one attack with
				// several components — Xiangling's third normal hits twice.
				// Number them rather than leaking "param7" into the UI.
				label = fmt.Sprintf("%s · del %d", label, i+1)
			}
			out.Entries = append(out.Entries, gamedata.TalentEntry{
				Label:    label,
				Unit:     unitFromFormat(mm[2]),
				Category: categoryFor(raw, slot),
				// Scale off the text that follows this particular
				// placeholder. Nahida's skill reads
				// "{param3} ATK + {param4} Elemental Mastery": one label,
				// two different scaling stats, and only the trailing
				// fragment tells them apart.
				Scaling:     scalingFor(fragmentAfter(raw, spans, i), raw),
				Element:     element,
				Multipliers: append([]float64(nil), values...),
			})
		}
	}
	sort.SliceStable(out.Entries, func(i, j int) bool { return out.Entries[i].Label < out.Entries[j].Label })
	return out
}

// cleanLabel strips the placeholders and trailing punctuation, leaving the
// human-readable name of the row.
func cleanLabel(raw string) string {
	if i := strings.IndexByte(raw, '|'); i >= 0 {
		raw = raw[:i]
	}
	raw = labelParam.ReplaceAllString(raw, "")
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(raw), "|·:"))
}

// unitFromFormat reads genshin-db's format suffix: a trailing P means the
// value is displayed as a percentage, which is exactly the set of values the
// damage engine can use as multipliers.
func unitFromFormat(format string) string {
	switch {
	case strings.HasSuffix(strings.ToUpper(format), "P"):
		return "percent"
	case strings.EqualFold(format, "F1"), strings.EqualFold(format, "F2"):
		return "seconds"
	default:
		return "flat"
	}
}

// scalingFromLabel infers which stat a multiplier applies to. ATK is the
// default because it is the overwhelming majority; HP and DEF scaling say so
// in the label text, which is the only signal upstream gives.
// scalingFor prefers the fragment immediately after the placeholder and only
// falls back to the whole label when that fragment names no stat. Consulting
// both at once would let one mention of "Elemental Mastery" claim every
// placeholder in a multi-part label.
func scalingFor(fragment, whole string) model.Stat {
	if stat, ok := scalingIn(fragment); ok {
		return stat
	}
	if stat, ok := scalingIn(whole); ok {
		return stat
	}
	return model.ATK
}

func scalingIn(text string) (model.Stat, bool) {
	upper := strings.ToUpper(text)
	switch {
	case strings.Contains(upper, "ELEMENTAL MASTERY"):
		return model.ElementalMastery, true
	case strings.Contains(upper, "MAX HP"), strings.Contains(upper, "HP)"):
		return model.HP, true
	case strings.Contains(upper, "DEF"):
		return model.DEF, true
	case strings.Contains(upper, "ATK"):
		return model.ATK, true
	default:
		return "", false
	}
}

// fragmentAfter returns the text between placeholder i and the next one, which
// is where upstream names the stat that placeholder scales off.
func fragmentAfter(raw string, spans [][]int, i int) string {
	if i >= len(spans) {
		return ""
	}
	start := spans[i][1]
	end := len(raw)
	if i+1 < len(spans) {
		end = spans[i+1][0]
	}
	if start > end {
		return ""
	}
	return raw[start:end]
}

// categoryFor decides which attack-category DMG bonuses a row picks up.
//
// The talent slot is the wrong answer on its own. Raiden's burst table lists
// the sword swings of Musou Isshin, and those are normal, charged and
// plunging attacks — they take normal-attack bonuses, not burst bonuses.
// Charging them her Emblem set would overstate her by tens of percent.
func categoryFor(label, slot string) model.Category {
	upper := strings.ToUpper(label)
	switch {
	case strings.Contains(upper, "CHARGED"):
		return model.CategoryCharged
	case strings.Contains(upper, "PLUNGE"), strings.Contains(upper, "PLUNGING"):
		return model.CategoryPlunge
	case strings.Contains(upper, "-HIT"), strings.Contains(upper, "NORMAL ATTACK"):
		return model.CategoryNormal
	}
	switch slot {
	case gamedata.TalentSkill:
		return model.CategorySkill
	case gamedata.TalentBurst:
		return model.CategoryBurst
	default:
		return model.CategoryNormal
	}
}
