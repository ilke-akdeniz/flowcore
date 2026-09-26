package app

import (
	"github.com/google/uuid"
	"github.com/mike-akdeniz/flowcore"
)

// The two seeded workflows.
//
// Every step in the claim workflow demonstrates something structural that no
// other step does — that was the test the owner set, and decision 17 records the
// two steps it removed. Assignee strings are opaque to FlowCore: `agent:triage`
// and `group:adjusters` are meaningful only to this console, which is what makes
// an agent an ordinary actor rather than a special case.

func claimAssessmentDefinition() flowcore.WorkflowDefinition {
	var (
		inAssessment = uuid.Must(uuid.NewV7())
		settled      = uuid.Must(uuid.NewV7())
		declined     = uuid.Must(uuid.NewV7())

		triage        = uuid.Must(uuid.NewV7())
		fastTrack     = uuid.Must(uuid.NewV7())
		documentation = uuid.Must(uuid.NewV7())
		awaiting      = uuid.Must(uuid.NewV7())
		consistency   = uuid.Must(uuid.NewV7())
		adjuster      = uuid.Must(uuid.NewV7())
		fraud         = uuid.Must(uuid.NewV7())
	)

	return flowcore.WorkflowDefinition{
		Name:                    "Claim assessment",
		InitialStepDefinitionID: &triage,
		Statuses: []flowcore.WorkflowStatusDefinition{
			{ID: inAssessment, Name: "in assessment"},
			{ID: settled, Name: "settled"},
			{ID: declined, Name: "declined"},
		},
		Steps: []flowcore.StepDefinition{
			{
				// An AI entry step that branches. The first thing a visitor sees
				// happen after submitting is this deciding which path the claim takes.
				ID: triage, WorkflowStatusDefinitionID: inAssessment,
				Name: "triage", AssigneeID: "agent:triage",
				Actions: []flowcore.ActionDefinition{
					{Name: "fast track", NextStepDefinitionID: &fastTrack},
					{Name: "full assessment", NextStepDefinitionID: &documentation},
				},
			},
			{
				// One side of the branch, with a cross-over out of it.
				ID: fastTrack, WorkflowStatusDefinitionID: inAssessment,
				Name: "fast-track review", AssigneeID: "group:adjusters",
				Actions: []flowcore.ActionDefinition{
					{Name: "settle", TerminalWorkflowStatusDefinitionID: &settled},
					{Name: "escalate", NextStepDefinitionID: &documentation},
					{Name: "decline", TerminalWorkflowStatusDefinitionID: &declined},
				},
			},
			{
				// An AI step whose failure loops back for more input.
				ID: documentation, WorkflowStatusDefinitionID: inAssessment,
				Name: "documentation check", AssigneeID: "agent:intake",
				Actions: []flowcore.ActionDefinition{
					{Name: "complete", NextStepDefinitionID: &consistency},
					{Name: "incomplete", NextStepDefinitionID: &awaiting},
				},
			},
			{
				// The loop target: an AI step re-run against a genuinely different file.
				ID: awaiting, WorkflowStatusDefinitionID: inAssessment,
				Name: "awaiting documents", AssigneeID: "group:intake",
				Actions: []flowcore.ActionDefinition{
					{Name: "resubmit", NextStepDefinitionID: &documentation},
				},
			},
			{
				// An AI step that diverts to a specialist.
				ID: consistency, WorkflowStatusDefinitionID: inAssessment,
				Name: "narrative consistency", AssigneeID: "agent:fraud",
				Actions: []flowcore.ActionDefinition{
					{Name: "consistent", NextStepDefinitionID: &adjuster},
					{Name: "inconsistent", NextStepDefinitionID: &fraud},
				},
			},
			{
				// The main human decision, with the cross-over back.
				ID: adjuster, WorkflowStatusDefinitionID: inAssessment,
				Name: "adjuster review", AssigneeID: "group:adjusters",
				Actions: []flowcore.ActionDefinition{
					{Name: "settle", TerminalWorkflowStatusDefinitionID: &settled},
					{Name: "downgrade", NextStepDefinitionID: &fastTrack},
					{Name: "decline", TerminalWorkflowStatusDefinitionID: &declined},
				},
			},
			{
				// A side branch that rejoins the main path or ends the run.
				ID: fraud, WorkflowStatusDefinitionID: inAssessment,
				Name: "fraud referral", AssigneeID: "group:siu",
				Actions: []flowcore.ActionDefinition{
					{Name: "cleared", NextStepDefinitionID: &adjuster},
					{Name: "confirmed", TerminalWorkflowStatusDefinitionID: &declined},
				},
			},
		},
	}
}

// underwritingDefinition is the simple one: an AI entry step and two approvers.
// Its job is to be short, and a plain referral is worth demonstrating once.
func underwritingDefinition() flowcore.WorkflowDefinition {
	var (
		inUnderwriting = uuid.Must(uuid.NewV7())
		accepted       = uuid.Must(uuid.NewV7())
		declined       = uuid.Must(uuid.NewV7())

		riskScreen = uuid.Must(uuid.NewV7())
		underwrite = uuid.Must(uuid.NewV7())
		senior     = uuid.Must(uuid.NewV7())
	)

	return flowcore.WorkflowDefinition{
		Name:                    "New business underwriting",
		InitialStepDefinitionID: &riskScreen,
		Statuses: []flowcore.WorkflowStatusDefinition{
			{ID: inUnderwriting, Name: "in underwriting"},
			{ID: accepted, Name: "accepted"},
			{ID: declined, Name: "declined"},
		},
		Steps: []flowcore.StepDefinition{
			{
				ID: riskScreen, WorkflowStatusDefinitionID: inUnderwriting,
				Name: "risk screen", AssigneeID: "agent:risk",
				Actions: []flowcore.ActionDefinition{
					{Name: "standard", NextStepDefinitionID: &underwrite},
					{Name: "refer", NextStepDefinitionID: &senior},
				},
			},
			{
				ID: underwrite, WorkflowStatusDefinitionID: inUnderwriting,
				Name: "underwriter review", AssigneeID: "group:underwriters",
				Actions: []flowcore.ActionDefinition{
					{Name: "accept", TerminalWorkflowStatusDefinitionID: &accepted},
					{Name: "refer up", NextStepDefinitionID: &senior},
					{Name: "decline", TerminalWorkflowStatusDefinitionID: &declined},
				},
			},
			{
				ID: senior, WorkflowStatusDefinitionID: inUnderwriting,
				Name: "senior underwriter", AssigneeID: "group:senior-uw",
				Actions: []flowcore.ActionDefinition{
					{Name: "accept", TerminalWorkflowStatusDefinitionID: &accepted},
					{Name: "decline", TerminalWorkflowStatusDefinitionID: &declined},
				},
			},
		},
	}
}

// seededDefinitions is every workflow the console seeds, used where it needs to
// know the assignees it will meet — which agents to dispatch, and which
// references to offer.
func seededDefinitions() []flowcore.WorkflowDefinition {
	return []flowcore.WorkflowDefinition{claimAssessmentDefinition(), underwritingDefinition()}
}
