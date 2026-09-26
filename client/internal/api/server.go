// Package api is the console's HTTP surface: a JSON API for the React
// application, plus the built assets themselves.
//
// The endpoints speak claims and applications, not FlowCore. One request per
// screen, returning what that screen needs already composed — the browser never
// learns the library's model, and never orchestrates. Domain rules that live in
// package app cannot be bypassed by a front end taking a shortcut.
//
// The one carved-out exception is the workflow editor, whose endpoints mirror
// Catalog closely, because that screen genuinely is about definitions, steps and
// actions.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/mike-akdeniz/flowcore/client/internal/app"
	"github.com/mike-akdeniz/flowcore/client/internal/store"
)

const sessionCookie = "console_session"

type contextKey struct{}

type Server struct {
	app    *app.App
	logger *slog.Logger
	assets http.Handler
}

func NewServer(application *app.App, logger *slog.Logger, assets http.Handler) *Server {
	return &Server{app: application, logger: logger, assets: assets}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/session", s.showSession)
	mux.HandleFunc("POST /api/session", s.signIn)
	mux.HandleFunc("GET /api/queue", s.showQueue)

	// Everything else is the single-page application: its own router owns the
	// paths, so any unmatched GET returns the shell.
	mux.Handle("/", s.assets)

	return s.withSession(mux)
}

// withSession resolves the cookie into a session, creating and seeding one on a
// first visit.
//
// Seeding copies a template dataset into rows tagged with this visitor's session.
// Hosting means concurrent visitors, and the console owns that isolation because
// FlowCore has no tenant of any kind.
func (s *Server) withSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Static assets do not need a session, and must not create one: seeding
		// builds two workflow definitions, so a crawler fetching a stylesheet
		// without a cookie would otherwise seed a whole dataset per request.
		if isAsset(r.URL.Path) {
			next.ServeHTTP(w, r)

			return
		}

		sessionID := ""
		if cookie, err := r.Cookie(sessionCookie); err == nil {
			sessionID = cookie.Value
		}

		if sessionID == "" {
			sessionID = app.NewSessionID()
			http.SetCookie(w, &http.Cookie{
				Name:     sessionCookie,
				Value:    sessionID,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
		}

		created, err := s.app.Store.TouchSession(r.Context(), sessionID)
		if err != nil {
			s.fail(w, "could not open a session", err)

			return
		}

		if created {
			if err := s.app.SeedSession(r.Context(), sessionID); err != nil {
				s.fail(w, "could not prepare this session", err)

				return
			}

			s.logger.Info("session seeded", "session", sessionID)
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, sessionID)))
	})
}

// isAsset reports whether a path names a built file rather than a route. The
// application's own routes never contain a dot; every asset Vite emits does.
func isAsset(path string) bool {
	return strings.Contains(strings.TrimPrefix(path, "/"), ".")
}

func sessionFrom(r *http.Request) string {
	sessionID, _ := r.Context().Value(contextKey{}).(string)

	return sessionID
}

// --- session ---------------------------------------------------------------

type staffJSON struct {
	Reference string   `json:"reference"`
	Name      string   `json:"name"`
	Title     string   `json:"title"`
	Groups    []string `json:"groups"`
}

type sessionJSON struct {
	SignedInAs *staffJSON  `json:"signedInAs"`
	Roster     []staffJSON `json:"roster"`
	AgentMode  string      `json:"agentMode"`
}

func (s *Server) showSession(w http.ResponseWriter, r *http.Request) {
	roster, err := s.app.Store.Roster(r.Context())
	if err != nil {
		s.fail(w, "could not read the roster", err)

		return
	}

	payload := sessionJSON{
		Roster:    make([]staffJSON, 0, len(roster)),
		AgentMode: s.app.Dispatcher.Mode(),
	}

	for _, member := range roster {
		payload.Roster = append(payload.Roster, toStaffJSON(member))
	}

	if member, ok := s.signedIn(r); ok {
		signedIn := toStaffJSON(member)
		payload.SignedInAs = &signedIn
	}

	s.write(w, payload)
}

// signIn is a choice from a list, not authentication. Identity is faked
// deliberately: it demonstrates nothing about the library and would be the
// largest code here.
func (s *Server) signIn(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reference string `json:"reference"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)

		return
	}

	member, err := s.app.Store.StaffByReference(r.Context(), body.Reference)
	if err != nil {
		http.Error(w, "no such person", http.StatusNotFound)

		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "console_identity",
		Value:    member.Reference,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	s.write(w, toStaffJSON(member))
}

func (s *Server) signedIn(r *http.Request) (store.Staff, bool) {
	cookie, err := r.Cookie("console_identity")
	if err != nil {
		return store.Staff{}, false
	}

	member, err := s.app.Store.StaffByReference(r.Context(), cookie.Value)
	if err != nil {
		return store.Staff{}, false
	}

	return member, true
}

// --- the queue -------------------------------------------------------------

type queueItemJSON struct {
	Reference    string `json:"reference"`
	Type         string `json:"type"`
	StepName     string `json:"stepName"`
	Assignee     string `json:"assignee"`
	WaitingSince string `json:"waitingSince"`
	IsDraft      bool   `json:"isDraft"`
}

func (s *Server) showQueue(w http.ResponseWriter, r *http.Request) {
	member, ok := s.signedIn(r)
	if !ok {
		http.Error(w, "not signed in", http.StatusUnauthorized)

		return
	}

	items, err := s.app.Queue(r.Context(), sessionFrom(r), app.Identity{
		Reference: member.Reference,
		Name:      member.Name,
		Title:     member.Title,
		Groups:    member.Groups,
	})
	if err != nil {
		s.fail(w, "could not read the queue", err)

		return
	}

	payload := make([]queueItemJSON, 0, len(items))
	for _, item := range items {
		payload = append(payload, queueItemJSON{
			Reference:    item.Reference,
			Type:         string(item.Type),
			StepName:     item.StepName,
			Assignee:     item.Assignee,
			WaitingSince: item.WaitingSince.Format("2006-01-02T15:04:05Z07:00"),
			IsDraft:      item.IsDraft,
		})
	}

	s.write(w, payload)
}

// --- helpers ---------------------------------------------------------------

func toStaffJSON(member store.Staff) staffJSON {
	groups := member.Groups
	if groups == nil {
		groups = []string{}
	}

	return staffJSON{
		Reference: member.Reference,
		Name:      member.Name,
		Title:     member.Title,
		Groups:    groups,
	}
}

func (s *Server) write(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		s.logger.Error("encoding response", "err", err)
	}
}

func (s *Server) fail(w http.ResponseWriter, message string, cause error) {
	s.logger.Error(message, "err", cause)
	http.Error(w, message, http.StatusInternalServerError)
}
