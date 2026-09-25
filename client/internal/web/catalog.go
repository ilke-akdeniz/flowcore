package web

import (
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"github.com/mike-akdeniz/flowcore"
	"github.com/mike-akdeniz/flowcore/client/internal/app"
)

type workflowsPage struct {
	page
	Definitions []flowcore.WorkflowDefinition
	Assignable  []string
}

func (s *Server) showWorkflows(w http.ResponseWriter, r *http.Request) {
	session := sessionFrom(r)

	definitions, err := s.app.Definitions(r.Context(), session)
	if err != nil {
		s.logger.Error("listing definitions", "err", err)
		http.Error(w, "could not read your workflows", http.StatusInternalServerError)

		return
	}

	s.render(w, "workflows.html", workflowsPage{
		page:        s.newPage(r, session),
		Definitions: definitions,
		Assignable:  app.AssignableReferences(),
	})
}

func (s *Server) createWorkflow(w http.ResponseWriter, r *http.Request) {
	session := sessionFrom(r)

	definition, err := s.app.CreateDefinition(r.Context(), session, app.NewDefinition{
		Name:       r.FormValue("name"),
		StatusName: r.FormValue("status"),
		StepName:   r.FormValue("step"),
		AssigneeID: r.FormValue("assignee"),
	})
	if err != nil {
		s.toWorkflows(w, r, err)

		return
	}

	http.Redirect(w, r, "/workflows/"+definition.ID.String(), http.StatusSeeOther)
}

type workflowPage struct {
	page
	Definition flowcore.WorkflowDefinition
	Mermaid    string
	Assignable []string
	// Startable reports whether a run can begin: a definition with no entry step
	// is editable but not runnable, which the library refuses at Start rather than
	// at edit time.
	Startable bool
	// EntryStepID and SelectedID are strings because template `eq` compares basic
	// kinds and a uuid.UUID is a byte array.
	EntryStepID string
	SelectedID  string
	// Selected is the step being edited, or nil when none is chosen. The graph is
	// the navigation: clicking a node picks the step this panel edits.
	Selected *flowcore.StepDefinition
	// FirstStatusID seeds the quick "add step" form, which asks only for a name.
	FirstStatusID string
}

func (s *Server) showWorkflow(w http.ResponseWriter, r *http.Request) {
	page, ok := s.buildEditor(w, r, "")
	if !ok {
		return
	}

	s.render(w, "workflow.html", page)
}

// showEditorFragment serves just the editor, for HTMX. Selecting a step is a
// navigation, so it swaps the same fragment every edit returns.
func (s *Server) showEditorFragment(w http.ResponseWriter, r *http.Request) {
	page, ok := s.buildEditor(w, r, "")
	if !ok {
		return
	}

	s.render(w, "editor", page)
}

// buildEditor assembles the editor's view of a definition, including which step is
// selected. A missing or unknown selection leaves Selected nil, which the template
// renders as a prompt rather than an error.
func (s *Server) buildEditor(w http.ResponseWriter, r *http.Request, message string) (workflowPage, bool) {
	session := sessionFrom(r)

	definitionID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)

		return workflowPage{}, false
	}

	definition, err := s.app.Definition(r.Context(), session, definitionID)
	if err != nil {
		s.toWorkflows(w, r, err)

		return workflowPage{}, false
	}

	page := s.newPage(r, session)
	if message != "" {
		page.Error = message
	}

	selectedID := r.URL.Query().Get("step")

	var selected *flowcore.StepDefinition
	for i := range definition.Steps {
		if definition.Steps[i].ID.String() == selectedID {
			selected = &definition.Steps[i]
		}
	}

	// Default to the entry step, so the panel is never empty on arrival.
	if selected == nil && len(definition.Steps) > 0 {
		selected = &definition.Steps[0]
		for i := range definition.Steps {
			if definition.Steps[i].ID.String() == entryStepID(definition) {
				selected = &definition.Steps[i]
			}
		}
	}

	firstStatus := ""
	if len(definition.Statuses) > 0 {
		firstStatus = definition.Statuses[0].ID.String()
	}

	if selected != nil {
		selectedID = selected.ID.String()
	}

	return workflowPage{
		page:          page,
		Definition:    definition,
		Mermaid:       app.Mermaid(definition),
		Assignable:    app.AssignableReferences(),
		Startable:     definition.InitialStepDefinitionID != nil,
		EntryStepID:   entryStepID(definition),
		SelectedID:    selectedID,
		Selected:      selected,
		FirstStatusID: firstStatus,
	}, true
}

// edit runs one catalog change and returns to the editor, carrying any error.
//
// Every mutation below is a form post that either works or produces a sentence.
// The sentences come from app.ErrorMessage, which is where the library's typed
// errors become advice — and the reason they were worth typing.
func (s *Server) edit(w http.ResponseWriter, r *http.Request, change func(*app.Session, uuid.UUID) error) {
	session := sessionFrom(r)

	definitionID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)

		return
	}

	message := ""
	if err := change(session, definitionID); err != nil {
		s.logger.Warn("edit rejected", "path", r.URL.Path, "err", err)
		message = app.ErrorMessage(err)
	}

	// With HTMX the whole editor comes back and is swapped in place: one fragment,
	// always internally consistent, and no page reload. Without it — scripting off,
	// or a plain curl — the same handler falls back to a redirect, so nothing here
	// depends on JavaScript to work.
	if r.Header.Get("HX-Request") == "true" {
		if page, ok := s.buildEditor(w, r, message); ok {
			s.render(w, "editor", page)
		}

		return
	}

	target := "/workflows/" + definitionID.String()
	if message != "" {
		target += "?error=" + url.QueryEscape(message)
	}

	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *Server) toWorkflows(w http.ResponseWriter, r *http.Request, cause error) {
	s.logger.Warn("rejected", "path", r.URL.Path, "err", cause)
	http.Redirect(w, r, "/workflows?error="+url.QueryEscape(app.ErrorMessage(cause)), http.StatusSeeOther)
}

