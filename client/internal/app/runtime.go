package app

import (
	"context"

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
	assigned, err := a.Engine.ListAssignedSteps(ctx, identity.WorklistReferences())
	if err != nil {
		return nil, err
	}

	mine := make([]flowcore.AssignedStep, 0, len(assigned))
	for _, step := range assigned {
		if session.Owns(step.WorkflowDefinitionID) {
			mine = append(mine, step)
		}
	}

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

	return a.Engine.CompleteStep(ctx, params)
}

// Reassign moves an open visit to another assignee.
//
// There is no unassign: FlowCore requires a value, and work with no assignee
// would match no worklist query, so releasing it that way would hide it rather
// than free it.
func (a *App) Reassign(ctx context.Context, visitID uuid.UUID, assignee string) (flowcore.WorkflowState, error) {
	return a.Engine.Reassign(ctx, visitID, assignee)
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

	return references
}
