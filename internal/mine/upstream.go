package mine

// The upstream row shapes, trimmed to the fields Mimir uses.
//
// Field names in ExcelBinOutput are mostly readable, but every table also
// carries obfuscated keys whose names rotate between versions. Decoding into
// narrow structs rather than map[string]any means a rotation cannot silently
// change which value we read — an unexpected shape shows up as a zero we
// validate against, not as a plausible wrong number.

type avatarRow struct {
	ID              int     `json:"id"`
	HPBase          float64 `json:"hpBase"`
	AttackBase      float64 `json:"attackBase"`
	DefenseBase     float64 `json:"defenseBase"`
	QualityType     string  `json:"qualityType"`
	WeaponType      string  `json:"weaponType"`
	SkillDepotID    int     `json:"skillDepotId"`
	AvatarPromoteID int     `json:"avatarPromoteId"`
	PropGrowCurves  []struct {
		Type      string `json:"type"`
		GrowCurve string `json:"growCurve"`
	} `json:"propGrowCurves"`
}

type curveRow struct {
	Level      int `json:"level"`
	CurveInfos []struct {
		Type  string  `json:"type"`
		Value float64 `json:"value"`
	} `json:"curveInfos"`
}

type addProp struct {
	PropType string  `json:"propType"`
	Value    float64 `json:"value"`
}

type promoteRow struct {
	AvatarPromoteID int       `json:"avatarPromoteId"`
	WeaponPromoteID int       `json:"weaponPromoteId"`
	PromoteLevel    int       `json:"promoteLevel"`
	AddProps        []addProp `json:"addProps"`
	UnlockMaxLevel  int       `json:"unlockMaxLevel"`
}

type skillDepotRow struct {
	ID          int   `json:"id"`
	Skills      []int `json:"skills"`
	EnergySkill int   `json:"energySkill"`
	Talents     []int `json:"talents"`
}

type weaponRow struct {
	ID              int    `json:"id"`
	RankLevel       int    `json:"rankLevel"`
	WeaponType      string `json:"weaponType"`
	WeaponPromoteID int    `json:"weaponPromoteId"`
	WeaponProp      []struct {
		PropType  string  `json:"propType"`
		InitValue float64 `json:"initValue"`
		Type      string  `json:"type"`
	} `json:"weaponProp"`
	SkillAffix []int `json:"skillAffix"`
}

type reliquarySetRow struct {
	SetID        int   `json:"setId"`
	EquipAffixID int   `json:"equipAffixId"`
	SetNeedNum   []int `json:"setNeedNum"`
	ContainsList []int `json:"containsList"`
}

type equipAffixRow struct {
	ID         int       `json:"id"`
	AffixID    int       `json:"affixId"`
	Level      int       `json:"level"`
	AddProps   []addProp `json:"addProps"`
	OpenConfig string    `json:"openConfig"`
}

type reliquaryRow struct {
	ID                int    `json:"id"`
	SetID             int    `json:"setId"`
	RankLevel         int    `json:"rankLevel"`
	EquipType         string `json:"equipType"`
	MainPropDepotID   int    `json:"mainPropDepotId"`
	AppendPropDepotID int    `json:"appendPropDepotId"`
	MaxLevel          int    `json:"maxLevel"`
}

type reliquaryLevelRow struct {
	Rank     int       `json:"rank"`
	Level    int       `json:"level"`
	AddProps []addProp `json:"addProps"`
}

type reliquaryAffixRow struct {
	DepotID   int     `json:"depotId"`
	PropType  string  `json:"propType"`
	PropValue float64 `json:"propValue"`
}

type elementCoeffRow struct {
	Level                int     `json:"level"`
	PlayerElementLevelCo float64 `json:"playerElementLevelCo"`
	PlayerShieldLevelCo  float64 `json:"playerShieldLevelCo"`
}

// enkaCharacter is one entry in Enka's store/characters.json.
//
// Enka's store is the name source for characters rather than a TextMap,
// because it is keyed by exactly the avatarId that arrives in a showcase.
// A name resolved this way cannot disagree with the data being imported.
type enkaCharacter struct {
	Element         string         `json:"Element"`
	SkillOrder      []int          `json:"SkillOrder"`
	Skills          map[string]any `json:"Skills"`
	ProudMap        map[string]int `json:"ProudMap"`
	NameTextMapHash int64          `json:"NameTextMapHash"`
	QualityType     string         `json:"QualityType"`
	WeaponType      string         `json:"WeaponType"`
	Consts          []string       `json:"Consts"`
}

// genshinDBEntity is the shared shape of genshin-db's weapon and artifact
// files: a numeric id matching the datamine, plus the English display name.
type genshinDBEntity struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	RarityList []int  `json:"rarityList"`
	Rarity     any    `json:"rarity"`
	Effect2Pc  string `json:"effect2Pc"`
	Effect4Pc  string `json:"effect4Pc"`
}
