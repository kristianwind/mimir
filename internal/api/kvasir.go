package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kristianwind/mimir/internal/advisor"
	"github.com/kristianwind/mimir/internal/db"
	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/i18n"
	"github.com/kristianwind/mimir/internal/kvasir"
	"github.com/kristianwind/mimir/internal/model"
)

// The AI layer's HTTP surface.
//
// Two endpoints and a status. The status exists so the frontend can leave the
// whole feature out rather than render a card that will only ever say the
// operator has not configured a model — an AI layer that is optional should be
// invisible when it is off, not broken.
//
// Answers are stored against the hash of the fact sheet that produced them.
// That is a cache, but it is also the reason "why did Kvasir say that?" has an
// answer: the evidence is kept next to the opinion, and an account that has
// not changed does not get a differently-worded opinion every time the page is
// opened. It is the same instinct as seeding the farm simulator.

// kvasirBudget bounds one request's time with the model.
//
// Below the server's two-minute write timeout on purpose: an answer that
// arrives after the connection has been closed is worse than a timeout, since
// the tokens are spent either way and only one of the two outcomes tells the
// user what happened.
const kvasirBudget = 100 * time.Second

type opinionRequest struct {
	Surface string `json:"surface"`
	Subject string `json:"subject,omitempty"`
	// Refresh asks for a new answer even though the facts have not moved.
	Refresh bool `json:"refresh,omitempty"`
}

type opinionResponse struct {
	Surface     string           `json:"surface"`
	Subject     string           `json:"subject,omitempty"`
	Opinion     kvasir.Opinion   `json:"opinion"`
	Dropped     []kvasir.Dropped `json:"dropped,omitempty"`
	Brief       string           `json:"brief"`
	Model       string           `json:"model,omitempty"`
	Cached      bool             `json:"cached"`
	GeneratedAt string           `json:"generatedAt,omitempty"`
}

// handleKvasirStatus says whether there is a model to ask.
func (s *Server) handleKvasirStatus(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"enabled": s.Kvasir.Available()}
	if s.Kvasir.Available() {
		out["model"] = s.Kvasir.Client.Model
	}
	writeJSON(w, http.StatusOK, out)
}

// handleKvasirCheck probes the endpoint without spending a completion.
//
// A configured but unreachable endpoint is the usual failure here — a
// container pointing at 127.0.0.1 for a model running on the host — and it
// should be visible as such on the System page rather than as an opinion that
// never arrives.
func (s *Server) handleKvasirCheck(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if !s.Kvasir.Available() {
		writeError(w, r, http.StatusServiceUnavailable, "no language model is configured",
			"Set MIMIR_LLM_BASE_URL to an OpenAI-compatible endpoint. Everything else in Mimir works without one.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	models, err := s.Kvasir.Client.Models(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":       false,
			"endpoint": s.Kvasir.Client.BaseURL,
			"model":    s.Kvasir.Client.Model,
			"error":    err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"endpoint": s.Kvasir.Client.BaseURL,
		"model":    s.Kvasir.Client.Model,
		"models":   models,
	})
}

// handleKvasirOpinion answers "how do I get better?" for one page.
func (s *Server) handleKvasirOpinion(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())
	lang := i18n.FromRequest(r)

	if !s.Kvasir.Available() {
		writeError(w, r, http.StatusServiceUnavailable, "no language model is configured",
			"Set MIMIR_LLM_BASE_URL to an OpenAI-compatible endpoint. Everything else in Mimir works without one.")
		return
	}

	var req opinionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "malformed request", "")
		return
	}

	brief, err := s.briefFor(r.Context(), a, req.Surface, req.Subject, lang)
	if err != nil {
		s.writeKvasirError(w, r, err)
		return
	}
	if !brief.Facts() {
		writeError(w, r, http.StatusUnprocessableEntity,
			"there is nothing calculated to have an opinion about",
			"Import an account and set up a goal, and the engine has something to hand over.")
		return
	}

	hash := brief.Hash()
	if !req.Refresh {
		if cached, ok := s.cachedOpinion(r.Context(), a.ID, req.Surface, req.Subject, lang, hash); ok {
			cached.Brief = brief.Text()
			writeJSON(w, http.StatusOK, cached)
			return
		}
	}

	// The model's own budget, detached from the request so a user navigating
	// away mid-answer does not waste the completion that is already running.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), kvasirBudget)
	defer cancel()

	result, err := s.Kvasir.Advise(ctx, brief, lang)
	if err != nil {
		s.writeKvasirError(w, r, err)
		return
	}

	out := opinionResponse{
		Surface:     req.Surface,
		Subject:     req.Subject,
		Opinion:     result.Opinion,
		Dropped:     result.Dropped,
		Brief:       brief.Text(),
		Model:       result.Model,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	s.storeOpinion(ctx, a.ID, req.Surface, req.Subject, lang, hash, brief.Text(), out)
	writeJSON(w, http.StatusOK, out)
}

type chatRequest struct {
	Surface  string        `json:"surface,omitempty"`
	Subject  string        `json:"subject,omitempty"`
	Messages []kvasir.Turn `json:"messages"`
}

