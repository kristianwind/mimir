package enka

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

func TestValidateUID(t *testing.T) {
	for _, uid := range []string{"700000001", "1234567890"} {
		if err := ValidateUID(uid); err != nil {
			t.Errorf("ValidateUID(%q) = %v, want nil", uid, err)
		}
	}
	for _, uid := range []string{"", "12345", "70000000a", "12345678901"} {
		if err := ValidateUID(uid); err == nil {
			t.Errorf("ValidateUID(%q) = nil, want an error", uid)
		}
	}
}

func TestRegionFromLeadingDigit(t *testing.T) {
	cases := map[string]string{
		"600000001": "america",
		"700000001": "europe",
		"800000001": "asia",
		"900000001": "twhkmo",
		"100000001": "cn",
		"":          "unknown",
	}
	for uid, want := range cases {
		if got := Region(uid); got != want {
			t.Errorf("Region(%q) = %q, want %q", uid, got, want)
		}
	}
}

const showcaseJSON = `{
  "playerInfo": {"nickname":"Testrejsende","level":60,"worldLevel":8},
  "avatarInfoList": [{
    "avatarId": 10000073,
    "propMap": {"4001": {"type":4001,"val":"90"}, "1002": {"type":1002,"val":"6"}},
    "talentIdList": [1,2],
    "skillLevelMap": {"10731":9,"10732":10,"10735":8},
    "equipList": [
      {
        "itemId": 91545,
        "reliquary": {"level":21,"mainPropId":14001,"appendPropIdList":[]},
        "flat": {
          "setId": 15006,
          "setNameTextMapHash":"1085612012",
          "rankLevel":5,
          "itemType":"ITEM_RELIQUARY",
          "equipType":"EQUIP_DRESS",
          "reliquaryMainstat":{"mainPropId":"FIGHT_PROP_CRITICAL_HURT","statValue":62.2},
          "reliquarySubstats":[
            {"appendPropId":"FIGHT_PROP_ATTACK_PERCENT","statValue":9.9},
            {"appendPropId":"FIGHT_PROP_ELEMENT_MASTERY","statValue":40}
          ]
        }
      },
      {
        "itemId": 13509,
        "weapon": {"level":90,"promoteLevel":6,"affixMap":{"113509":2}},
        "flat": {"itemType":"ITEM_WEAPON","rankLevel":5}
      }
    ]
  }],
  "ttl": 60,
  "uid": "700000001"
}`

func testSnapshot() *gamedata.Snapshot {
	return &gamedata.Snapshot{
		Version:   "test",
		AvatarIDs: map[int]string{10000073: "Nahida"},
		WeaponIDs: map[int]string{13509: "AThousandFloatingDreams"},
		SetIDs:    map[int]string{15006: "DeepwoodMemories"},
		SetNameHashes: map[string]string{
			"1085612012": "DeepwoodMemories",
		},
		Characters: map[string]gamedata.Character{
			"Nahida": {
				Key:      "Nahida",
				Element:  model.Dendro,
				SkillIDs: gamedata.SkillIDs{Auto: 10731, Skill: 10732, Burst: 10735},
			},
		},
	}
}

func serve(t *testing.T, status int, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request left without a User-Agent; Enka blocks anonymous clients")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := New("mimir-test/0.1")
	c.BaseURL = srv.URL
	return c
}

func TestFetchAndImport(t *testing.T) {
	c := serve(t, http.StatusOK, showcaseJSON)
	resp, err := c.Fetch(context.Background(), "700000001")
	if err != nil {
		t.Fatal(err)
	}

	res := resp.Import(42, testSnapshot())
	if len(res.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", res.Warnings)
	}
	if res.Account.Region != "europe" || res.Account.Nickname != "Testrejsende" {
		t.Errorf("account = %+v", res.Account)
	}

	if len(res.Characters) != 1 {
		t.Fatalf("got %d characters, want 1", len(res.Characters))
	}
	ch := res.Characters[0]
	if ch.Key != "Nahida" || ch.Level != 90 || ch.Ascension != 6 {
		t.Errorf("character = %+v", ch)
	}
	if ch.Constellation != 2 {
		t.Errorf("constellation = %d, want 2", ch.Constellation)
	}
	if ch.TalentAuto != 9 || ch.TalentSkill != 10 || ch.TalentBurst != 8 {
		t.Errorf("talents = %d/%d/%d, want 9/10/8", ch.TalentAuto, ch.TalentSkill, ch.TalentBurst)
	}

	if len(res.Artifacts) != 1 {
		t.Fatalf("got %d artifacts, want 1", len(res.Artifacts))
	}
	a := res.Artifacts[0]
	if a.SetKey != "DeepwoodMemories" || a.SlotKey != model.Circlet {
		t.Errorf("artifact = %+v", a)
	}
	// Enka reports +20 as level 21.
	if a.Level != 20 {
		t.Errorf("artifact level = %d, want 20", a.Level)
	}
	// Percent stats must arrive as fractions; flat stats must not be scaled.
	if a.MainStat != model.CritDMG {
		t.Errorf("main stat = %q", a.MainStat)
	}
	var atkPct, em float64
	for _, s := range a.Substats {
		switch s.Key {
		case model.ATKPercent:
			atkPct = s.Value
		case model.ElementalMastery:
			em = s.Value
		}
	}
	if atkPct != 0.099 {
		t.Errorf("ATK%% = %v, want 0.099", atkPct)
	}
	if em != 40 {
		t.Errorf("EM = %v, want 40 (flat stats must not be divided)", em)
	}

	if len(res.Weapons) != 1 || res.Weapons[0].Refinement != 3 {
		t.Errorf("weapons = %+v, want one R3", res.Weapons)
	}
}

