// Package api is the HTTP layer: routing, request decoding, response shaping.
// It owns no domain logic — every number it returns comes from calc,
// optimizer or advisor.
package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/kristianwind/mimir/internal/auth"
	"github.com/kristianwind/mimir/internal/beacon"
	"github.com/kristianwind/mimir/internal/billing"
	"github.com/kristianwind/mimir/internal/config"
	"github.com/kristianwind/mimir/internal/enka"
	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/kvasir"
	"github.com/kristianwind/mimir/internal/selfupdate"
)

// Server wires the dependencies the handlers need.
type Server struct {
	Config   *config.Config
	DB       *sql.DB
	Auth     *auth.Store
	Enka     *enka.CachedClient
	GameData *gamedata.Store
	Log      *slog.Logger
	// Web is the built frontend bundle, embedded into the binary.
	Web fs.FS

	// Version is the running build, "dev" when unstamped.
	Version string
	// Passkeys verifies WebAuthn credentials. Unavailable, and saying so,
	// where no origin is configured.
	Passkeys *auth.Passkeys
	// Billing reads and writes what a user is entitled to. Present on every
	// install; on a self-hosted one it always answers yes.
	Billing *billing.Store
	// Stripe is nil-safe and reports itself unconfigured where there is
	// nothing to sell.
	Stripe *billing.Stripe
	// Beacon sends the one anonymous daily ping, when the operator has said
	// it may.
	Beacon *beacon.Beacon
	// Updater checks for releases and, where the deployment allows, installs
	// one.
	Updater *selfupdate.Updater
	// signups meters account creation per address. Built on first use so a
	// zero Server still works in tests.
	signups *signupLimiter
	// Mine is the game data sync job. One at a time.
	Mine *MineJob
	// Kvasir is the AI layer. Nil, or configured with no endpoint, and every
	// other part of Mimir behaves exactly as it does now — no number in the
	// product comes from a model.
	Kvasir *kvasir.Advisor
	// seoCache holds index.html with per-path meta tags written into it, so
	// a crawler that runs no JavaScript still gets a title and a description.
	// Built on first use from the embedded bundle.
	seoCache seo
	// Shutdown asks the process to exit so a supervisor restarts it. The
	// updater needs it: replacing the binary does nothing until the old
	// process leaves.
	Shutdown func()
}