func (s *Server) renameWorkflow(w http.ResponseWriter, r *http.Request) {
	s.edit(w, r, func(session *app.Session, definitionID uuid.UUID) error {
		return s.app.RenameDefinition(r.Context(), session, definitionID, r.FormValue("name"))
	})
}

func (s *Server) setEntryStep(w http.ResponseWriter, r *http.Request) {
	s.edit(w, r, func(session *app.Session, definitionID uuid.UUID) error {
		stepID, err := uuid.Parse(r.FormValue("step"))
		if err != nil {
			return err
		}

		return s.app.SetEntryStep(r.Context(), session, definitionID, stepID)
	})
}

func (s *Server) addStatus(w http.ResponseWriter, r *http.Request) {
	s.edit(w, r, func(session *app.Session, definitionID uuid.UUID) error {
		return s.app.AddStatus(r.Context(), session, definitionID, r.FormValue("name"))
	})
}

func (s *Server) deleteStatus(w http.ResponseWriter, r *http.Request) {
	s.edit(w, r, func(session *app.Session, definitionID uuid.UUID) error {
		statusID, err := uuid.Parse(r.PathValue("statusID"))
		if err != nil {
			return err
		}

		return s.app.DeleteStatus(r.Context(), session, definitionID, statusID)
	})
}

func (s *Server) addStep(w http.ResponseWriter, r *http.Request) {
	s.edit(w, r, func(session *app.Session, definitionID uuid.UUID) error {
		request, err := stepRequest(r)
		if err != nil {
			return err
		}

		return s.app.AddStep(r.Context(), session, definitionID, request)
	})
}

func (s *Server) updateStep(w http.ResponseWriter, r *http.Request) {
	s.edit(w, r, func(session *app.Session, definitionID uuid.UUID) error {
		stepID, err := uuid.Parse(r.PathValue("stepID"))
		if err != nil {
			return err
		}

		request, err := stepRequest(r)
		if err != nil {
			return err
		}

		return s.app.UpdateStep(r.Context(), session, definitionID, stepID, request)
	})
}

func (s *Server) deleteStep(w http.ResponseWriter, r *http.Request) {
	s.edit(w, r, func(session *app.Session, definitionID uuid.UUID) error {
		stepID, err := uuid.Parse(r.PathValue("stepID"))
		if err != nil {
			return err
		}

		return s.app.DeleteStep(r.Context(), session, definitionID, stepID)
	})
}

func (s *Server) addAction(w http.ResponseWriter, r *http.Request) {
	s.edit(w, r, func(session *app.Session, definitionID uuid.UUID) error {
		stepID, err := uuid.Parse(r.PathValue("stepID"))
		if err != nil {
			return err
		}

		request := app.AddActionRequest{Name: r.FormValue("name")}

		// The form posts one "target" field holding either a step or a status id,
		// prefixed to say which. Exactly one of the two may be set, and the schema
		// enforces that too — an action with both or neither is unrepresentable.
		kind, raw, found := cut(r.FormValue("target"))
		if !found {
			return errBadTarget
		}

		target, err := uuid.Parse(raw)
		if err != nil {
			return err
		}

		switch kind {
		case "step":
			request.NextStepID = &target
		case "status":
			request.TerminalStatusID = &target
		default:
			return errBadTarget
		}

		return s.app.AddAction(r.Context(), session, definitionID, stepID, request)
	})
}

func (s *Server) deleteAction(w http.ResponseWriter, r *http.Request) {
	s.edit(w, r, func(session *app.Session, definitionID uuid.UUID) error {
		actionID, err := uuid.Parse(r.PathValue("actionID"))
		if err != nil {
			return err
		}

		return s.app.DeleteAction(r.Context(), session, definitionID, actionID)
	})
}

func stepRequest(r *http.Request) (app.AddStepRequest, error) {
	statusID, err := uuid.Parse(r.FormValue("status"))
	if err != nil {
		return app.AddStepRequest{}, err
	}

	return app.AddStepRequest{
		Name:       r.FormValue("name"),
		StatusID:   statusID,
		AssigneeID: r.FormValue("assignee"),
	}, nil
}

var errBadTarget = errBadTargetType{}

type errBadTargetType struct{}

func (errBadTargetType) Error() string {
	return "an action has to go to a step or to a terminal status"
}

func cut(value string) (string, string, bool) {
	for i := range value {
		if value[i] == ':' {
			return value[:i], value[i+1:], true
		}
	}

	return "", "", false
}

func entryStepID(definition flowcore.WorkflowDefinition) string {
	if definition.InitialStepDefinitionID == nil {
		return ""
	}

	return definition.InitialStepDefinitionID.String()
}
