package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/advisor"
	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// potentialSnapshot is a miniature game: one character with a real talent
// table and one artifact set, which is everything the yardstick reads.
func potentialSnapshot() *gamedata.Snapshot {
	flat := make([]float64, 91)
	for i := range flat {
		flat[i] = 1 + float64(i)*0.05
	}
	ladder := func(start, step float64, n int) []float64 {
		out := make([]float64, n)
		for i := range out {
			out[i] = start + float64(i)*step
		}
		return out
	}
	curve := func(v float64) []float64 {
		out := make([]float64, 21)
		for i := range out {
			out[i] = v * float64(i) / 20
		}
		return out
	}

	return &gamedata.Snapshot{
		Version: "test",
		Curves:  map[string][]float64{"FLAT": flat},
		Characters: map[string]gamedata.Character{
			"Tester": {
				Key: "Tester", Name: "Tester", Element: model.Pyro,
				WeaponType: "sword", Rarity: 5,
				BaseHP: 1000, BaseATK: 100, BaseDEF: 500,
				CurveHP: "FLAT", CurveATK: "FLAT", CurveDEF: "FLAT",
				AscensionStat:  model.CritDMG,
				AscensionBonus: []float64{0, 0, 0.096, 0.192, 0.192, 0.288, 0.384},
				PromoteATK:     []float64{0, 10, 20, 30, 40, 50, 60},
				PromoteHP:      []float64{0, 100, 200, 300, 400, 500, 600},
				PromoteDEF:     []float64{0, 10, 20, 30, 40, 50, 60},
				SkillIDs:       gamedata.SkillIDs{Auto: 1, Skill: 2, Burst: 3},
				Talents: map[string]gamedata.Talent{
					gamedata.TalentSkill: {Name: "Test Skill", Entries: []gamedata.TalentEntry{{
						Label: "Skill DMG", Unit: "percent", Scaling: model.ATK,
						Element: model.Pyro, Multipliers: ladder(1.0, 0.15, 10),
					}}},
					gamedata.TalentBurst: {Name: "Test Burst", Entries: []gamedata.TalentEntry{{
						Label: "Burst DMG", Unit: "percent", Scaling: model.ATK,
						Element: model.Pyro, Multipliers: ladder(2.0, 0.2, 10),
					}}},
				},
			},
		},
		ArtifactSets: map[string]gamedata.ArtifactSet{
			"A": {Key: "A", Name: "Set A", TwoPiece: model.StatBlock{model.ATKPercent: 0.18}},
		},
		MainStatValues: map[int]map[model.Stat][]float64{
			5: {model.ATKPercent: curve(0.466), model.HP: curve(4780), model.ATK: curve(311)},
		},
	}
}

// potentialServer wires an account with one measurable character.
//
// Deliberately small: what is under test here is the HTTP layer's contract —
// who gets measured, what is never overwritten — not the ranking itself, which
// has its own tests against the engine.
func potentialServer(t *testing.T) (*Server, func(as, method, path, body string) *httptest.ResponseRecorder, int64) {
	t.Helper()
	s, do := newServer(t)
	s.GameData = gamedata.NewStore(s.DB)
	if err := s.GameData.Save(potentialSnapshot()); err != nil {
		t.Fatal(err)
	}
	if err := s.GameData.Load(); err != nil {
		t.Fatal(err)
	}

	id := seedAccount(t, s, "member")
	if _, err := s.DB.Exec(`
		INSERT INTO characters (account_id, char_key, level, ascension, constellation,
		                        talent_auto, talent_skill, talent_burst, source)
		VALUES (?, 'Tester', 90, 6, 0, 6, 6, 6, 'enka')`, id); err != nil {
		t.Fatal(err)
	}
	subs := `[{"key":"critRate_","value":0.1},{"key":"critDMG_","value":0.2}]`
	for i, slot := range model.Slots {
		if _, err := s.DB.Exec(`
			INSERT INTO artifacts (account_id, fingerprint, identity, set_key, slot_key,
			                       rarity, level, main_stat, substats, location, crit_value, source)
			VALUES (?, ?, ?, 'A', ?, 5, 0, 'atk_', ?, 'Tester', 40, 'enka')`,
			id, fmt.Sprintf("fp-%d", i), fmt.Sprintf("id-%d", i), string(slot), subs); err != nil {
			t.Fatal(err)
		}
	}
	return s, do, id
}

func TestPotentialMeasuresACharacterWithNoGoal(t *testing.T) {
	s, do, id := potentialServer(t)
	_ = s

	res := do("member", "GET", fmt.Sprintf("/api/accounts/%d/potential", id), "")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var got advisor.Ranking
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Characters) != 1 || got.Characters[0].Character != "Tester" {
		t.Fatalf("ranking = %+v", got.Characters)
	}
	if got.Characters[0].Current <= 0 {
		t.Error("a character with no goal scored zero; the yardstick needs no rotation")
	}
	if len(got.Caveats) == 0 {
		t.Error("the ranking states no limits, so it reads as a verdict")
	}
}

