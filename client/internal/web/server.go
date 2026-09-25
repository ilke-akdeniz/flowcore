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
	"sort"
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
	mux.HandleFunc("GET /subject/{kind}/{id}", s.showSubject)
	mux.HandleFunc("POST /subject/{kind}/{id}/complete", s.completeStep)
	mux.HandleFunc("POST /subject/{kind}/{id}/reassign", s.reassign)

	mux.HandleFunc("GET /workflows", s.showWorkflows)
	mux.HandleFunc("POST /workflows", s.createWorkflow)
	mux.HandleFunc("GET /workflows/{id}", s.showWorkflow)
	mux.HandleFunc("GET /workflows/{id}/fragment", s.showEditorFragment)
	mux.HandleFunc("POST /workflows/{id}/rename", s.renameWorkflow)
	mux.HandleFunc("POST /workflows/{id}/entry", s.setEntryStep)
	mux.HandleFunc("POST /workflows/{id}/statuses", s.addStatus)
	mux.HandleFunc("POST /workflows/{id}/statuses/{statusID}/delete", s.deleteStatus)
	mux.HandleFunc("POST /workflows/{id}/steps", s.addStep)
	mux.HandleFunc("POST /workflows/{id}/steps/{stepID}", s.updateStep)
	mux.HandleFunc("POST /workflows/{id}/steps/{stepID}/delete", s.deleteStep)
	mux.HandleFunc("POST /workflows/{id}/steps/{stepID}/actions", s.addAction)
	mux.HandleFunc("POST /workflows/{id}/actions/{actionID}/delete", s.deleteAction)

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
	// Traces is the call log: what this application did, and what it asked the
	// library, side by side.
	Traces []app.Trace
}

func (s *Server) newPage(r *http.Request, session *app.Session) page {
	return page{
		SessionID:   session.ID,
		CheckerMode: s.app.Dispatcher.Mode(),
		Identity:    app.IdentityByReference(session.ActingAs),
		Roster:      app.Roster,
		Error:       r.URL.Query().Get("error"),
		Return:      r.URL.Path,
		Traces:      session.Tracer.Traces(),
	}
}

type homePage struct {
	page
	Worklist []flowcore.AssignedStep
	Runs     []app.Run
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

	runs := make([]app.Run, 0, len(session.Runs))
	for _, run := range session.Runs {
		runs = append(runs, run)
	}

	sort.Slice(runs, func(i, j int) bool {
		return runs[i].Subject.Reference() < runs[j].Subject.Reference()
	})

	s.render(w, "home.html", homePage{
		page:     s.newPage(r, session),
		Worklist: worklist,
		Runs:     runs,
	})
}

func (s *Server) switchIdentity(w http.ResponseWriter, r *http.Request) {
	session := sessionFrom(r)
	session.ActingAs = app.IdentityByReference(r.FormValue("reference")).Reference

	http.Redirect(w, r, r.FormValue("return"), http.StatusSeeOther)
}

