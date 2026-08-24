package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/kvasir"
	"github.com/kristianwind/mimir/internal/llm"
)

// modelStub is an OpenAI-compatible endpoint that always answers the same
// thing, and counts how often it was asked. The count is the assertion that
// matters for the cache: a page that re-asks a local model every time it is
// opened is a page nobody leaves open.
type modelStub struct {
	reply string
	calls int
}

func (m *modelStub) advisor(t *testing.T) *kvasir.Advisor {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.calls++
		w.Header().Set("Content-Type", "application/json")
		body, _ := json.Marshal(m.reply)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":`+
			string(body)+`}}],"usage":{}}`)
	}))
	t.Cleanup(srv.Close)
	return &kvasir.Advisor{Client: llm.New(srv.URL, "test-model", "", 0)}
}

// seedInventory gives the account something the engine can describe without a
// game data snapshot: the artifacts brief is inventory arithmetic, not damage.
func seedInventory(t *testing.T, s *Server, accountID int64) {
	t.Helper()
	subs := `[{"key":"critRate_","value":0.101},{"key":"critDMG_","value":0.202}]`
	for i := 0; i < 6; i++ {
		if _, err := s.DB.Exec(`
			INSERT INTO artifacts (account_id, fingerprint, identity, set_key, slot_key,
			                       rarity, level, main_stat, substats, location, crit_value, source)
			VALUES (?, ?, ?, 'EmblemOfSeveredFate', 'flower', 5, 20, 'hp', ?, ?, 40.4, 'good')`,
			accountID, fmt.Sprintf("fp-%d", i), fmt.Sprintf("id-%d", i), subs,
			map[bool]string{true: "RaidenShogun", false: ""}[i == 0],
		); err != nil {
			t.Fatal(err)
		}
	}
}

func seedAccount(t *testing.T, s *Server, username string) int64 {
	t.Helper()
	var userID int64
	if err := s.DB.QueryRow(`SELECT id FROM users WHERE username = ?`, username).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	res, err := s.DB.Exec(`INSERT INTO accounts (user_id, uid) VALUES (?, '700000001')`, userID)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// With no endpoint configured the feature is off, not broken. The frontend
// reads this to leave the cards out entirely.
func TestKvasirReportsItselfOffWhenUnconfigured(t *testing.T) {
	s, do := newServer(t)
	s.Kvasir = &kvasir.Advisor{Client: llm.New("", "", "", 0)}

	res := do("member", "GET", "/api/kvasir", "")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d", res.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["enabled"] != false {
		t.Fatalf("enabled = %v", out["enabled"])
	}
}

