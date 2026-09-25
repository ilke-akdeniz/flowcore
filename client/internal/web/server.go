// Package web is the HTTP layer: routes, session handling, and templates.
//
// It is thin on purpose. Handlers resolve a request into a session and an
// identity, call into package app, and render. They hold no logic that belongs a
// layer down, and they never call the library directly — every flowcore call in
// this application goes through package app, which is what keeps the boundary
// legible in one place.
package web

import (
	"context"
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
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
	templates, err := template.New("").Funcs(templateFuncs()).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}

	return &Server{app: application, logger: logger, templates: templates}, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.showHome)
	mux.HandleFunc("POST /identity", s.switchIdentity)
	mux.HandleFunc("GET /release/{version}", s.showRelease)
	mux.HandleFunc("POST /release/{version}/complete", s.completeStep)
	mux.HandleFunc("POST /release/{version}/reassign", s.reassign)

	return s.withSession(mux)
}

// withSession resolves the cookie into a session, creating and seeding one on a
// first visit.
//
// This is the identity half of the boundary in its simplest form: the client
// decides who this visitor is. FlowCore has no session, no cookie and no user —
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

// page carries what every screen needs: who you are, who you could be, and the
// error from whatever POST bounced you here.
type page struct {
	SessionID string
	// CheckerMode is "canned" or the model being called, so a visitor can see
	// whether they are watching a real judgment or a script.
	CheckerMode string
	Identity    app.Identity
	Roster      []app.Identity
	Error       string
	// Return is where the identity switcher sends you back to, so changing who
	// you are acting as never moves you off the screen you were reading.
	Return string
}

func (s *Server) newPage(r *http.Request, session *app.Session) page {
	return page{
		SessionID:   session.ID,
		CheckerMode: s.app.Dispatcher.Mode(),
		Identity:    app.IdentityByReference(session.ActingAs),
		Roster:      app.Roster,
		Error:       r.URL.Query().Get("error"),
		Return:      r.URL.Path,
	}
}

type homePage struct {
	page
	Worklist []flowcore.AssignedStep
	Releases []app.Release
}

func (s *Server) showHome(w http.ResponseWriter, r *http.Request) {
	session := sessionFrom(r)
	current := app.IdentityByReference(session.ActingAs)

	worklist, err := s.app.Worklist(r.Context(), session, current)
	if err != nil {
		s.logger.Error("worklist", "session", session.ID, "err", err)
		http.Error(w, "could not read the worklist", http.StatusInternalServerError)

		return
	}

	releases := make([]app.Release, 0, len(session.Releases))
	for _, release := range session.Releases {
		releases = append(releases, release)
	}

	s.render(w, "home.html", homePage{
		page:     s.newPage(r, session),
		Worklist: worklist,
		Releases: releases,
	})
}

func (s *Server) switchIdentity(w http.ResponseWriter, r *http.Request) {
	session := sessionFrom(r)
	session.ActingAs = app.IdentityByReference(r.FormValue("reference")).Reference

	http.Redirect(w, r, r.FormValue("return"), http.StatusSeeOther)
}

type releasePage struct {
	page
	Release    app.Release
	Subject    string
	State      flowcore.WorkflowState
	Assignable []string
	// CanAct reports whether the current identity would find this step in its own
	// worklist. It greys the buttons; it does not gate them, because FlowCore does
	// not gate them either — anyone may complete any step, and the record says who
	// did. That is how a human overrides an agent.
	CanAct bool
	// AgentPending is true when the open step belongs to an agent, so the page can
	// say why nothing appears to be happening.
	AgentPending bool
}

