package api

// Enrolling, proving and removing a second factor.
//
// The rule that shapes this file: adding protection is an ordinary action, and
// removing it is not. Anyone holding a live session may turn a factor on —
// the worst case is that they protect somebody else's account. Turning one
// off, or reprinting the recovery codes that bypass it, needs the password
// again, because otherwise a stolen session is all it takes to undo the thing
// the factor exists to survive.

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/kristianwind/mimir/internal/auth"
)

func (s *Server) twoFactor() *auth.TwoFactor { return s.Auth.TwoFactor }

func (s *Server) handleTwoFactorStatus(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	status, err := s.twoFactor().Status(r.Context(), u.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// handleTwoFactorBegin generates a secret and returns it once.
//
// The secret is in the response body because it has to be — it is what the
// authenticator app stores. It is never returned again: after confirmation
// the only copy outside the user's phone is the sealed one in the database.
func (s *Server) handleTwoFactorBegin(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	secret, uri, err := s.twoFactor().Begin(r.Context(), u.ID, u.Username)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	s.audit(r, "user.2fa.begin", u.Username, nil)
	writeJSON(w, http.StatusOK, map[string]any{"secret": secret, "uri": uri})
}

// handleTwoFactorConfirm proves a code and switches the factor on.
func (s *Server) handleTwoFactorConfirm(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request", "")
		return
	}
	codes, err := s.twoFactor().Confirm(r.Context(), u.ID, body.Code)
	if errors.Is(err, auth.ErrSecondFactorInvalid) {
		writeError(w, http.StatusBadRequest,
			"that code is not right — check your phone's clock is correct", "")
		return
	}
	if err != nil {
		writeDomainError(w, err)
		return
	}
	s.audit(r, "user.2fa.enabled", u.Username, nil)
	writeJSON(w, http.StatusOK, map[string]any{"recoveryCodes": codes})
}

// handleTwoFactorDisable removes the factor, after the password again.
func (s *Server) handleTwoFactorDisable(w http.ResponseWriter, r *http.Request) {
	u, ok := s.reauthenticate(w, r)
	if !ok {
		return
	}
	if err := s.twoFactor().Disable(r.Context(), u.ID); err != nil {
		writeDomainError(w, err)
		return
	}
	s.audit(r, "user.2fa.disabled", u.Username, nil)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// handleTwoFactorRecovery reprints the recovery codes, voiding the old set.
//
// Behind the password for the same reason as disabling: these codes are a way
// past the factor, so handing them out is handing out the factor.
func (s *Server) handleTwoFactorRecovery(w http.ResponseWriter, r *http.Request) {
	u, ok := s.reauthenticate(w, r)
	if !ok {
		return
	}
	codes, err := s.twoFactor().RegenerateRecoveryCodes(r.Context(), u.ID)
	if errors.Is(err, auth.ErrNotEnrolled) {
		writeError(w, http.StatusBadRequest, "there is no second factor to recover", "")
		return
	}
	if err != nil {
		writeDomainError(w, err)
		return
	}
	s.audit(r, "user.2fa.recovery", u.Username, nil)
	writeJSON(w, http.StatusOK, map[string]any{"recoveryCodes": codes})
}

// reauthenticate demands the account password again for an action that
// weakens the account.
//
// A session is evidence that someone signed in once. For turning protection
// off it is not enough evidence, because surviving a stolen session is
// precisely what the protection is for.
// It shares the sign-in limiter rather than keeping its own, on purpose. The
// two are the same resource — somebody guessing a password from one address —
// and separate budgets would let an attacker take ten guesses at the login
// form and then ten more here by walking between doors.
//
// It needs a limit at all because of what it guards. Without one the path is:
// steal a session, grind the password through this oracle as fast as the CPU
// allows, then use it to turn the second factor off and delete the passkeys.
// A second factor whose removal is protected only by an unmetered password
// prompt is not a second factor.
func (s *Server) reauthenticate(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	u, _ := auth.FromContext(r.Context())

	addr := clientAddr(r)
	if s.logins != nil && s.logins.over(addr, time.Now()) {
		writeError(w, http.StatusTooManyRequests,
			"too many attempts from here — wait a few minutes", "")
		return auth.User{}, false
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request", "")
		return auth.User{}, false
	}
	var hash string
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT password_hash FROM users WHERE id = ?`, u.ID).Scan(&hash); err != nil {
		writeDomainError(w, err)
		return auth.User{}, false
	}
	ok, err := auth.VerifyPassword(body.Password, hash)
	if err != nil {
		writeDomainError(w, err)
		return auth.User{}, false
	}
	if !ok {
		if s.logins != nil {
			s.logins.record(addr, time.Now())
		}
		// Audited like a failed sign-in, because that is what it is: somebody
		// proving they do not know the password, at the door that guards
		// turning the second factor off.
		s.audit(r, "user.login.failed", u.Username, map[string]any{"reason": "reauthentication"})
		if s.Log != nil {
			s.Log.Warn("re-authentication refused", "username", u.Username, "from", addr)
		}
		writeError(w, http.StatusForbidden, "that password is wrong", "")
		return auth.User{}, false
	}
	return u, true
}