// handleKvasirChat answers a follow-up question against the engine.
func (s *Server) handleKvasirChat(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())
	lang := i18n.FromRequest(r)

	if !s.Kvasir.Available() {
		writeError(w, r, http.StatusServiceUnavailable, "no language model is configured",
			"Set MIMIR_LLM_BASE_URL to an OpenAI-compatible endpoint. Everything else in Mimir works without one.")
		return
	}

	var req chatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<18)).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "malformed request", "")
		return
	}
	req.Messages = trimHistory(req.Messages)
	if len(req.Messages) == 0 {
		writeError(w, r, http.StatusBadRequest, "there is no question to answer", "")
		return
	}

	// A brief that cannot be built is not a failure here: the conversation
	// still works, the model just has to fetch what it needs.
	brief, _ := s.briefFor(r.Context(), a, req.Surface, req.Subject, lang)

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), kvasirBudget)
	defer cancel()

	result, err := s.Kvasir.Chat(ctx, kvasir.ChatRequest{
		Brief:   brief,
		History: req.Messages,
		Lang:    lang,
		Runner:  &kvasirRunner{server: s, account: a, lang: lang},
	})
	if err != nil {
		s.writeKvasirError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// trimHistory bounds a conversation before it reaches the model: the last
// dozen turns, each capped. A context window filled by a pasted wall of text
// is one that has no room left for the facts.
func trimHistory(in []kvasir.Turn) []kvasir.Turn {
	const maxTurns, maxRunes = 12, 4000
	if len(in) > maxTurns {
		in = in[len(in)-maxTurns:]
	}
	out := make([]kvasir.Turn, 0, len(in))
	for _, t := range in {
		content := strings.TrimSpace(t.Content)
		if content == "" {
			continue
		}
		if r := []rune(content); len(r) > maxRunes {
			content = string(r[:maxRunes])
		}
		out = append(out, kvasir.Turn{Role: t.Role, Content: content})
	}
	return out
}

// writeKvasirError maps the layer's failures onto something actionable.
func (s *Server) writeKvasirError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, kvasir.ErrNotConfigured):
		writeError(w, r, http.StatusServiceUnavailable, "no language model is configured",
			"Set MIMIR_LLM_BASE_URL to an OpenAI-compatible endpoint. Everything else in Mimir works without one.")
	case errors.Is(err, kvasir.ErrNoFacts):
		writeError(w, r, http.StatusUnprocessableEntity,
			"there is nothing calculated to have an opinion about",
			"Import an account and set up a goal, and the engine has something to hand over.")
	case errors.Is(err, kvasir.ErrUnsourced):
		writeError(w, r, http.StatusUnprocessableEntity,
			"Kvasir used numbers that are not in the calculation, twice in a row",
			"The answer was discarded rather than shown. A smaller or better-instructed model usually fixes this.")
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, r, http.StatusGatewayTimeout, "the model did not answer in time",
			"A local model on a small machine can take longer than Mimir waits.")
	case errors.Is(err, gamedata.ErrNoSnapshot), errors.Is(err, gamedata.ErrMissing):
		writeDomainError(w, r, err)
	default:
		writeError(w, r, http.StatusUnprocessableEntity, err.Error(), "")
	}
}

// ---------------------------------------------------------------- the cache

func (s *Server) cachedOpinion(
	ctx context.Context, accountID int64, surface, subject string, lang i18n.Lang, hash string,
) (opinionResponse, bool) {
	var body, modelName, created string
	err := s.DB.QueryRowContext(ctx, `
		SELECT body, model, created_at FROM kvasir_opinions
		WHERE account_id = ? AND surface = ? AND subject = ? AND lang = ? AND facts_hash = ?`,
		accountID, surface, subject, string(lang), hash).Scan(&body, &modelName, &created)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) && s.Log != nil {
			s.Log.Warn("could not read a stored opinion", "error", err)
		}
		return opinionResponse{}, false
	}

	var stored struct {
		Opinion kvasir.Opinion   `json:"opinion"`
		Dropped []kvasir.Dropped `json:"dropped"`
	}
	if err := json.Unmarshal([]byte(body), &stored); err != nil {
		return opinionResponse{}, false
	}
	return opinionResponse{
		Surface: surface, Subject: subject,
		Opinion: stored.Opinion, Dropped: stored.Dropped,
		Model: modelName, Cached: true, GeneratedAt: created,
	}, true
}