// Router builds the HTTP handler.
func (s *Server) Router() http.Handler {
	if s.Mine == nil {
		s.Mine = &MineJob{}
	}
	if s.signups == nil {
		s.signups = newSignupLimiter()
	}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		// Unauthenticated by necessity: nobody can log in before the first
		// account exists. Both close themselves the moment it does.
		r.Get("/auth/bootstrap", s.handleBootstrapStatus)
		r.Post("/auth/bootstrap", s.handleBootstrap)

		// What face to show before sign-in. Public by definition: it is read
		// by a browser that has no session and cannot get one until it
		// knows whether this instance offers accounts at all.
		r.Get("/instance", s.handleInstance)

		// Stripe has no session, so the signature on the payload is the
		// whole of this endpoint's security. See internal/billing.
		r.Post("/stripe/webhook", s.handleStripeWebhook)

		// Signing in with a passkey needs no session and no username: the
		// credential says who it belongs to.
		r.Post("/auth/passkey/begin", s.handlePasskeyLoginBegin)
		r.Post("/auth/passkey/finish", s.handlePasskeyLoginFinish)

		// Creating an account. Present only where there is something to
		// sell; see internal/api/signup.go.
		r.Post("/auth/signup", s.handleSignup)

		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/logout", s.handleLogout)
		r.Get("/healthz", s.handleHealth)

		// The collector endpoint. Public because the instances reporting to
		// it have no credentials — and 404 when this instance is not a
		// collector, so it is not advertised where it would only refuse.
		r.Post("/beacon", s.handleBeaconPing)

		r.Group(func(r chi.Router) {
			r.Use(s.Auth.Middleware)

			r.Get("/me", s.handleMe)

			r.Route("/billing", func(r chi.Router) {
				r.Get("/", s.handleBillingStatus)
				r.Post("/checkout", s.handleCheckout)
				r.Post("/portal", s.handlePortal)
				// Comping is an administrator's decision about a person; the
				// handler checks the role.
				r.Post("/comp", s.handleComp)
			})

			// The second factor. Enrolling is an ordinary action; removing
			// it and reprinting the recovery codes are not, and those two
			// ask for the password again inside the handler.
			r.Route("/passkeys", func(r chi.Router) {
				r.Get("/", s.handlePasskeyList)
				r.Post("/begin", s.handlePasskeyRegisterBegin)
				r.Post("/finish", s.handlePasskeyRegisterFinish)
				// Removing a way in asks for the password again.
				r.Post("/{id}/delete", s.handlePasskeyDelete)
			})

			r.Route("/2fa", func(r chi.Router) {
				r.Get("/", s.handleTwoFactorStatus)
				r.Post("/begin", s.handleTwoFactorBegin)
				r.Post("/confirm", s.handleTwoFactorConfirm)
				r.Post("/recovery", s.handleTwoFactorRecovery)
				r.Post("/disable", s.handleTwoFactorDisable)
			})
			r.Put("/me/prefs", s.handleSetPrefs)
			r.Put("/me/password", s.handleChangeOwnPassword)

			r.Route("/users", func(r chi.Router) {
				r.Get("/", s.handleListUsers)
				r.Post("/", s.handleCreateUser)
				r.Put("/{userID}", s.handleUpdateUser)
				r.Delete("/{userID}", s.handleDeleteUser)
			})

			r.Get("/gamedata", s.handleGameDataStatus)
			r.Get("/kvasir", s.handleKvasirStatus)

			// The game's own character art, fetched once and kept. Behind
			// the session like everything else: which characters an instance
			// asks for is a fact about its household.
			r.Get("/art/{characterKey}", s.handleCharacterArt)
			r.Get("/art/set/{setKey}/{slot}", s.handleArtifactArt)

			r.Route("/system", func(r chi.Router) {
				r.Get("/", s.handleSystemStatus)
				r.Post("/update/check", s.handleCheckUpdate)
				r.Post("/update", s.handleApplyUpdate)
				r.Post("/rollback", s.handleRollback)
				r.Put("/beacon", s.handleSetBeacon)
				r.Get("/gamedata/mine", s.handleMineStatus)
				r.Post("/gamedata/mine", s.handleStartMine)
				r.Post("/kvasir/check", s.handleKvasirCheck)
				r.Get("/beacon/receiver", s.handleReceiverStats)
				r.Put("/beacon/receiver", s.handleSetReceiver)
			})

			r.Route("/accounts", func(r chi.Router) {
				r.Get("/", s.handleListAccounts)
				r.Post("/", s.handleCreateAccount)

				r.Route("/{accountID}", func(r chi.Router) {
					r.Use(s.requireAccount)
					r.Get("/", s.handleGetAccount)
					r.Delete("/", s.handleDeleteAccount)
					r.Post("/import/enka", s.handleImportEnka)
					r.Post("/import/good", s.handleImportGOOD)
					r.Get("/characters", s.handleListCharacters)
					r.Get("/artifacts", s.handleListArtifacts)
					r.Get("/weapons", s.handleListWeapons)
					r.Get("/talents/{characterKey}", s.handleTalentTable)
					r.Get("/build/{characterKey}", s.handleBuildSheet)

					r.Get("/goals", s.handleListGoals)
					r.Put("/goals", s.handleSaveGoal)
					r.Delete("/goals/{characterKey}", s.handleDeleteGoal)

					// The ranking that needs no goal, and the one endpoint
					// that writes goals from it.
					r.Get("/potential", s.handlePotential)
					r.Post("/goals/derive", s.handleDeriveGoals)

					// Somebody else's published showcase, measured on the
					// same yardstick as this account. Read-only in both
					// directions: nothing about this account is sent
					// anywhere, and nothing about theirs is kept.
					r.Get("/compare/{uid}", s.handleCompare)

					// What a character wants, computed rather than looked
					// up on a wiki: which set, which main stats, which
					// weapon — none of it filtered by what is in the bag.
					r.Get("/target/{characterKey}", s.handleTarget)

					r.Get("/dropmodel", s.handleDropModel)
					r.Get("/plan", s.handleAccountPlan)
					r.Get("/plan/{characterKey}", s.handlePlanForGoal)

					// The AI layer. Every other route above answers with a
					// number the engine produced; these two answer with
					// sentences about those numbers, and never with a
					// number of their own.
					r.Post("/kvasir/opinion", s.handleKvasirOpinion)
					r.Post("/kvasir/chat", s.handleKvasirChat)
				})
			})
		})
	})

	// Served whether or not there is a bundle: a crawler asking a headless
	// instance for robots.txt should be told no, not given a 404 it may read
	// as permission.
	r.Get("/robots.txt", s.handleRobots)
	r.Get("/sitemap.xml", s.handleSitemap)

	if s.Web != nil {
		r.Handle("/*", s.spa())
	}
	return r
}