func TestAskingWithNoModelConfiguredSaysSo(t *testing.T) {
	s, do := newServer(t)
	s.Kvasir = &kvasir.Advisor{Client: llm.New("", "", "", 0)}
	id := seedAccount(t, s, "member")

	res := do("member", "POST", fmt.Sprintf("/api/accounts/%d/kvasir/opinion", id),
		`{"surface":"artifacts"}`)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestAnOpinionIsAnsweredOnceAndThenRemembered(t *testing.T) {
	s, do := newServer(t)
	stub := &modelStub{reply: `{"verdict":"Upload a full inventory.",
	  "points":[{"headline":"Get the rest of the artifacts in","why":"Only 6 pieces are known."}]}`}
	s.Kvasir = stub.advisor(t)

	id := seedAccount(t, s, "member")
	seedInventory(t, s, id)
	path := fmt.Sprintf("/api/accounts/%d/kvasir/opinion", id)

	res := do("member", "POST", path, `{"surface":"artifacts"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var first opinionResponse
	if err := json.Unmarshal(res.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.Opinion.Verdict == "" || len(first.Opinion.Points) != 1 {
		t.Fatalf("opinion = %+v", first.Opinion)
	}
	if first.Cached {
		t.Error("the first answer claims to be cached")
	}
	// The fact sheet comes back with the answer: an opinion whose evidence
	// is not shown cannot be checked against the page it is about.
	if !strings.Contains(first.Brief, "EmblemOfSeveredFate") {
		t.Errorf("the fact sheet does not describe the inventory:\n%s", first.Brief)
	}

	res = do("member", "POST", path, `{"surface":"artifacts"}`)
	var second opinionResponse
	if err := json.Unmarshal(res.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if !second.Cached || stub.calls != 1 {
		t.Fatalf("the model was asked %d times for unchanged facts (cached=%v)", stub.calls, second.Cached)
	}
	if second.Opinion.Verdict != first.Opinion.Verdict {
		t.Error("the remembered answer differs from the one that was given")
	}

	// An explicit refresh is the one thing that spends a second completion.
	do("member", "POST", path, `{"surface":"artifacts","refresh":true}`)
	if stub.calls != 2 {
		t.Fatalf("refresh did not re-ask the model (calls = %d)", stub.calls)
	}
}

// Changing the account has to move the fact sheet's hash, or a remembered
// opinion outlives the numbers it was about.
func TestNewGearMeansANewOpinion(t *testing.T) {
	s, do := newServer(t)
	stub := &modelStub{reply: `{"verdict":"Fine.","points":[]}`}
	s.Kvasir = stub.advisor(t)

	id := seedAccount(t, s, "member")
	seedInventory(t, s, id)
	path := fmt.Sprintf("/api/accounts/%d/kvasir/opinion", id)

	do("member", "POST", path, `{"surface":"artifacts"}`)
	if _, err := s.DB.Exec(`
		INSERT INTO artifacts (account_id, fingerprint, identity, set_key, slot_key,
		                       rarity, level, main_stat, substats, crit_value, source)
		VALUES (?, 'fp-new', 'id-new', 'ShimenawasReminiscence', 'sands', 5, 20, 'atk_', '[]', 12.0, 'good')`,
		id); err != nil {
		t.Fatal(err)
	}
	do("member", "POST", path, `{"surface":"artifacts"}`)

	if stub.calls != 2 {
		t.Fatalf("the model was asked %d times; new gear should mean a new opinion", stub.calls)
	}
}

func TestAnUnknownSurfaceIsRefused(t *testing.T) {
	s, do := newServer(t)
	stub := &modelStub{reply: `{"verdict":"?","points":[]}`}
	s.Kvasir = stub.advisor(t)
	id := seedAccount(t, s, "member")

	res := do("member", "POST", fmt.Sprintf("/api/accounts/%d/kvasir/opinion", id),
		`{"surface":"whatever"}`)
	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", res.Code)
	}
	if stub.calls != 0 {
		t.Error("the model was asked about a page that does not exist")
	}
}

// Ownership is enforced in one middleware for every account route. This pins
// that the AI routes went inside it: they carry more of an account than any
// other endpoint does.
func TestAnotherUsersAccountIsNotVisibleToKvasir(t *testing.T) {
	s, do := newServer(t)
	stub := &modelStub{reply: `{"verdict":"?","points":[]}`}
	s.Kvasir = stub.advisor(t)
	id := seedAccount(t, s, "boss")

	res := do("member", "POST", fmt.Sprintf("/api/accounts/%d/kvasir/opinion", id),
		`{"surface":"artifacts"}`)
	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if stub.calls != 0 {
		t.Error("somebody else's account reached the model")
	}
}

// Without game data nothing can be calculated, so there is nothing to have an
// opinion about — and the answer has to be the same one the rest of the
// product gives, not a model asked to improvise around the gap.
func TestNoGameDataMeansNoOpinion(t *testing.T) {
	s, do := newServer(t)
	stub := &modelStub{reply: `{"verdict":"?","points":[]}`}
	s.Kvasir = stub.advisor(t)
	s.GameData = gamedata.NewStore(s.DB)

	id := seedAccount(t, s, "member")
	if _, err := s.DB.Exec(
		`INSERT INTO goals (account_id, char_key, rotation) VALUES (?, 'RaidenShogun', '{}')`, id); err != nil {
		t.Fatal(err)
	}

	res := do("member", "POST", fmt.Sprintf("/api/accounts/%d/kvasir/opinion", id), `{"surface":"plan"}`)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if stub.calls != 0 {
		t.Error("the model was asked to comment on an account nothing could be calculated for")
	}
}

// The cache mints a row per change of the facts, so it has to have a ceiling.
// An unbounded table in a database that also holds fourteen hundred artifacts
// is the kind of thing nobody notices until a backup takes a minute.
func TestStoredOpinionsAreCapped(t *testing.T) {
	s, do := newServer(t)
	stub := &modelStub{reply: `{"verdict":"Fine.","points":[]}`}
	s.Kvasir = stub.advisor(t)

	id := seedAccount(t, s, "member")
	seedInventory(t, s, id)
	path := fmt.Sprintf("/api/accounts/%d/kvasir/opinion", id)

	// Each refresh replaces the row for these facts, so the cap is exercised
	// by rows that differ: fill the table directly and let one real request
	// do the pruning.
	for i := 0; i < keptOpinions+20; i++ {
		if _, err := s.DB.Exec(`
			INSERT INTO kvasir_opinions (account_id, surface, subject, facts_hash, body)
			VALUES (?, 'plan', ?, ?, '{}')`,
			id, fmt.Sprintf("old-%d", i), fmt.Sprintf("hash-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	do("member", "POST", path, `{"surface":"artifacts"}`)

	var rows int
	if err := s.DB.QueryRow(`SELECT count(*) FROM kvasir_opinions WHERE account_id = ?`, id).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows > keptOpinions {
		t.Fatalf("%d stored opinions, cap is %d", rows, keptOpinions)
	}

	// The answer just given has to be one of the survivors, or the cache
	// prunes the very row it wrote.
	res := do("member", "POST", path, `{"surface":"artifacts"}`)
	var out opinionResponse
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Cached {
		t.Error("the newest answer was pruned along with the old ones")
	}
}
