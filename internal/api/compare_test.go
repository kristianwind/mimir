package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/enka"
)

// A showcase for somebody else, wearing the same fixture character this
// account has, with worse artifacts: five +0 pieces where the account under
// test has five of its own.
const theirShowcase = `{
  "playerInfo": {"nickname":"Somebody","level":58,"worldLevel":8},
  "avatarInfoList": [{
    "avatarId": 900001,
    "propMap": {"4001": {"type":4001,"val":"90"}, "1002": {"type":1002,"val":"6"}},
    "talentIdList": [],
    "skillLevelMap": {"1":6,"2":6,"3":6},
    "equipList": [
      {"itemId": 1, "reliquary": {"level":1,"mainPropId":14001,"appendPropIdList":[]},
       "flat": {"setId": 90001, "rankLevel":5, "itemType":"ITEM_RELIQUARY",
                "equipType":"EQUIP_BRACER",
                "reliquaryMainstat":{"mainPropId":"FIGHT_PROP_HP","statValue":717},
                "reliquarySubstats":[]}},
      {"itemId": 2, "reliquary": {"level":1,"mainPropId":14001,"appendPropIdList":[]},
       "flat": {"setId": 90001, "rankLevel":5, "itemType":"ITEM_RELIQUARY",
                "equipType":"EQUIP_NECKLACE",
                "reliquaryMainstat":{"mainPropId":"FIGHT_PROP_ATTACK","statValue":47},
                "reliquarySubstats":[]}},
      {"itemId": 3, "reliquary": {"level":1,"mainPropId":14001,"appendPropIdList":[]},
       "flat": {"setId": 90001, "rankLevel":5, "itemType":"ITEM_RELIQUARY",
                "equipType":"EQUIP_SHOES",
                "reliquaryMainstat":{"mainPropId":"FIGHT_PROP_ATTACK_PERCENT","statValue":7},
                "reliquarySubstats":[]}},
      {"itemId": 4, "reliquary": {"level":1,"mainPropId":14001,"appendPropIdList":[]},
       "flat": {"setId": 90001, "rankLevel":5, "itemType":"ITEM_RELIQUARY",
                "equipType":"EQUIP_RING",
                "reliquaryMainstat":{"mainPropId":"FIGHT_PROP_ATTACK_PERCENT","statValue":7},
                "reliquarySubstats":[]}},
      {"itemId": 5, "reliquary": {"level":1,"mainPropId":14001,"appendPropIdList":[]},
       "flat": {"setId": 90001, "rankLevel":5, "itemType":"ITEM_RELIQUARY",
                "equipType":"EQUIP_DRESS",
                "reliquaryMainstat":{"mainPropId":"FIGHT_PROP_ATTACK_PERCENT","statValue":7},
                "reliquarySubstats":[]}}
    ]
  }],
  "ttl": 60,
  "uid": "800000002"
}`

// A showcase holding a character this account does not have.
const strangerShowcase = `{
  "playerInfo": {"nickname":"Stranger","level":58},
  "avatarInfoList": [],
  "ttl": 60,
  "uid": "800000003"
}`

// serveEnka points a cached Enka client at a canned payload.
func serveEnka(t *testing.T, body string) *enka.CachedClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	c := enka.NewCached("mimir-test/0.1")
	c.Client.BaseURL = srv.URL
	return c
}

// comparable extends the potential fixture with the ids an Enka import needs
// to recognise the same character and set.
func comparable(t *testing.T) (*Server, func(as, method, path, body string) *httptest.ResponseRecorder, int64) {
	t.Helper()
	s, do, id := potentialServer(t)

	snap := potentialSnapshot()
	snap.AvatarIDs = map[int]string{900001: "Tester"}
	snap.SetIDs = map[int]string{90001: "A"}
	if err := s.GameData.Save(snap); err != nil {
		t.Fatal(err)
	}
	if err := s.GameData.Load(); err != nil {
		t.Fatal(err)
	}
	return s, do, id
}

