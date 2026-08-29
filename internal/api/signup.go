package api

// Signing up.
//
// The endpoint that turns a reader into a customer, and therefore the only
// unauthenticated thing on this service that creates state. Two consequences
// follow, and both are handled here rather than assumed away.
//
// It exists only where there is something to sell. A self-hosted Mimir has
// accounts made by whoever runs it, and an open signup form there would let
// anybody on the network help themselves.
//
// And it is rate limited. Every account gets fourteen free days, so an
// unmetered signup form is a machine for issuing free trials to whoever asks
// fastest. The limit is per address and deliberately loose: it is there to
// stop a script, not to make a person who mistyped their password twice wait.

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/kristianwind/mimir/internal/auth"
)

// signupWindow and signupBurst are how many accounts one address may create.
// Three in an hour covers a household and a shared office; it does not cover
// a loop.
const (
	signupWindow = time.Hour
	signupBurst  = 3
)

// handleSignup creates an account and signs the person in.
func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	if !s.Config.Hosted || !s.Config.AllowRegistration {
		writeError(w, http.StatusNotFound, "this instance does not take signups", "")
		return
	}
	if !s.signups.allow(clientAddr(r), time.Now()) {
		writeError(w, http.StatusTooManyRequests,
			"that is a lot of accounts from one place — try again later", "")
		return
	}

	var body struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request", "")
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	body.Email = strings.TrimSpace(body.Email)

	if body.Username == "" {
		writeError(w, http.StatusBadRequest, "pick a username", "")
		return
	}
	// An email is required here where it is optional elsewhere, because a
	// paying customer has to be reachable: a receipt has to go somewhere, and
	// so does the message saying a card stopped working.
	if !strings.Contains(body.Email, "@") {
		writeError(w, http.StatusBadRequest, "an email address is needed for receipts", "")
		return
	}
	if err := checkPassword(body.Password); err != nil {
		writeError(w, http.StatusBadRequest, passwordError(err), "")
		return
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	res, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO users (username, email, password_hash, role) VALUES (?, ?, ?, 'user')`,
		body.Username, body.Email, hash)
	if isUniqueViolation(err) {
		// Which of the two collided is not said. Telling somebody an email
		// is taken turns this form into a way of asking whether a person has
		// an account here.
		writeError(w, http.StatusConflict, "that username or email is already in use", "")
		return
	}
	if err != nil {
		writeDomainError(w, err)
		return
	}
	id, err := res.LastInsertId()
	if err != nil {
		writeDomainError(w, err)
		return
	}

	// Signed in straight away. They have just proved the password by choosing
	// it, and making somebody type it again to begin a trial is a step that
	// exists for nobody's benefit.
	token, err := s.Auth.Issue(r.Context(), id, r.UserAgent())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	s.Auth.SetCookie(w, token)
	s.audit(r, "user.signup", body.Username, nil)
	writeJSON(w, http.StatusOK, auth.User{ID: id, Username: body.Username, Role: "user"})
}