func TestImportWarnsInsteadOfFailing(t *testing.T) {
	c := serve(t, http.StatusOK, showcaseJSON)
	resp, err := c.Fetch(context.Background(), "700000001")
	if err != nil {
		t.Fatal(err)
	}
	snap := testSnapshot()
	delete(snap.AvatarIDs, 10000073)

	res := resp.Import(1, snap)
	if len(res.Characters) != 0 {
		t.Error("an unmappable character should be skipped")
	}
	if len(res.Warnings) == 0 {
		t.Error("skipping silently hides a stale gamedata sync from the user")
	}
}

func TestFetchErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{http.StatusNotFound, ErrNotFound},
		{http.StatusTooManyRequests, ErrRateLimited},
	}
	for _, tc := range cases {
		c := serve(t, tc.status, `{}`)
		_, err := c.Fetch(context.Background(), "700000001")
		if !errors.Is(err, tc.want) {
			t.Errorf("status %d gave %v, want %v", tc.status, err, tc.want)
		}
	}

	c := serve(t, http.StatusOK, `{"playerInfo":{"nickname":"x"},"ttl":60,"uid":"700000001"}`)
	if _, err := c.Fetch(context.Background(), "700000001"); !errors.Is(err, ErrNoShowcase) {
		t.Errorf("empty showcase gave %v, want ErrNoShowcase", err)
	}
}

func TestCachedClientHonoursTTL(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(showcaseJSON))
	}))
	defer srv.Close()

	clock := time.Unix(0, 0)
	cc := NewCached("mimir-test/0.1")
	cc.Client.BaseURL = srv.URL
	cc.Now = func() time.Time { return clock }

	first, err := cc.Fetch(context.Background(), "700000001")
	if err != nil {
		t.Fatal(err)
	}
	if first.FromCache {
		t.Error("the first fetch cannot be a cache hit")
	}

	clock = clock.Add(30 * time.Second)
	second, err := cc.Fetch(context.Background(), "700000001")
	if err != nil {
		t.Fatal(err)
	}
	if !second.FromCache {
		t.Error("a request inside the TTL must not reach Enka")
	}
	if calls != 1 {
		t.Errorf("made %d upstream requests inside one TTL window, want 1", calls)
	}

	clock = clock.Add(31 * time.Second)
	if _, err := cc.Fetch(context.Background(), "700000001"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("made %d upstream requests after the TTL expired, want 2", calls)
	}
}

func TestCachedClientFallsBackToStaleOnRateLimit(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(showcaseJSON))
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	clock := time.Unix(0, 0)
	cc := NewCached("mimir-test/0.1")
	cc.Client.BaseURL = srv.URL
	cc.Now = func() time.Time { return clock }

	if _, err := cc.Fetch(context.Background(), "700000001"); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Minute)

	got, err := cc.Fetch(context.Background(), "700000001")
	if err != nil {
		t.Fatalf("a rate limit with warm cache should degrade, not fail: %v", err)
	}
	if !got.Stale {
		t.Error("stale data must be labelled stale")
	}
	if got.PlayerInfo.Nickname != "Testrejsende" {
		t.Error("fell back to an empty response instead of the cached one")
	}
}

func TestImportCrossChecksConstellationBonuses(t *testing.T) {
	snap := testSnapshot()
	nahida := snap.Characters["Nahida"]
	nahida.ProudSkillGroupIDs = gamedata.SkillIDs{Auto: 7331, Skill: 7332, Burst: 7339}
	// Mimir's own derivation: C3 boosts the skill.
	nahida.ConstellationTalentBonus = map[int]gamedata.TalentBoost{
		3: {Slot: gamedata.TalentSkill, Levels: 3, MaxLevel: 15},
	}
	snap.Characters["Nahida"] = nahida

	// The showcase says C2 with no bonuses active, which agrees.
	c := serve(t, http.StatusOK, showcaseJSON)
	resp, err := c.Fetch(context.Background(), "700000001")
	if err != nil {
		t.Fatal(err)
	}
	if res := resp.Import(1, snap); len(res.Warnings) != 0 {
		t.Errorf("agreement produced warnings: %v", res.Warnings)
	}

	// Now the game reports a bonus on the burst that Mimir's derivation puts
	// on the skill. That is a mining error and must be visible.
	withExtras := strings.Replace(showcaseJSON,
		`"talentIdList": [1,2],`,
		`"talentIdList": [1,2,3],"proudSkillExtraLevelMap": {"7339": 3},`, 1)
	c2 := serve(t, http.StatusOK, withExtras)
	resp2, err := c2.Fetch(context.Background(), "700000001")
	if err != nil {
		t.Fatal(err)
	}
	res := resp2.Import(1, snap)
	if len(res.Warnings) == 0 {
		t.Fatal("a disagreement between the game and the game data went unreported")
	}
	// Both halves of the mistake are reported: the burst that gained levels
	// Mimir did not expect, and the skill that gained none where it did.
	joined := strings.Join(res.Warnings, " | ")
	for _, want := range []string{"burst", "skill"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings do not mention %s: %q", want, joined)
		}
	}
}
