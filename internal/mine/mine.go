package mine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// Config names the upstream sources. Every one is overridable because none of
// them is a contract: mirrors go stale, move, and desync from each other.
type Config struct {
	// GameData is the datamine the numeric tables come from. Values there
	// are keyed by numeric id, which is why a stale *name* map upstream
	// cannot corrupt them.
	GameDataRepo string
	GameDataRef  string

	// EnkaStore supplies character names, elements and skill ordering.
	EnkaStoreRepo string
	EnkaStoreRef  string

	// GenshinDB supplies weapon and artifact-set names, keyed by the same
	// numeric ids the datamine uses.
	GenshinDBRepo string
	GenshinDBRef  string

	// Version labels the snapshot.
	Version string

	// MaxLevel is how far to expand the growth curves.
	MaxLevel int
}

// DefaultConfig returns the sources Mimir mines from today.
//
// The split is deliberate. Dimbreath's repository is the only current source
// for the numeric tables, but its ExcelBinOutput name hashes do not resolve
// against its own TextMap — so names come from elsewhere, keyed by id. See
// docs/GAMEDATA.md.
func DefaultConfig() Config {
	return Config{
		GameDataRepo:  "DimbreathBot/AnimeGameData",
		GameDataRef:   "main",
		EnkaStoreRepo: "EnkaNetwork/API-docs",
		EnkaStoreRef:  "master",
		GenshinDBRepo: "theBowja/genshin-db",
		GenshinDBRef:  "main",
		MaxLevel:      90,
	}
}

// Miner builds a snapshot.
type Miner struct {
	Src *Source
	Cfg Config
	Log func(format string, args ...any)
}

// New returns a miner.
func New(src *Source, cfg Config) *Miner {
	return &Miner{Src: src, Cfg: cfg, Log: func(string, ...any) {}}
}

func (m *Miner) excel(name string) string {
	return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/ExcelBinOutput/%s.json",
		m.Cfg.GameDataRepo, m.Cfg.GameDataRef, name)
}

// Run mines a complete snapshot.
func (m *Miner) Run(ctx context.Context) (*gamedata.Snapshot, error) {
	snap := &gamedata.Snapshot{
		Version:          m.Cfg.Version,
		Characters:       map[string]gamedata.Character{},
		Weapons:          map[string]gamedata.Weapon{},
		ArtifactSets:     map[string]gamedata.ArtifactSet{},
		LevelMultipliers: map[int]float64{},
		SubstatRolls:     map[int]map[model.Stat][]float64{},
		MainStatValues:   map[int]map[model.Stat][]float64{},
		AvatarIDs:        map[int]string{},
		WeaponIDs:        map[int]string{},
		SetIDs:           map[int]string{},
		SetNameHashes:    map[string]string{},
		TravelerDepots:   map[int]string{},
		Curves:           map[string][]float64{},
	}

	m.Log("mining curves")
	if err := m.mineCurves(ctx, snap); err != nil {
		return nil, err
	}
	m.Log("mining characters")
	if err := m.mineCharacters(ctx, snap); err != nil {
		return nil, err
	}
	m.Log("mining weapons")
	if err := m.mineWeapons(ctx, snap); err != nil {
		return nil, err
	}
	m.Log("mining artifacts")
	if err := m.mineArtifacts(ctx, snap); err != nil {
		return nil, err
	}
	m.Log("mining artifact domains")
	if err := m.mineDomains(ctx, snap); err != nil {
		return nil, err
	}
	m.Log("mining talent tables")
	if err := m.mineTalents(ctx, snap); err != nil {
		return nil, err
	}
	m.Log("mining constellations")
	if err := m.mineConstellations(ctx, snap); err != nil {
		return nil, err
	}
	m.Log("mining reaction level multipliers")
	if err := m.mineElementCoeff(ctx, snap); err != nil {
		return nil, err
	}

	return snap, nil
}

// ---------------------------------------------------------------- curves

