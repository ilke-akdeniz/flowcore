package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/mike-akdeniz/flowcore"
)

// releaseApprovalName is what the seeded definition is called. Definition names
// are not unique in FlowCore — deliberately, since a multi-tenant client may want
// the same name per customer — so every session gets its own definition with this
// same name and they do not collide.
const releaseApprovalName = "Release Approval"

// EnsureSeeded gives a session its starting content the first time it is seen.
//
// Seeding happens per session on first request, never at startup. That is what
// lets local and hosted use run the same code with no mode flag: alone on your
// laptop you are simply the only session, and the isolation costs nothing.
func (a *App) EnsureSeeded(ctx context.Context, session *Session) error {
	if len(session.DefinitionIDs) > 0 {
		return nil
	}

	definition, err := a.Catalog.Create(ctx, releaseApprovalDefinition())
	if err != nil {
		return fmt.Errorf("seed release approval: %w", err)
	}

	session.DefinitionIDs = append(session.DefinitionIDs, definition.ID)

	release := Release{
		Version:   "v2.4.0",
		Commit:    "a3f91c2",
		Title:     "Add rate limiting to the public API",
		Changelog: "Adds per-key rate limiting. No breaking changes.",
		DiffStat:  "14 files changed, 512 insertions(+), 38 deletions(-)",
	}
	session.Releases[release.Version] = release

	// The subject lives in the line above. FlowCore gets the reference and the
	// version token, and nothing else — it never learns what a release is.
	_, err = a.Engine.Start(ctx, flowcore.StartParams{
		WorkflowDefinitionID: definition.ID,
		SubjectReference:     session.SubjectReference(release.Version),
		SubjectVersionToken:  &release.Commit,
	})
	if err != nil {
		return fmt.Errorf("start seeded run: %w", err)
	}

	return nil
}

// releaseApprovalDefinition builds the release workflow: two agent steps, four
// human groups, an escalation path to legal, and a loop back to the author.
//
// Ids are generated up front because actions route to steps by id, so the graph
// has to be wired before it is written. Assignee strings are opaque to FlowCore —
// "agent:diff-risk@v1" and "group:security" are meaningful only to this client,
// which is what makes an agent just another actor rather than a special case.
func releaseApprovalDefinition() flowcore.WorkflowDefinition {
	var (
		inReview = uuid.Must(uuid.NewV7())
		approved = uuid.Must(uuid.NewV7())
		rejected = uuid.Must(uuid.NewV7())

		riskAnalysis   = uuid.Must(uuid.NewV7())
		changelogCheck = uuid.Must(uuid.NewV7())
		securityReview = uuid.Must(uuid.NewV7())
		qaSignOff      = uuid.Must(uuid.NewV7())
		legalReview    = uuid.Must(uuid.NewV7())
		authorRevision = uuid.Must(uuid.NewV7())
	)

	return flowcore.WorkflowDefinition{
		Name:                    releaseApprovalName,
		InitialStepDefinitionID: &riskAnalysis,
		Statuses: []flowcore.WorkflowStatusDefinition{
			{ID: inReview, Name: "in review"},
			{ID: approved, Name: "approved"},
			{ID: rejected, Name: "rejected"},
		},
		Steps: []flowcore.StepDefinition{
			{
				ID:                         riskAnalysis,
				WorkflowStatusDefinitionID: inReview,
				Name:                       "automated risk analysis",
				AssigneeID:                 "agent:diff-risk@v1",
				Actions: []flowcore.ActionDefinition{
					{Name: "low risk", NextStepDefinitionID: &changelogCheck},
					{Name: "high risk", NextStepDefinitionID: &securityReview},
				},
			},
			{
				ID:                         changelogCheck,
				WorkflowStatusDefinitionID: inReview,
				Name:                       "changelog check",
				AssigneeID:                 "agent:changelog@v1",
				Actions: []flowcore.ActionDefinition{
					{Name: "accurate", NextStepDefinitionID: &qaSignOff},
					{Name: "mismatch", NextStepDefinitionID: &authorRevision},
				},
			},
			{
				ID:                         securityReview,
				WorkflowStatusDefinitionID: inReview,
				Name:                       "security review",
				AssigneeID:                 "group:security",
				Actions: []flowcore.ActionDefinition{
					{Name: "clear", NextStepDefinitionID: &qaSignOff},
					{Name: "needs legal", NextStepDefinitionID: &legalReview},
					{Name: "reject", TerminalWorkflowStatusDefinitionID: &rejected},
				},
			},
			{
				ID:                         legalReview,
				WorkflowStatusDefinitionID: inReview,
				Name:                       "legal review",
				AssigneeID:                 "group:legal",
				Actions: []flowcore.ActionDefinition{
					{Name: "clear", NextStepDefinitionID: &qaSignOff},
					{Name: "reject", TerminalWorkflowStatusDefinitionID: &rejected},
				},
			},
			{
				ID:                         qaSignOff,
				WorkflowStatusDefinitionID: inReview,
				Name:                       "QA sign-off",
				AssigneeID:                 "group:qa",
				Actions: []flowcore.ActionDefinition{
					{Name: "pass", TerminalWorkflowStatusDefinitionID: &approved},
					{Name: "fail", NextStepDefinitionID: &authorRevision},
				},
			},
			{
				// The loop: a rejected release goes back to its author, who pushes
				// a new commit and resubmits, and the run re-enters the agent steps
				// with a new subject version token.
				ID:                         authorRevision,
				WorkflowStatusDefinitionID: inReview,
				Name:                       "author revision",
				AssigneeID:                 "user:submitter",
				Actions: []flowcore.ActionDefinition{
					{Name: "resubmit", NextStepDefinitionID: &riskAnalysis},
				},
			},
		},
	}
}