type subjectPage struct {
	page
	Run       app.Run
	Reference string
	// IDPart is the subject's reference without its kind, for building form
	// actions back to this page.
	IDPart     string
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

func (s *Server) showSubject(w http.ResponseWriter, r *http.Request) {
	session := sessionFrom(r)

	run, ok := session.RunFor(subjectReference(r))
	if !ok {
		http.NotFound(w, r)

		return
	}

	state, err := s.app.Engine.GetState(r.Context(),
		session.SubjectReference(run.Subject), run.DefinitionID)
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

	s.render(w, "subject.html", subjectPage{
		page:         s.newPage(r, session),
		Run:          run,
		Reference:    session.SubjectReference(run.Subject),
		IDPart:       r.PathValue("id"),
		State:        state,
		Assignable:   app.AssignableReferences(),
		CanAct:       canAct,
		AgentPending: agentPending,
	})
}

func (s *Server) completeStep(w http.ResponseWriter, r *http.Request) {
	session := sessionFrom(r)
	run, _ := session.RunFor(subjectReference(r))

	visitID, actionID, err := formIDs(r, "visit", "action")
	if err != nil {
		s.back(w, r, err)

		return
	}

	state, err := s.app.CompleteStep(r.Context(), session, app.IdentityByReference(session.ActingAs),
		app.CompleteRequest{
			VisitID:  visitID,
			ActionID: actionID,
			Remark:   r.FormValue("remark"),
			// The token comes from the client's own store, because only the client
			// knows what revision it is showing.
			SubjectVersionToken: run.Subject.VersionToken(),
		})
	if err != nil {
		s.back(w, r, err)

		return
	}

	// Case-4 dispatch: this response already says whether an agent owns the next
	// step, so nothing has to poll to discover it. The request returns now and a
	// worker picks the step up afterwards.
	s.app.Dispatcher.Dispatch(session, run.DefinitionID, state)

	http.Redirect(w, r, subjectPath(r), http.StatusSeeOther)
}

func (s *Server) reassign(w http.ResponseWriter, r *http.Request) {
	session := sessionFrom(r)
	run, _ := session.RunFor(subjectReference(r))

	visitID, err := uuid.Parse(r.FormValue("visit"))
	if err != nil {
		s.back(w, r, err)

		return
	}

	state, err := s.app.Reassign(r.Context(), session, visitID, r.FormValue("assignee"))
	if err != nil {
		s.back(w, r, err)

		return
	}

	// Moving a step to an agent hands it to the worker, exactly as routing into
	// one would. Nothing distinguishes the two cases, because nothing about an
	// agent is special — it is an assignee like any other.
	s.app.Dispatcher.Dispatch(session, run.DefinitionID, state)

	http.Redirect(w, r, subjectPath(r), http.StatusSeeOther)
}

// back returns to the release page carrying the error.
//
// The message is currently whatever the library said. Phase 4 translates the
// typed errors into sentences a person can act on, which is one of the things
// only a real client reveals about the API.
// back returns to the subject page carrying the error, translated into a sentence
// by the same function the workflow editor uses.
func (s *Server) back(w http.ResponseWriter, r *http.Request, cause error) {
	s.logger.Warn("rejected", "path", r.URL.Path, "err", cause)
	http.Redirect(w, r,
		subjectPath(r)+"?error="+url.QueryEscape(app.ErrorMessage(cause)), http.StatusSeeOther)
}

// subjectReference rebuilds the subject's own reference from the path, and
// subjectPath does the reverse. "release:v2.4.0" is one path segment per half.
func subjectReference(r *http.Request) string {
	return r.PathValue("kind") + ":" + r.PathValue("id")
}

func subjectPath(r *http.Request) string {
	return "/subject/" + r.PathValue("kind") + "/" + r.PathValue("id")
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
		// subjectPath turns the reference FlowCore stored —
		// "s7f3a2:claim:C-1042" — into a link. Only this application knows that
		// the first segment is a session and the rest names a subject; the
		// library stores the whole string and never looks inside it.
		"subjectPath": func(storedReference string) string {
			parts := strings.SplitN(storedReference, ":", 3)
			if len(parts) < 3 {
				return "/"
			}

			return "/subject/" + parts[1] + "/" + parts[2]
		},
		// stepName resolves an action's destination to a name, so the editor can say
		// "approve → finance review" rather than showing an id.
		"stepName": func(definition flowcore.WorkflowDefinition, id *uuid.UUID) string {
			if id == nil {
				return ""
			}

			for _, step := range definition.Steps {
				if step.ID == *id {
					return step.Name
				}
			}

			return "(deleted)"
		},
		// subjectID is the half of a subject's own reference that is not its kind.
		"subjectID": func(subject app.Subject) string {
			_, id, _ := strings.Cut(subject.Reference(), ":")

			return id
		},
	}
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		s.logger.Error("rendering", "template", name, "err", err)
	}
}