func (m *Miner) mineCurves(ctx context.Context, snap *gamedata.Snapshot) error {
	for _, table := range []string{"AvatarCurveExcelConfigData", "WeaponCurveExcelConfigData"} {
		var rows []curveRow
		if err := m.Src.GetJSON(ctx, m.excel(table), &rows); err != nil {
			return err
		}
		for _, row := range rows {
			if row.Level < 1 || row.Level > m.Cfg.MaxLevel {
				continue
			}
			for _, info := range row.CurveInfos {
				curve, ok := snap.Curves[info.Type]
				if !ok {
					curve = make([]float64, m.Cfg.MaxLevel)
					snap.Curves[info.Type] = curve
				}
				curve[row.Level-1] = info.Value
			}
		}
	}
	if len(snap.Curves) == 0 {
		return fmt.Errorf("mine: no growth curves found; the datamine layout has changed")
	}
	return nil
}

// ---------------------------------------------------------------- characters

func (m *Miner) mineCharacters(ctx context.Context, snap *gamedata.Snapshot) error {
	var avatars []avatarRow
	if err := m.Src.GetJSON(ctx, m.excel("AvatarExcelConfigData"), &avatars); err != nil {
		return err
	}
	var promotes []promoteRow
	if err := m.Src.GetJSON(ctx, m.excel("AvatarPromoteExcelConfigData"), &promotes); err != nil {
		return err
	}
	var depots []skillDepotRow
	if err := m.Src.GetJSON(ctx, m.excel("AvatarSkillDepotExcelConfigData"), &depots); err != nil {
		return err
	}

	enkaChars, names, err := m.enkaStore(ctx)
	if err != nil {
		return err
	}

	depotByID := map[int]skillDepotRow{}
	for _, d := range depots {
		depotByID[d.ID] = d
	}
	promoteByID := map[int][]promoteRow{}
	for _, p := range promotes {
		if p.AvatarPromoteID != 0 {
			promoteByID[p.AvatarPromoteID] = append(promoteByID[p.AvatarPromoteID], p)
		}
	}

	// Enka keys the Traveler as "<avatarId>-<skillDepotId>", one entry per
	// element. Those become their own characters — TravelerDendro is a
	// different build from TravelerGeo — so they are handled separately.
	travelers := map[int][]travelerVariant{}
	for id, ec := range enkaChars {
		avatarID, depotID, ok := splitTravelerKey(id)
		if !ok {
			continue
		}
		element, ok := model.ElementFromDatamine(ec.Element)
		if !ok {
			// The unaligned starting depot has no element and no build.
			continue
		}
		travelers[avatarID] = append(travelers[avatarID], travelerVariant{
			depotID: depotID, element: element, order: ec.SkillOrder,
		})
	}

	for _, av := range avatars {
		if variants, ok := travelers[av.ID]; ok {
			m.addTraveler(snap, av, variants, promoteByID[av.AvatarPromoteID], enkaChars, names)
			continue
		}
		ec, ok := enkaChars[strconv.Itoa(av.ID)]
		if !ok {
			// Not in Enka's store means not a playable, showcaseable
			// character: test rows, story NPCs, unreleased entries. Skipping
			// them is the point of using that store as the roster.
			continue
		}
		name := names[strconv.FormatInt(ec.NameTextMapHash, 10)]
		if name == "" {
			continue
		}
		key := GOODKey(name)
		if key == "" {
			continue
		}

		element, _ := model.ElementFromDatamine(ec.Element)

		c := gamedata.Character{
			Key:        key,
			Name:       name,
			Element:    element,
			WeaponType: friendlyWeaponType(av.WeaponType),
			Rarity:     rarityFromQuality(ec.QualityType),
			BaseHP:     av.HPBase,
			BaseATK:    av.AttackBase,
			BaseDEF:    av.DefenseBase,
			Talents:    map[string]gamedata.Talent{},
		}
		for _, g := range av.PropGrowCurves {
			switch g.Type {
			case "FIGHT_PROP_BASE_HP":
				c.CurveHP = g.GrowCurve
			case "FIGHT_PROP_BASE_ATTACK":
				c.CurveATK = g.GrowCurve
			case "FIGHT_PROP_BASE_DEFENSE":
				c.CurveDEF = g.GrowCurve
			}
		}

		applyPromotes(&c, promoteByID[av.AvatarPromoteID])

		if d, ok := depotByID[av.SkillDepotID]; ok {
			c.SkillIDs = skillIDs(d)
		}
		// Enka's SkillOrder is authoritative where the depot is ambiguous —
		// several characters carry alternate sprints and stance skills in
		// the same list.
		if len(ec.SkillOrder) == 3 {
			c.SkillIDs = gamedata.SkillIDs{
				Auto:  ec.SkillOrder[0],
				Skill: ec.SkillOrder[1],
				Burst: ec.SkillOrder[2],
			}
			c.ProudSkillGroupIDs = gamedata.SkillIDs{
				Auto:  ec.ProudMap[strconv.Itoa(ec.SkillOrder[0])],
				Skill: ec.ProudMap[strconv.Itoa(ec.SkillOrder[1])],
				Burst: ec.ProudMap[strconv.Itoa(ec.SkillOrder[2])],
			}
		}

		snap.Characters[key] = c
		snap.AvatarIDs[av.ID] = key
	}

	if len(snap.Characters) == 0 {
		return fmt.Errorf("mine: no characters resolved; the name source and the datamine disagree")
	}
	return nil
}