func (s *Server) showRelease(w http.ResponseWriter, r *http.Request) {
	session := sessionFrom(r)
	release, ok := session.Releases[r.PathValue("version")]
	if !ok {
		http.NotFound(w, r)

		return
	}

	state, err := s.app.Engine.GetState(r.Context(),
		session.SubjectReference(release.Version), session.DefinitionIDs[0])
	if err != nil {
		s.logger.Error("reading state", "session", session.ID, "err", err)
		http.Error(w, "could not read the workflow state", http.StatusInternalServerError)

		return
	}

	current := app.IdentityByReference(session.ActingAs)

	canAct, agentPending := false, false
	if state.CurrentStep != nil {
		canAct = current.CanActAs(state.CurrentStep.AssigneeID)
		agentPending = app.IsAgent(state.CurrentStep.AssigneeID)
	}

	s.render(w, "release.html", releasePage{
		page:         s.newPage(r, session),
		Release:      release,
		Subject:      session.SubjectReference(release.Version),
		State:        state,
		Assignable:   app.AssignableReferences(),
		CanAct:       canAct,
		AgentPending: agentPending,
	})
}

func (s *Server) completeStep(w http.ResponseWriter, r *http.Request) {
	session := sessionFrom(r)
	version := r.PathValue("version")
	release := session.Releases[version]

	visitID, actionID, err := formIDs(r, "visit", "action")
	if err != nil {
		s.back(w, r, version, err)

		return
	}

	state, err := s.app.CompleteStep(r.Context(), app.IdentityByReference(session.ActingAs),
		app.CompleteRequest{
			VisitID:  visitID,
			ActionID: actionID,
			Remark:   r.FormValue("remark"),
			// The token comes from the client's own store, because only the client
			// knows what revision it is showing.
			SubjectVersionToken: release.Commit,
		})
	if err != nil {
		s.back(w, r, version, err)

		return
	}

	// Case-4 dispatch: this response already says whether an agent owns the next
	// step, so nothing has to poll to discover it. The request returns now and a
	// worker picks the step up afterwards.
	s.app.Dispatcher.Dispatch(session, session.DefinitionIDs[0], state)

	http.Redirect(w, r, "/release/"+version, http.StatusSeeOther)
}

func (s *Server) reassign(w http.ResponseWriter, r *http.Request) {
	session := sessionFrom(r)
	version := r.PathValue("version")

	visitID, err := uuid.Parse(r.FormValue("visit"))
	if err != nil {
		s.back(w, r, version, err)

		return
	}

	state, err := s.app.Reassign(r.Context(), visitID, r.FormValue("assignee"))
	if err != nil {
		s.back(w, r, version, err)

		return
	}

	// Moving a step to an agent hands it to the worker, exactly as routing into
	// one would. Nothing distinguishes the two cases, because nothing about an
	// agent is special — it is an assignee like any other.
	s.app.Dispatcher.Dispatch(session, session.DefinitionIDs[0], state)

	http.Redirect(w, r, "/release/"+version, http.StatusSeeOther)
}

// back returns to the release page carrying the error.
//
// The message is currently whatever the library said. Phase 4 translates the
// typed errors into sentences a person can act on, which is one of the things
// only a real client reveals about the API.
func (s *Server) back(w http.ResponseWriter, r *http.Request, version string, cause error) {
	s.logger.Warn("rejected", "path", r.URL.Path, "err", cause)
	http.Redirect(w, r,
		"/release/"+version+"?error="+url.QueryEscape(cause.Error()), http.StatusSeeOther)
}

func formIDs(r *http.Request, first, second string) (uuid.UUID, uuid.UUID, error) {
	a, err := uuid.Parse(r.FormValue(first))
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	b, err := uuid.Parse(r.FormValue(second))
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	return a, b, nil
}

// templateFuncs holds the one thing the templates cannot do themselves: recover a
// release version from the subject reference the worklist returns.
//
// The worklist is a library projection, so its rows carry FlowCore's opaque
// reference — "s7f3a2:release:v2.4.0" — and only this application knows that the
// last segment is a version it can route to. Interpreting that string is exactly
// the work the library refuses to do.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"releaseVersion": func(subjectReference string) string {
			parts := strings.Split(subjectReference, ":")

			return parts[len(parts)-1]
		},
	}
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		s.logger.Error("rendering", "template", name, "err", err)
	}
}