// The rule that matters most here: a showcase Mimir looked at is not a
// showcase Mimir keeps. It belongs to whoever published it, and an account
// that was inspected once must not turn into rows on this instance.
func TestComparingStoresNothingAboutTheOtherAccount(t *testing.T) {
	s, do, id := comparable(t)
	s.Enka = serveEnka(t, theirShowcase)

	before := countRows(t, s)

	res := do("member", "GET", fmt.Sprintf("/api/accounts/%d/compare/800000002", id), "")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}

	for table, n := range countRows(t, s) {
		if n != before[table] {
			t.Errorf("%s went from %d rows to %d; the other account was written down",
				table, before[table], n)
		}
	}
	for _, uid := range scanStrings(t, s, `SELECT uid FROM accounts`) {
		if uid == "800000002" {
			t.Error("the other account was saved as an account on this instance")
		}
	}
}

// Both sides have to be measured with the same ruler, or the comparison is
// two unrelated numbers printed next to each other.
func TestComparingMeasuresBothSidesTheSameWay(t *testing.T) {
	s, do, id := comparable(t)
	s.Enka = serveEnka(t, theirShowcase)

	res := do("member", "GET", fmt.Sprintf("/api/accounts/%d/compare/800000002", id), "")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var got Comparison
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Characters) != 1 {
		t.Fatalf("compared %d characters, want the one both accounts have: %+v", len(got.Characters), got)
	}
	c := got.Characters[0]
	if c.Yours.Score <= 0 || c.Theirs.Score <= 0 {
		t.Fatalf("a side scored nothing: %+v", c)
	}
	if len(c.Yours.MeasuredOn) == 0 {
		t.Error("the comparison does not say what it measured")
	}
	if strings.Join(c.Yours.MeasuredOn, "|") != strings.Join(c.Theirs.MeasuredOn, "|") {
		t.Errorf("the two sides were measured on different rows: %v against %v",
			c.Yours.MeasuredOn, c.Theirs.MeasuredOn)
	}
	if c.Ratio <= 0 {
		t.Errorf("ratio = %v", c.Ratio)
	}
	if len(got.Caveats) == 0 {
		t.Error("the comparison states no limits, so it reads as a verdict")
	}
	if got.Nickname != "Somebody" {
		t.Errorf("nickname = %q", got.Nickname)
	}
}

// Comparing an account with itself is a mistake worth catching: the answer is
// always "identical", which looks like the feature is broken.
func TestComparingWithYourOwnUIDIsRefused(t *testing.T) {
	s, do, id := comparable(t)
	s.Enka = serveEnka(t, theirShowcase)

	uid := scanStrings(t, s, `SELECT uid FROM accounts`)[0]
	res := do("member", "GET", fmt.Sprintf("/api/accounts/%d/compare/%s", id, uid), "")
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

// A showcase has to be switched on in the game before anyone can read it, and
// a profile with it switched off is the commonest reason this appears not to
// work. The answer has to name the setting, because "no characters" reads as
// a broken feature and "turn this on" does not.
func TestAnEmptyShowcaseSaysWhatToSwitchOn(t *testing.T) {
	s, do, id := comparable(t)
	s.Enka = serveEnka(t, strangerShowcase)

	res := do("member", "GET", fmt.Sprintf("/api/accounts/%d/compare/800000003", id), "")
	if res.Code == http.StatusOK {
		t.Fatalf("an empty showcase was accepted: %s", res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "Show Character Details") {
		t.Errorf("the answer does not name the setting to switch on: %s", res.Body.String())
	}
}

func countRows(t *testing.T, s *Server) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, table := range []string{"accounts", "characters", "artifacts", "weapons"} {
		var n int
		if err := s.DB.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[table] = n
	}
	return out
}

func scanStrings(t *testing.T, s *Server, query string) []string {
	t.Helper()
	rows, err := s.DB.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}