// applyPromotes fills the ascension tables from the promote rows, and infers
// which stat this character gains on ascension.
func applyPromotes(c *gamedata.Character, rows []promoteRow) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].PromoteLevel < rows[j].PromoteLevel })

	c.PromoteHP = make([]float64, 7)
	c.PromoteATK = make([]float64, 7)
	c.PromoteDEF = make([]float64, 7)
	c.AscensionBonus = make([]float64, 7)

	for _, r := range rows {
		if r.PromoteLevel < 0 || r.PromoteLevel > 6 {
			continue
		}
		for _, p := range r.AddProps {
			switch p.PropType {
			case "FIGHT_PROP_BASE_HP":
				c.PromoteHP[r.PromoteLevel] = p.Value
			case "FIGHT_PROP_BASE_ATTACK":
				c.PromoteATK[r.PromoteLevel] = p.Value
			case "FIGHT_PROP_BASE_DEFENSE":
				c.PromoteDEF[r.PromoteLevel] = p.Value
			default:
				// Whatever is left is the ascension stat. Every character
				// has exactly one, but which one varies, and the datamine
				// does not label it — it is simply the odd prop out.
				if stat, ok := model.StatFromFightProp(p.PropType); ok {
					c.AscensionStat = stat
					c.AscensionBonus[r.PromoteLevel] = p.Value
				}
			}
		}
	}
}

func skillIDs(d skillDepotRow) gamedata.SkillIDs {
	var ids gamedata.SkillIDs
	ids.Burst = d.EnergySkill
	for i, s := range d.Skills {
		if s == 0 {
			continue
		}
		switch i {
		case 0:
			ids.Auto = s
		case 1:
			ids.Skill = s
		}
	}
	return ids
}

// enkaStore returns Enka's character roster and its English name table.
func (m *Miner) enkaStore(ctx context.Context) (map[string]enkaCharacter, map[string]string, error) {
	base := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/store",
		m.Cfg.EnkaStoreRepo, m.Cfg.EnkaStoreRef)

	var chars map[string]enkaCharacter
	if err := m.Src.GetJSON(ctx, base+"/characters.json", &chars); err != nil {
		return nil, nil, err
	}
	var loc map[string]map[string]string
	if err := m.Src.GetJSON(ctx, base+"/loc.json", &loc); err != nil {
		return nil, nil, err
	}
	en, ok := loc["en"]
	if !ok {
		return nil, nil, fmt.Errorf("mine: Enka's loc.json has no \"en\" table")
	}
	return chars, en, nil
}

// ---------------------------------------------------------------- weapons

