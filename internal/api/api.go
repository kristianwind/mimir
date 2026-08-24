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
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/kristianwind/mimir/internal/auth"
	"github.com/kristianwind/mimir/internal/beacon"
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
	// Beacon sends the one anonymous daily ping, when the operator has said
	// it may.
	Beacon *beacon.Beacon
	// Updater checks for releases and, where the deployment allows, installs
	// one.
	Updater *selfupdate.Updater
	// Mine is the game data sync job. One at a time.
	Mine *MineJob
	// Kvasir is the AI layer. Nil, or configured with no endpoint, and every
	// other part of Mimir behaves exactly as it does now — no number in the
	// product comes from a model.
	Kvasir *kvasir.Advisor
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
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		// Unauthenticated by necessity: nobody can log in before the first
		// account exists. Both close themselves the moment it does.
		r.Get("/auth/bootstrap", s.handleBootstrapStatus)
		r.Post("/auth/bootstrap", s.handleBootstrap)

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

	if s.Web != nil {
		r.Handle("/*", s.spa())
	}
	return r
}

// spa serves the built frontend, falling back to index.html so client-side
// routes survive a hard refresh.
func (s *Server) spa() http.Handler {
	files := http.FileServer(http.FS(s.Web))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(s.Web, trimLeadingSlash(r.URL.Path)); err != nil {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
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
