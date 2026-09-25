// Package web is the HTTP layer: routes, session handling, and templates.
//
// It is thin on purpose. Handlers resolve a request into a session, call into
// package app, and render — no library calls of their own, and no logic that
// belongs a layer down.
package web

import (
	"context"
	"embed"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/mike-akdeniz/flowcore"
	"github.com/mike-akdeniz/flowcore/client/internal/app"
)

//go:embed templates/*.html
var templateFS embed.FS

const sessionCookie = "flowcore_client_session"

type contextKey struct{}

// Server wires the routes.
type Server struct {
	app       *app.App
	logger    *slog.Logger
	templates *template.Template
}

func NewServer(application *app.App, logger *slog.Logger) (*Server, error) {
	templates, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}

	return &Server{app: application, logger: logger, templates: templates}, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.showRelease)

	return s.withSession(mux)
}

// withSession resolves the cookie into a session, creating and seeding one on a
// first visit.
//
// This is the identity half of the boundary in its simplest form: the client
// decides who this visitor is. FlowCore has no session, no cookie, and no user —
// it only ever sees the opaque strings this layer hands it.
func (s *Server) withSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var session *app.Session

		if cookie, err := r.Cookie(sessionCookie); err == nil {
			session, _ = s.app.Sessions.Get(cookie.Value)
		}

		if session == nil {
			session = s.app.Sessions.Create()
			http.SetCookie(w, &http.Cookie{
				Name:     sessionCookie,
				Value:    session.ID,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
		}

		if err := s.app.EnsureSeeded(r.Context(), session); err != nil {
			s.logger.Error("seeding session", "session", session.ID, "err", err)
			http.Error(w, "could not prepare this session", http.StatusInternalServerError)

			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, session)))
	})
}

func sessionFrom(r *http.Request) *app.Session {
	session, _ := r.Context().Value(contextKey{}).(*app.Session)

	return session
}

// releasePage is what the template renders: the run's state from FlowCore, and
// the release itself from the client's own store. Two sources, which is the whole
// point — FlowCore knows where the work stands, the client knows what the work is
// about.
type releasePage struct {
	SessionID string
	Release   app.Release
	Subject   string
	State     flowcore.WorkflowState
}

func (s *Server) showRelease(w http.ResponseWriter, r *http.Request) {
	session := sessionFrom(r)
	release := session.Releases["v2.4.0"]

	state, err := s.app.Engine.GetState(r.Context(),
		session.SubjectReference(release.Version), session.DefinitionIDs[0])
	if err != nil {
		s.logger.Error("reading state", "session", session.ID, "err", err)
		http.Error(w, "could not read the workflow state", http.StatusInternalServerError)

		return
	}

	page := releasePage{
		SessionID: session.ID,
		Release:   release,
		Subject:   session.SubjectReference(release.Version),
		State:     state,
	}

	if err := s.templates.ExecuteTemplate(w, "index.html", page); err != nil {
		s.logger.Error("rendering", "err", err)
	}
}