func (m *Miner) mineWeapons(ctx context.Context, snap *gamedata.Snapshot) error {
	var weapons []weaponRow
	if err := m.Src.GetJSON(ctx, m.excel("WeaponExcelConfigData"), &weapons); err != nil {
		return err
	}
	var promotes []promoteRow
	if err := m.Src.GetJSON(ctx, m.excel("WeaponPromoteExcelConfigData"), &promotes); err != nil {
		return err
	}
	names, passives, err := m.genshinDBWeaponText(ctx)
	if err != nil {
		return err
	}

	promoteByID := map[int][]promoteRow{}
	for _, p := range promotes {
		promoteByID[p.WeaponPromoteID] = append(promoteByID[p.WeaponPromoteID], p)
	}

	for _, wr := range weapons {
		name, ok := names[wr.ID]
		if !ok {
			continue
		}
		key := GOODKey(name)
		if key == "" {
			continue
		}

		w := gamedata.Weapon{
			Key:          key,
			Name:         name,
			Type:         friendlyWeaponType(wr.WeaponType),
			Rarity:       wr.RankLevel,
			PassiveName:  passives[wr.ID].name,
			PassiveTexts: passives[wr.ID].texts,
		}
		for _, p := range wr.WeaponProp {
			if p.PropType == "" {
				continue
			}
			if p.PropType == "FIGHT_PROP_BASE_ATTACK" {
				w.BaseATK = p.InitValue
				w.CurveATK = p.Type
				continue
			}
			if stat, ok := model.StatFromFightProp(p.PropType); ok {
				w.SubStat = stat
				w.SubValue = p.InitValue
				w.CurveSub = p.Type
			}
		}

		w.PromoteATK = make([]float64, 7)
		for _, r := range promoteByID[wr.WeaponPromoteID] {
			if r.PromoteLevel < 0 || r.PromoteLevel > 6 {
				continue
			}
			for _, p := range r.AddProps {
				if p.PropType == "FIGHT_PROP_BASE_ATTACK" {
					w.PromoteATK[r.PromoteLevel] = p.Value
				}
			}
		}

		snap.Weapons[key] = w
		snap.WeaponIDs[wr.ID] = key
	}

	if len(snap.Weapons) == 0 {
		return fmt.Errorf("mine: no weapons resolved")
	}
	return nil
}

// ---------------------------------------------------------------- artifacts

func (m *Miner) mineArtifacts(ctx context.Context, snap *gamedata.Snapshot) error {
	var sets []reliquarySetRow
	if err := m.Src.GetJSON(ctx, m.excel("ReliquarySetExcelConfigData"), &sets); err != nil {
		return err
	}
	var affixes []equipAffixRow
	if err := m.Src.GetJSON(ctx, m.excel("EquipAffixExcelConfigData"), &affixes); err != nil {
		return err
	}
	var pieces []reliquaryRow
	if err := m.Src.GetJSON(ctx, m.excel("ReliquaryExcelConfigData"), &pieces); err != nil {
		return err
	}
	names, texts, err := m.genshinDBArtifactText(ctx)
	if err != nil {
		return err
	}

	affixByID := map[int][]equipAffixRow{}
	for _, a := range affixes {
		affixByID[a.ID] = append(affixByID[a.ID], a)
	}
	raritiesBySet := map[int]map[int]bool{}
	for _, p := range pieces {
		if raritiesBySet[p.SetID] == nil {
			raritiesBySet[p.SetID] = map[int]bool{}
		}
		raritiesBySet[p.SetID][p.RankLevel] = true
	}

	for _, s := range sets {
		name, ok := names[s.SetID]
		if !ok {
			continue
		}
		key := GOODKey(name)
		if key == "" {
			continue
		}

		set := gamedata.ArtifactSet{
			Key:           key,
			Name:          name,
			TwoPieceText:  texts[s.SetID].two,
			FourPieceText: texts[s.SetID].four,
		}
		for r := range raritiesBySet[s.SetID] {
			set.Rarities = append(set.Rarities, r)
		}
		sort.Ints(set.Rarities)

		// The affix rows for a set arrive in bonus order: index 0 is the
		// two-piece, index 1 the four-piece. A bonus expressed as addProps
		// is unconditional and becomes a stat block; one expressed only as
		// an openConfig is conditional and is recorded by name for the
		// effect layer to handle.
		rows := affixByID[s.EquipAffixID]
		sort.Slice(rows, func(i, j int) bool { return rows[i].AffixID < rows[j].AffixID })
		for i, row := range rows {
			block := statBlock(row.AddProps)
			switch i {
			case 0:
				set.TwoPiece = block
			case 1:
				set.FourPiece = block
			}
			if len(block) == 0 && row.OpenConfig != "" {
				set.Conditional = append(set.Conditional, row.OpenConfig)
			}
		}

		snap.ArtifactSets[key] = set
		snap.SetIDs[s.SetID] = key
	}

	if err := m.mineArtifactStatTables(ctx, snap); err != nil {
		return err
	}
	if len(snap.ArtifactSets) == 0 {
		return fmt.Errorf("mine: no artifact sets resolved")
	}
	return nil
}