// The one rule that matters when a machine writes goals.
func TestDerivingNeverOverwritesAGoalYouWrote(t *testing.T) {
	s, do, id := potentialServer(t)

	mine := `{"characterKey":"Tester","priority":9,"rotation":{"name":"mine","duration":20,` +
		`"steps":[{"talent":"burst","entry":"Burst DMG","hits":3}]}}`
	if res := do("member", "PUT", fmt.Sprintf("/api/accounts/%d/goals", id), mine); res.Code != http.StatusOK {
		t.Fatalf("could not save my own goal: %d %s", res.Code, res.Body.String())
	}

	res := do("member", "POST", fmt.Sprintf("/api/accounts/%d/goals/derive", id), `{}`)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var out struct {
		Created int `json:"created"`
		Goals   []struct {
			Character string `json:"character"`
			Created   bool   `json:"created"`
			Reason    string `json:"reason"`
		} `json:"goals"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Created != 0 {
		t.Errorf("overwrote %d goals the player wrote", out.Created)
	}
	if len(out.Goals) != 1 || out.Goals[0].Reason == "" {
		t.Errorf("the skip is not explained: %+v", out.Goals)
	}

	// And the rotation is still theirs, hits and all.
	var rotation string
	if err := s.DB.QueryRow(`SELECT rotation FROM goals WHERE account_id = ? AND char_key = 'Tester'`,
		id).Scan(&rotation); err != nil {
		t.Fatal(err)
	}
	var spec advisor.Spec
	if err := json.Unmarshal([]byte(rotation), &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Name != "mine" || spec.Steps[0].Hits != 3 {
		t.Errorf("the player's rotation was changed: %+v", spec)
	}
}

func TestADerivedGoalIsMarkedAsSuch(t *testing.T) {
	s, do, id := potentialServer(t)

	res := do("member", "POST", fmt.Sprintf("/api/accounts/%d/goals/derive", id), `{}`)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}

	var source string
	if err := s.DB.QueryRow(`SELECT source FROM goals WHERE account_id = ? AND char_key = 'Tester'`,
		id).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != "derived" {
		t.Fatalf("source = %q, want derived", source)
	}

	// The list says so too, or the UI cannot warn anybody.
	list := do("member", "GET", fmt.Sprintf("/api/accounts/%d/goals", id), "")
	var goals []map[string]any
	if err := json.Unmarshal(list.Body.Bytes(), &goals); err != nil {
		t.Fatal(err)
	}
	if len(goals) != 1 || goals[0]["source"] != "derived" {
		t.Fatalf("the goal list does not carry the source: %v", goals)
	}
}

// Opening a derived goal and saving it makes it the player's. That is the
// whole point of showing them the guess.
func TestSavingADerivedGoalMakesItYours(t *testing.T) {
	s, do, id := potentialServer(t)
	do("member", "POST", fmt.Sprintf("/api/accounts/%d/goals/derive", id), `{}`)

	mine := `{"characterKey":"Tester","priority":1,"rotation":{"name":"mine","duration":20,` +
		`"steps":[{"talent":"skill","entry":"Skill DMG","hits":2}]}}`
	if res := do("member", "PUT", fmt.Sprintf("/api/accounts/%d/goals", id), mine); res.Code != http.StatusOK {
		t.Fatalf("save failed: %d %s", res.Code, res.Body.String())
	}

	var source string
	if err := s.DB.QueryRow(`SELECT source FROM goals WHERE account_id = ? AND char_key = 'Tester'`,
		id).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != "manual" {
		t.Errorf("source = %q after the player saved it, want manual", source)
	}
}

// A plan built on a rotation Mimir guessed has to say so, every time.
func TestThePlanAdmitsWhenTheRotationIsDerived(t *testing.T) {
	_, do, id := potentialServer(t)
	do("member", "POST", fmt.Sprintf("/api/accounts/%d/goals/derive", id), `{}`)

	res := do("member", "GET", fmt.Sprintf("/api/accounts/%d/plan", id), "")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var out struct {
		Plan struct {
			Caveats []string `json:"caveats"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	var admitted bool
	for _, c := range out.Plan.Caveats {
		if strings.Contains(c, "derived by Mimir") && strings.Contains(c, "Tester") {
			admitted = true
		}
	}
	if !admitted {
		t.Errorf("the plan does not admit the rotation is a guess: %v", out.Plan.Caveats)
	}
}
