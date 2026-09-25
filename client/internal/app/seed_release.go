package app

import (
	"github.com/google/uuid"
	"github.com/mike-akdeniz/flowcore"
)

const releaseApprovalName = "Release Approval"

// releaseApprovalDefinition is the first scenario: two agent steps, four human
// groups, an escalation to legal, and a loop back to the author.
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
				// The loop: a rejected release goes back to its author, who pushes a
				// new commit and resubmits, and the run re-enters the agent steps with
				// a new subject version token.
				ID:                         authorRevision,
				WorkflowStatusDefinitionID: inReview,
				Name:                       "author revision",
				AssigneeID:                 "user:alex",
				Actions: []flowcore.ActionDefinition{
					{Name: "resubmit", NextStepDefinitionID: &riskAnalysis},
				},
			},
		},
	}
}

// seededDefinitions is every workflow this client seeds. Used where the client
// needs to know the assignees it will encounter — which agents to dispatch, and
// which references to offer for reassignment.
func seededDefinitions() []flowcore.WorkflowDefinition {
	return []flowcore.WorkflowDefinition{releaseApprovalDefinition(), claimAssessmentDefinition()}
}