func (m *Miner) mineArtifactStatTables(ctx context.Context, snap *gamedata.Snapshot) error {
	var levels []reliquaryLevelRow
	if err := m.Src.GetJSON(ctx, m.excel("ReliquaryLevelExcelConfigData"), &levels); err != nil {
		return err
	}
	for _, row := range levels {
		if row.Rank == 0 {
			continue
		}
		byStat, ok := snap.MainStatValues[row.Rank]
		if !ok {
			byStat = map[model.Stat][]float64{}
			snap.MainStatValues[row.Rank] = byStat
		}
		// The datamine counts artifact levels from 1; Mimir and GOOD count
		// from 0, so a +20 is level 20 here and 21 upstream.
		idx := row.Level - 1
		if idx < 0 {
			continue
		}
		for _, p := range row.AddProps {
			stat, ok := model.StatFromFightProp(p.PropType)
			if !ok {
				continue
			}
			curve := byStat[stat]
			for len(curve) <= idx {
				curve = append(curve, 0)
			}
			curve[idx] = p.Value
			byStat[stat] = curve
		}
	}

	var affixes []reliquaryAffixRow
	if err := m.Src.GetJSON(ctx, m.excel("ReliquaryAffixExcelConfigData"), &affixes); err != nil {
		return err
	}
	for _, row := range affixes {
		rarity := rarityFromDepot(row.DepotID)
		if rarity == 0 {
			continue
		}
		stat, ok := model.StatFromFightProp(row.PropType)
		if !ok {
			continue
		}
		byStat, ok := snap.SubstatRolls[rarity]
		if !ok {
			byStat = map[model.Stat][]float64{}
			snap.SubstatRolls[rarity] = byStat
		}
		byStat[stat] = append(byStat[stat], row.PropValue)
	}
	for _, byStat := range snap.SubstatRolls {
		for stat, vals := range byStat {
			sort.Float64s(vals)
			byStat[stat] = dedupe(vals)
		}
	}
	return nil
}

// ---------------------------------------------------------------- reactions

func (m *Miner) mineElementCoeff(ctx context.Context, snap *gamedata.Snapshot) error {
	var rows []elementCoeffRow
	if err := m.Src.GetJSON(ctx, m.excel("ElementCoeffExcelConfigData"), &rows); err != nil {
		return err
	}
	for _, r := range rows {
		if r.Level < 1 || r.PlayerElementLevelCo <= 0 {
			continue
		}
		snap.LevelMultipliers[r.Level] = r.PlayerElementLevelCo
	}
	if len(snap.LevelMultipliers) == 0 {
		return fmt.Errorf("mine: no reaction level multipliers found")
	}
	return nil
}

// ---------------------------------------------------------------- helpers