// storeOpinion keeps the answer and the fact sheet that produced it.
//
// A failure here is not worth failing the request over — the user has their
// answer — but it is worth a log line, because a cache that silently never
// writes shows up as a model being asked the same question every page load.
func (s *Server) storeOpinion(
	ctx context.Context, accountID int64, surface, subject string,
	lang i18n.Lang, hash, brief string, out opinionResponse,
) {
	body, err := json.Marshal(map[string]any{"opinion": out.Opinion, "dropped": out.Dropped})
	if err != nil {
		return
	}
	if _, err := s.DB.ExecContext(ctx, `
		INSERT INTO kvasir_opinions (account_id, surface, subject, lang, facts_hash, model, body, brief)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id, surface, subject, lang, facts_hash) DO UPDATE SET
			model = excluded.model, body = excluded.body, brief = excluded.brief,
			created_at = datetime('now')`,
		accountID, surface, subject, string(lang), hash, out.Model, string(body), brief,
	); err != nil && s.Log != nil {
		s.Log.Warn("could not store an opinion", "error", err)
	}

	// Every change to the account mints a new row, because the hash is what
	// makes a stored answer safe to show. Left alone that grows without
	// bound, and the old rows are of no use to anybody: the UI only ever asks
	// about the facts as they are now, so an opinion about a build from four
	// re-imports ago can never be reached again.
	if _, err := s.DB.ExecContext(ctx, `
		DELETE FROM kvasir_opinions
		WHERE account_id = ? AND id NOT IN (
			SELECT id FROM kvasir_opinions WHERE account_id = ?
			ORDER BY created_at DESC, id DESC LIMIT ?)`,
		accountID, accountID, keptOpinions,
	); err != nil && s.Log != nil {
		s.Log.Warn("could not prune stored opinions", "error", err)
	}
}

// keptOpinions is how many answers an account keeps. Generous enough that a
// week of re-planning stays cached, small enough that the table cannot become
// the largest thing in the database.
const keptOpinions = 200

// ---------------------------------------------------------------- the tools

// kvasirRunner executes the engine calls the model asks for during a chat.
//
// Every tool returns a fact sheet built by the same code the opinion cards
// use, which is what keeps the two halves of this layer honest with each
// other: there is one definition of what Kvasir may know, and both paths go
// through it.
type kvasirRunner struct {
	server  *Server
	account model.Account
	lang    i18n.Lang
}

func (k *kvasirRunner) Run(ctx context.Context, name string, args map[string]any) (string, error) {
	character := argString(args, "character")

	switch name {
	case "account_plan":
		return k.brief(ctx, "plan", "")
	case "goal_plan":
		return k.brief(ctx, "goal", character)
	case "build_sheet":
		return k.brief(ctx, "character", character)
	case "roster":
		return k.brief(ctx, "roster", "")
	case "goals":
		return k.brief(ctx, "goals", "")
	case "talents":
		return k.talents(ctx, character)
	case "inventory":
		return k.inventory(ctx, argString(args, "set"), argString(args, "slot"))
	case "drop_model":
		return k.brief(ctx, "artifacts", "")
	default:
		return "", fmt.Errorf("%s", i18n.T(k.lang, "there is no such tool"))
	}
}

func (k *kvasirRunner) brief(ctx context.Context, surface, subject string) (string, error) {
	b, err := k.server.briefFor(ctx, k.account, surface, subject, k.lang)
	if err != nil {
		return "", err
	}
	return b.Text(), nil
}

// talents is the one fact sheet with no page of its own: the rotation editor
// shows the table, but a conversation about whether a talent is worth
// levelling needs the multipliers themselves.
func (k *kvasirRunner) talents(ctx context.Context, key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("%s", i18n.T(k.lang, "that needs a character"))
	}
	snap, err := k.server.GameData.Current()
	if err != nil {
		return "", err
	}
	def, err := snap.Char(key)
	if err != nil {
		return "", err
	}
	character, err := k.server.loadCharacter(ctx, k.account.ID, key)
	if err != nil {
		return "", fmt.Errorf("%s", i18n.T(k.lang, "%s is not on the account", key))
	}

	b := kvasir.NewBrief("talents", key, i18n.T(k.lang, "%s's talent table", key), "")
	for _, slot := range []string{"auto", "skill", "burst"} {
		talent, ok := def.Talents[slot]
		if !ok {
			continue
		}
		level := advisor.EffectiveTalentLevel(def, character, slot)
		sec := b.Add(i18n.T(k.lang, "%s — %s, at level %d", slot, talent.Name, level))
		for _, entry := range talent.Entries {
			value, err := entry.Multiplier(level)
			if err != nil {
				continue
			}
			unit := entry.Unit
			if unit == "" {
				unit = "%"
			}
			sec.Linef("%s: %s%s", entry.Label, num1(value*100), unit)
		}
	}
	return b.Text(), nil
}

func (k *kvasirRunner) inventory(ctx context.Context, setKey, slot string) (string, error) {
	inventory, err := db.LoadArtifacts(k.server.DB, k.account.ID)
	if err != nil {
		return "", err
	}
	if len(inventory) == 0 {
		return "", fmt.Errorf("%s", i18n.T(k.lang, "no artifacts have been imported yet"))
	}
	title := i18n.T(k.lang, "The artifact inventory on account %s", k.account.UID)
	b := kvasir.NewBrief("inventory", setKey, title, "")
	k.server.addInventoryFacts(b, inventory, setKey, slot, k.lang)
	if !b.Facts() {
		return "", fmt.Errorf("%s", i18n.T(k.lang, "nothing in the inventory matches that"))
	}
	return b.Text(), nil
}

func argString(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}
