package api

// Passkeys.
//
// Two ceremonies, each in two halves. The server issues a challenge and
// remembers issuing it; the authenticator signs that exact challenge; the
// server checks the signature against a key it stored earlier. The half-way
// point is deliberately server-side state rather than something handed to the
// browser, because a challenge the caller could choose is not a challenge.
//
// Signing in with a passkey needs no session and no username. The credential
// says who it belongs to, which is friendlier than a form and also quieter: a
// sign-in page that asks for a name first tells anybody who types one whether
// that account exists.

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/kristianwind/mimir/internal/auth"
)

// maxCeremonyBody caps the authenticator's answer. These are a few kilobytes.
const maxCeremonyBody = 64 << 10

func (s *Server) handlePasskeyList(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	list, err := s.Passkeys.List(r.Context(), u.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available": s.Passkeys.Available(),
		"passkeys":  list,
	})
}

func (s *Server) handlePasskeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	opts, challenge, err := s.Passkeys.BeginRegistration(r.Context(), u)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"options": opts, "challenge": challenge})
}

func (s *Server) handlePasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var body struct {
		Challenge string          `json:"challenge"`
		Name      string          `json:"name"`
		Response  json.RawMessage `json:"response"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCeremonyBody)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request", "")
		return
	}
	if err := s.Passkeys.FinishRegistration(r.Context(), u, body.Challenge, body.Name, body.Response); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "")
		return
	}
	s.audit(r, "user.passkey.added", u.Username, map[string]any{"name": body.Name})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// handlePasskeyDelete removes one, after the password again.
//
// Same rule as turning the second factor off: a session proves somebody
// signed in once, and removing a way in is exactly the thing that has to
// survive a stolen one.
func (s *Server) handlePasskeyDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.reauthenticate(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "not an id", "")
		return
	}
	if err := s.Passkeys.Delete(r.Context(), u.ID, id); err != nil {
		writeDomainError(w, err)
		return
	}
	s.audit(r, "user.passkey.removed", u.Username, nil)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// handlePasskeyLoginBegin is unauthenticated, because signing in is what it
// is for. It reveals nothing: the challenge is random and the same for an
// account that exists and one that does not.
func (s *Server) handlePasskeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	opts, challenge, err := s.Passkeys.BeginLogin(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"options": opts, "challenge": challenge})
}

func (s *Server) handlePasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCeremonyBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "unreadable body", "")
		return
	}
	var wrapper struct {
		Challenge string          `json:"challenge"`
		Response  json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request", "")
		return
	}

	user, err := s.Passkeys.FinishLogin(r.Context(), wrapper.Challenge, wrapper.Response)
	if errors.Is(err, auth.ErrCloned) {
		// Worth saying out loud rather than folding into "sign-in failed".
		// A counter going backwards is the only evidence anybody ever gets
		// that a credential has been copied.
		s.Log.Warn("a passkey presented a counter that went backwards")
		writeError(w, http.StatusUnauthorized,
			"that passkey looks like it has been copied and was refused", "")
		return
	}
	if err != nil {
		s.Log.Warn("passkey sign-in refused", "error", err)
		writeError(w, http.StatusUnauthorized, "that passkey was not accepted", "")
		return
	}

	// A passkey signs in on its own. The authenticator verified the person
	// before it would sign, so demanding a password or a code as well would
	// bolt a phishable step onto an unphishable one.
	token, err := s.Auth.Issue(r.Context(), user.ID, r.UserAgent())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	s.Auth.SetCookie(w, token)
	s.audit(r, "user.login.passkey", user.Username, nil)
	writeJSON(w, http.StatusOK, user)
}