func statBlock(props []addProp) model.StatBlock {
	out := model.StatBlock{}
	for _, p := range props {
		if p.PropType == "" || p.Value == 0 {
			continue
		}
		if stat, ok := model.StatFromFightProp(p.PropType); ok {
			out[stat] += p.Value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func rarityFromQuality(q string) int {
	switch q {
	case "QUALITY_ORANGE", "QUALITY_ORANGE_SP":
		return 5
	case "QUALITY_PURPLE":
		return 4
	default:
		return 0
	}
}

// rarityFromDepot reads the rarity out of a substat depot id: the depots are
// numbered 101..501, one hundred per rarity.
func rarityFromDepot(depot int) int {
	r := depot / 100
	if r < 1 || r > 5 {
		return 0
	}
	return r
}

func friendlyWeaponType(t string) string {
	switch t {
	case "WEAPON_SWORD_ONE_HAND":
		return "sword"
	case "WEAPON_CLAYMORE":
		return "claymore"
	case "WEAPON_POLE":
		return "polearm"
	case "WEAPON_CATALYST":
		return "catalyst"
	case "WEAPON_BOW":
		return "bow"
	default:
		return strings.ToLower(strings.TrimPrefix(t, "WEAPON_"))
	}
}

func dedupe(sorted []float64) []float64 {
	out := sorted[:0]
	for i, v := range sorted {
		if i == 0 || v != sorted[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// travelerVariant is one elemental form of the Traveler.
type travelerVariant struct {
	depotID int
	element model.Element
	order   []int
}

// splitTravelerKey parses Enka's "<avatarId>-<skillDepotId>" roster key.
func splitTravelerKey(key string) (avatarID, depotID int, ok bool) {
	dash := strings.IndexByte(key, '-')
	if dash < 0 {
		return 0, 0, false
	}
	a, err := strconv.Atoi(key[:dash])
	if err != nil {
		return 0, 0, false
	}
	d, err := strconv.Atoi(key[dash+1:])
	if err != nil {
		return 0, 0, false
	}
	return a, d, true
}

// addTraveler emits one character per elemental form, all sharing the base
// avatar's stats and curves but each with its own skills and key.
func (m *Miner) addTraveler(
	snap *gamedata.Snapshot,
	av avatarRow,
	variants []travelerVariant,
	promotes []promoteRow,
	enkaChars map[string]enkaCharacter,
	names map[string]string,
) {
	base := "Traveler"
	if ec, ok := enkaChars[strconv.Itoa(av.ID)]; ok {
		if n := names[strconv.FormatInt(ec.NameTextMapHash, 10)]; n != "" {
			base = n
		}
	}

	proto := gamedata.Character{
		Name:       base,
		WeaponType: friendlyWeaponType(av.WeaponType),
		Rarity:     5,
		BaseHP:     av.HPBase,
		BaseATK:    av.AttackBase,
		BaseDEF:    av.DefenseBase,
	}
	for _, g := range av.PropGrowCurves {
		switch g.Type {
		case "FIGHT_PROP_BASE_HP":
			proto.CurveHP = g.GrowCurve
		case "FIGHT_PROP_BASE_ATTACK":
			proto.CurveATK = g.GrowCurve
		case "FIGHT_PROP_BASE_DEFENSE":
			proto.CurveDEF = g.GrowCurve
		}
	}
	applyPromotes(&proto, promotes)

	for _, v := range variants {
		c := proto
		c.Element = v.element
		c.Key = GOODKey(base) + GOODKey(string(v.element))
		c.Name = fmt.Sprintf("%s (%s)", base, v.element)
		c.Talents = map[string]gamedata.Talent{}
		if len(v.order) == 3 {
			c.SkillIDs = gamedata.SkillIDs{Auto: v.order[0], Skill: v.order[1], Burst: v.order[2]}
		}

		snap.Characters[c.Key] = c
		snap.TravelerDepots[v.depotID] = c.Key
		// The avatarId alone is ambiguous for the Traveler; map it to one
		// variant only as a last resort for payloads with no depot id.
		if _, taken := snap.AvatarIDs[av.ID]; !taken {
			snap.AvatarIDs[av.ID] = c.Key
		}
	}
}

// setText is an artifact set's in-game bonus wording.
type setText struct{ two, four string }

// genshinDBArtifactText returns set names and their bonus descriptions.
//
// The text matters as much as the name: an effect in Mimir must cite the
// wording its numbers come from, and that citation is checked mechanically
// (see internal/effect). Without the text there is nothing to check against.
func (m *Miner) genshinDBArtifactText(ctx context.Context) (map[int]string, map[int]setText, error) {
	listURL := fmt.Sprintf("https://api.github.com/repos/%s/contents/src/data/English/artifacts?ref=%s",
		m.Cfg.GenshinDBRepo, m.Cfg.GenshinDBRef)

	var listing []struct {
		Name        string `json:"name"`
		DownloadURL string `json:"download_url"`
	}
	if err := m.Src.GetJSON(ctx, listURL, &listing); err != nil {
		return nil, nil, fmt.Errorf("mine: list artifacts: %w", err)
	}

	urls := make([]string, 0, len(listing))
	for _, f := range listing {
		if strings.HasSuffix(f.Name, ".json") && f.DownloadURL != "" {
			urls = append(urls, f.DownloadURL)
		}
	}

	names := map[int]string{}
	texts := map[int]setText{}
	err := m.Src.GetManyJSON(ctx, urls, func(url string, raw []byte) error {
		var e genshinDBEntity
		if err := json.Unmarshal(raw, &e); err != nil {
			return fmt.Errorf("mine: decode %s: %w", url, err)
		}
		if e.ID == 0 || e.Name == "" {
			return nil
		}
		names[e.ID] = e.Name
		texts[e.ID] = setText{two: e.Effect2Pc, four: e.Effect4Pc}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	m.Log("genshin-db artifacts: %d names", len(names))
	return names, texts, nil
}

// mineConstellations reads C1-C6 text, which effects cite the same way
// passives do.
func (m *Miner) mineConstellations(ctx context.Context, snap *gamedata.Snapshot) error {
	listURL := fmt.Sprintf("https://api.github.com/repos/%s/contents/src/data/English/constellations?ref=%s",
		m.Cfg.GenshinDBRepo, m.Cfg.GenshinDBRef)

	var listing []struct {
		Name        string `json:"name"`
		DownloadURL string `json:"download_url"`
	}
	if err := m.Src.GetJSON(ctx, listURL, &listing); err != nil {
		return fmt.Errorf("mine: list constellations: %w", err)
	}

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

	type gdbConstellations struct {
		C1 gdbDescribed `json:"c1"`
		C2 gdbDescribed `json:"c2"`
		C3 gdbDescribed `json:"c3"`
		C4 gdbDescribed `json:"c4"`
		C5 gdbDescribed `json:"c5"`
		C6 gdbDescribed `json:"c6"`
	}

	return m.Src.GetManyJSON(ctx, urls, func(url string, raw []byte) error {
		var file gdbConstellations
		if err := json.Unmarshal(raw, &file); err != nil {
			return fmt.Errorf("mine: decode constellations %s: %w", url, err)
		}
		key := byslug[slugOf[url]]
		c := snap.Characters[key]
		c.Constellations = nil
		for _, d := range []gdbDescribed{file.C1, file.C2, file.C3, file.C4, file.C5, file.C6} {
			c.Constellations = append(c.Constellations, gamedata.Described{
				Name: d.Name, Description: d.Description,
			})
		}
		c.ConstellationTalentBonus = talentBoosts(c)
		snap.Characters[key] = c
		return nil
	})
}

type gdbDescribed struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// weaponPassive is a weapon's passive and its wording at each refinement.
type weaponPassive struct {
	name  string
	texts []string
}

// genshinDBWeaponText returns weapon names and their passive text per
// refinement.
//
// Refinement matters here in a way it does not elsewhere: a weapon passive's
// numbers change with it, so an effect built on one has to cite the wording
// for the refinement it claims. Mining all five means the check can be exact.
func (m *Miner) genshinDBWeaponText(ctx context.Context) (map[int]string, map[int]weaponPassive, error) {
	listURL := fmt.Sprintf("https://api.github.com/repos/%s/contents/src/data/English/weapons?ref=%s",
		m.Cfg.GenshinDBRepo, m.Cfg.GenshinDBRef)

	var listing []struct {
		Name        string `json:"name"`
		DownloadURL string `json:"download_url"`
	}
	if err := m.Src.GetJSON(ctx, listURL, &listing); err != nil {
		return nil, nil, fmt.Errorf("mine: list weapons: %w", err)
	}

	urls := make([]string, 0, len(listing))
	for _, f := range listing {
		if strings.HasSuffix(f.Name, ".json") && f.DownloadURL != "" {
			urls = append(urls, f.DownloadURL)
		}
	}

	type refinement struct {
		Description string `json:"description"`
	}
	type weaponFile struct {
		ID         int        `json:"id"`
		Name       string     `json:"name"`
		EffectName string     `json:"effectName"`
		R1         refinement `json:"r1"`
		R2         refinement `json:"r2"`
		R3         refinement `json:"r3"`
		R4         refinement `json:"r4"`
		R5         refinement `json:"r5"`
	}

	names := map[int]string{}
	passives := map[int]weaponPassive{}
	err := m.Src.GetManyJSON(ctx, urls, func(url string, raw []byte) error {
		var f weaponFile
		if err := json.Unmarshal(raw, &f); err != nil {
			return fmt.Errorf("mine: decode %s: %w", url, err)
		}
		if f.ID == 0 || f.Name == "" {
			return nil
		}
		names[f.ID] = f.Name

		var texts []string
		for _, r := range []refinement{f.R1, f.R2, f.R3, f.R4, f.R5} {
			if r.Description == "" {
				break
			}
			texts = append(texts, r.Description)
		}
		if len(texts) > 0 {
			passives[f.ID] = weaponPassive{name: f.EffectName, texts: texts}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	m.Log("genshin-db weapons: %d names, %d with passives", len(names), len(passives))
	return names, passives, nil
}