// spa serves the built frontend, falling back to index.html so client-side
// routes survive a hard refresh.
//
// The fallback is where the document gets its title and description written
// in — see seo.go. Doing it here rather than at build time is what lets six
// addresses share one bundle and still describe themselves differently to a
// crawler that will never run the script.
func (s *Server) spa() http.Handler {
	files := http.FileServer(http.FS(s.Web))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Directories do not count as a hit. "/" resolves to ".", which
		// stats perfectly well and would hand the front page to the plain
		// file server — untouched, and so unreadable to a crawler. That is
		// the one page this whole file exists for.
		if info, err := fs.Stat(s.Web, trimLeadingSlash(r.URL.Path)); err == nil && !info.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		doc, ok := s.seoDocument(r.URL.Path)
		if !ok {
			// No bundle to read, or an unreadable one. Fall back to the
			// plain file server rather than inventing a page.
			r = r.Clone(r.Context())
			r.URL.Path = "/"
			files.ServeHTTP(w, r)
			return
		}
		// Written straight out rather than through ServeContent: there is no
		// meaningful modification time for a document assembled in memory,
		// and a Range request for an HTML page is nobody's use case.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(doc)))
		_, _ = io.WriteString(w, doc)
	})
}

func trimLeadingSlash(p string) string {
	if len(p) > 0 && p[0] == '/' {
		p = p[1:]
	}
	if p == "" {
		return "."
	}
	return p
}

// writeJSON sends a JSON response.
//
// It encodes into a buffer before touching the ResponseWriter. Encoding
// straight to the wire means a failure part-way — an unrepresentable float,
// say — leaves an already-sent 200 with a truncated body, which reaches the
// client as a mysterious empty success. Buffering turns that into a 500.
func writeJSON(w http.ResponseWriter, status int, v any) {
	if v == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		return
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		http.Error(w, `{"error":"could not send the response"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// apiError is the single error shape the frontend has to understand.
type apiError struct {
	Error string `json:"error"`
	// Hint is user-facing remediation. Several failures here are the user's
	// to fix — a private showcase, an unsynced game data table — and a bare
	// message with no next step is a support ticket waiting to happen.
	Hint string `json:"hint,omitempty"`
}

// writeError sends the one error shape the frontend understands.
func writeError(w http.ResponseWriter, status int, msg, hint string) {
	writeJSON(w, status, apiError{Error: msg, Hint: hint})
}

// writeDomainError maps known domain errors onto status codes and hints.
func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, enka.ErrNoShowcase):
		writeError(w, http.StatusUnprocessableEntity,
			"Enka has no characters for that UID",
			"Switch on Show Character Details in the game under Profile → Edit Profile, wait a couple of minutes, and try again.")
	case errors.Is(err, enka.ErrNotFound):
		writeError(w, http.StatusNotFound, "that UID does not exist", "Check the nine digits in the game's Paimon menu.")
	case errors.Is(err, enka.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, "Enka has rate-limited us", "Try again in a couple of minutes.")
	case errors.Is(err, gamedata.ErrNoSnapshot):
		writeError(w, http.StatusServiceUnavailable,
			"the game data has not been loaded yet",
			"Run a sync on the System page. Without it nothing can be calculated.")
	case errors.Is(err, gamedata.ErrMissing):
		// The underlying message names exactly what is missing — a character
		// id, a stat table — which is the only useful thing to show here.
		writeError(w, http.StatusServiceUnavailable,
			"the game data is out of date",
			fmt.Sprintf(
				"Something is missing from the active snapshot: %s. Sync a newer version.", err.Error()))
	default:
		writeError(w, http.StatusInternalServerError, err.Error(), "")
	}
}
