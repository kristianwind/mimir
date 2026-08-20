// Package enka reads character showcases from Enka.Network.
//
// Enka is the zero-friction entry point: the player types a nine-digit UID and
// Mimir gets their showcased characters with levels, constellations, talents,
// weapons and fully rolled artifacts — no login, no scanner, no cookies.
//
// The cost is coverage. A showcase holds at most eight characters and only
// their equipped artifacts, so Enka bootstraps an account but cannot replace
// a GOOD import of the full inventory. See docs/DATAMODEL.md, "Three sources,
// one inventory", for how the merge resolves conflicts.
package enka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrRateLimited is returned on HTTP 429. Back off; do not retry in a loop.
var ErrRateLimited = errors.New("enka: rate limited")

// ErrNoShowcase is returned when the UID exists but the player has not
// enabled "Show Character Details" in game. This is the single most common
// support question, so it gets its own error and its own UI copy.
var ErrNoShowcase = errors.New("enka: profile has no character showcase")

// ErrNotFound is returned for a UID Enka does not know.
var ErrNotFound = errors.New("enka: uid not found")

// Client is an Enka.Network HTTP client.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	// UserAgent must identify this application. Enka's terms ask for a
	// named agent so they can contact the operator instead of blocking a
	// generic one; a default Go agent is a good way to get banned.
	UserAgent string
}

// New returns a client with sensible defaults.
func New(userAgent string) *Client {
	return &Client{
		BaseURL:   "https://enka.network",
		HTTP:      &http.Client{Timeout: 20 * time.Second},
		UserAgent: userAgent,
	}
}

// Response is the shape Enka returns for /api/uid/{uid}/.
type Response struct {
	PlayerInfo     PlayerInfo   `json:"playerInfo"`
	AvatarInfoList []AvatarInfo `json:"avatarInfoList"`
	// TTL is seconds until Enka will serve fresh showcase data. Requests
	// before it expires return the same cached payload and still consume
	// rate limit, so Mimir caches on TTL rather than a fixed interval.
	TTL int    `json:"ttl"`
	UID string `json:"uid"`
}

// PlayerInfo is the always-public part of a profile.
type PlayerInfo struct {
	Nickname             string `json:"nickname"`
	Level                int    `json:"level"`
	Signature            string `json:"signature"`
	WorldLevel           int    `json:"worldLevel"`
	FinishAchievementNum int    `json:"finishAchievementNum"`
	TowerFloorIndex      int    `json:"towerFloorIndex"`
	TowerLevelIndex      int    `json:"towerLevelIndex"`
	ShowAvatarInfoList   []struct {
		AvatarID int `json:"avatarId"`
		Level    int `json:"level"`
	} `json:"showAvatarInfoList"`
}

// AvatarInfo is one showcased character.
type AvatarInfo struct {
	AvatarID int `json:"avatarId"`
	// SkillDepotID distinguishes the Traveler's elements; it is the only
	// field that does.
	SkillDepotID  int                `json:"skillDepotId"`
	PropMap       map[string]Prop    `json:"propMap"`
	FightPropMap  map[string]float64 `json:"fightPropMap"`
	SkillLevelMap map[string]int     `json:"skillLevelMap"`
	// ProudSkillExtraLevelMap holds the +3 talent levels granted by C3/C5.
	ProudSkillExtraLevelMap map[string]int `json:"proudSkillExtraLevelMap"`
	TalentIDList            []int          `json:"talentIdList"`
	EquipList               []Equip        `json:"equipList"`
}

// Prop is Enka's boxed property value; Val is a decimal string.
type Prop struct {
	Type int    `json:"type"`
	Ival string `json:"ival"`
	Val  string `json:"val"`
}

// Equip is one equipped artifact or weapon.
type Equip struct {
	ItemID    int        `json:"itemId"`
	Reliquary *Reliquary `json:"reliquary,omitempty"`
	Weapon    *Weapon    `json:"weapon,omitempty"`
	Flat      Flat       `json:"flat"`
}

// Reliquary is the artifact's upgrade state.
type Reliquary struct {
	// Level is 1-based in Enka's payload: a +20 artifact reports 21.
	Level            int   `json:"level"`
	MainPropID       int   `json:"mainPropId"`
	AppendPropIDList []int `json:"appendPropIdList"`
}

// Weapon is the weapon's upgrade state.
type Weapon struct {
	Level        int            `json:"level"`
	PromoteLevel int            `json:"promoteLevel"`
	AffixMap     map[string]int `json:"affixMap"`
}

// Flat is the denormalised display data Enka attaches to every equip.
type Flat struct {
	NameTextMapHash    string `json:"nameTextMapHash"`
	SetNameTextMapHash string `json:"setNameTextMapHash"`
	// SetID is the datamine's artifact set id, present on artifacts.
	SetID             int    `json:"setId"`
	RankLevel         int    `json:"rankLevel"`
	ItemType          string `json:"itemType"`
	EquipType         string `json:"equipType"`
	ReliquaryMainstat *struct {
		MainPropID string  `json:"mainPropId"`
		StatValue  float64 `json:"statValue"`
	} `json:"reliquaryMainstat,omitempty"`
	ReliquarySubstats []struct {
		AppendPropID string  `json:"appendPropId"`
		StatValue    float64 `json:"statValue"`
	} `json:"reliquarySubstats,omitempty"`
	WeaponStats []struct {
		AppendPropID string  `json:"appendPropId"`
		StatValue    float64 `json:"statValue"`
	} `json:"weaponStats,omitempty"`
}

// Fetch retrieves a UID's showcase.
func (c *Client) Fetch(ctx context.Context, uid string) (*Response, error) {
	if err := ValidateUID(uid); err != nil {
		return nil, err
	}
	// No trailing slash: Enka answers "/api/uid/<uid>/" with a 308 to the
	// unslashed form, so sending it costs an extra round trip on every call.
	url := fmt.Sprintf("%s/api/uid/%s", strings.TrimRight(c.BaseURL, "/"), uid)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("enka: fetch %s: %w", uid, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, ErrNotFound
	case http.StatusTooManyRequests:
		return nil, ErrRateLimited
	case http.StatusBadRequest:
		return nil, fmt.Errorf("enka: malformed uid %q", uid)
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("enka: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("enka: decode: %w", err)
	}
	if out.UID == "" {
		out.UID = uid
	}
	if len(out.AvatarInfoList) == 0 {
		return &out, ErrNoShowcase
	}
	return &out, nil
}

// ValidateUID rejects obviously wrong input before it costs a request.
func ValidateUID(uid string) error {
	if len(uid) < 9 || len(uid) > 10 {
		return fmt.Errorf("enka: uid %q must be 9 digits (10 for CN accounts)", uid)
	}
	for _, r := range uid {
		if r < '0' || r > '9' {
			return fmt.Errorf("enka: uid %q must be digits only", uid)
		}
	}
	return nil
}

// Region names the server a UID belongs to, from its leading digit.
func Region(uid string) string {
	if uid == "" {
		return "unknown"
	}
	switch uid[0] {
	case '1', '2', '3', '5':
		return "cn"
	case '6':
		return "america"
	case '7':
		return "europe"
	case '8':
		return "asia"
	case '9':
		return "twhkmo"
	default:
		return "unknown"
	}
}
