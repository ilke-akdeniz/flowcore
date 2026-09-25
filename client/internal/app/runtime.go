package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/mike-akdeniz/flowcore"
)

// Owns reports whether this definition belongs to this session.
func (s *Session) Owns(definitionID uuid.UUID) bool {
	for _, own := range s.DefinitionIDs {
		if own == definitionID {
			return true
		}
	}

	return false
}

// Worklist returns the open steps waiting on this identity, within this session.
//
// Two things happen here that FlowCore deliberately will not do. The identity is
// expanded into the set of references it answers to — itself plus its groups —
// because deciding who belongs to a group needs an identity model the library
// does not have. And the result is filtered to this session's own definitions,
// because the library has no tenant and the worklist spans every run in the
// database.
//
// The filter is possible because AssignedStep carries WorkflowDefinitionID, so no
// assignee string has to be namespaced: a visitor who types "group:security" into
// the configuration form gets exactly that stored.
func (a *App) Worklist(ctx context.Context, session *Session, identity Identity) ([]flowcore.AssignedStep, error) {
	references := identity.WorklistReferences()

	trace := session.Tracer.Start("Open " + identity.Name + "'s worklist")
	trace.Client("resolve %s → %v (this person, plus their groups)", identity.Name, references)

	assigned, err := a.Engine.ListAssignedSteps(ctx, references)
	if err != nil {
		return nil, err
	}

	trace.Call("engine.ListAssignedSteps(%v)", references)
	trace.Return("%d open steps, across every run in the database", len(assigned))

	mine := make([]flowcore.AssignedStep, 0, len(assigned))
	for _, step := range assigned {
		if session.Owns(step.WorkflowDefinitionID) {
			mine = append(mine, step)
		}
	}

	trace.Client("filter to this session's own definitions → %d", len(mine))
	session.Tracer.Record(trace)

	return mine, nil
}

// CompleteRequest is what the client needs to record a decision.
type CompleteRequest struct {
	VisitID  uuid.UUID
	ActionID uuid.UUID
	// Remark is optional: why this decision was made, stamped in the same
	// transaction as the decision itself so a crash cannot separate them.
	Remark string
	// SubjectVersionToken is the revision the decision was made against. The
	// client supplies it from its own store; FlowCore records it and never
	// compares it, so noticing that a subject moved on is this layer's job.
	SubjectVersionToken string
}

// CompleteStep records a decision by this identity.
//
// CompletedBy is the identity's opaque reference. Note what is not checked: the
// library never asks whether this person was the assignee. That is what makes a
// human override of an agent step possible with no special mechanism — the
// completer simply need not be the assignee.
func (a *App) CompleteStep(
	ctx context.Context,
	session *Session,
	identity Identity,
	request CompleteRequest,
) (flowcore.WorkflowState, error) {
	params := flowcore.CompleteParams{
		VisitID:     request.VisitID,
		ActionID:    request.ActionID,
		CompletedBy: identity.Reference,
	}

	if request.Remark != "" {
		params.Remark = &request.Remark
	}

	if request.SubjectVersionToken != "" {
		params.SubjectVersionToken = &request.SubjectVersionToken
	}

	trace := session.Tracer.Start(identity.Label() + " completes a step")
	trace.Client("resolve the signed-in visitor → %s", identity.Reference)
	trace.Client("read the subject's current revision from this application's own store → %s",
		request.SubjectVersionToken)
	trace.Call("engine.CompleteStep(visit=%s, action=%s, completedBy=%q%s)",
		short(request.VisitID.String()), short(request.ActionID.String()),
		identity.Reference, remarkNote(request.Remark))

	state, err := a.Engine.CompleteStep(ctx, params)
	if err != nil {
		trace.Return("refused: %s", ErrorMessage(err))
		session.Tracer.Record(trace)

		return state, err
	}

	trace.Return("%s", describeState(state))
	trace.Client("%s", nextOwner(state))
	session.Tracer.Record(trace)

	return state, nil
}

// Reassign moves an open visit to another assignee.
//
// There is no unassign: FlowCore requires a value, and work with no assignee
// would match no worklist query, so releasing it that way would hide it rather
// than free it.
func (a *App) Reassign(ctx context.Context, session *Session, visitID uuid.UUID, assignee string) (flowcore.WorkflowState, error) {
	trace := session.Tracer.Start("Move a step to " + assignee)
	trace.Call("engine.Reassign(visit=%s, assigneeID=%q)", short(visitID.String()), assignee)

	state, err := a.Engine.Reassign(ctx, visitID, assignee)
	if err != nil {
		trace.Return("refused: %s", ErrorMessage(err))
		session.Tracer.Record(trace)

		return state, err
	}

	trace.Return("%s", describeState(state))
	trace.Client("it now appears in whichever queue matches %q, and no other", assignee)
	session.Tracer.Record(trace)

	return state, nil
}

func short(id string) string {
	if len(id) < 8 {
		return id
	}

	return id[:8] + "…"
}

func remarkNote(remark string) string {
	if remark == "" {
		return ""
	}

	return ", remark=…"
}

func describeState(state flowcore.WorkflowState) string {
	if state.CurrentStep == nil {
		return fmt.Sprintf("run finished, status %q", state.WorkflowStatusName)
	}

	return fmt.Sprintf("status %q, now on %q assigned to %q",
		state.WorkflowStatusName, state.CurrentStep.Name, state.CurrentStep.AssigneeID)
}

func nextOwner(state flowcore.WorkflowState) string {
	if state.CurrentStep == nil {
		return "nothing is open; the run is over"
	}

	if IsAgent(state.CurrentStep.AssigneeID) {
		return "an agent owns the next step — queue it and return the response now"
	}

	return "render it into that group's queue; nothing runs until a person acts"
}

// AssignableReferences is every assignee the interface offers for reassignment:
// each roster member and each group any of them belongs to.
//
// It is built from the client's own roster because only the client knows what a
// person or a group is. FlowCore would accept any string at all.
func AssignableReferences() []string {
	seen := make(map[string]bool)

	var references []string
	for _, identity := range Roster {
		for _, reference := range identity.WorklistReferences() {
			if !seen[reference] {
				seen[reference] = true
				references = append(references, reference)
			}
		}
	}

	// Agents belong in this list for the same reason they belong in a worklist:
	// nothing distinguishes them from a person here. Moving a step to an agent is
	// how a human hands work back to one.
	for _, definition := range seededDefinitions() {
		for _, step := range definition.Steps {
			if !seen[step.AssigneeID] {
				seen[step.AssigneeID] = true
				references = append(references, step.AssigneeID)
			}
		}
	}

	return references
}
