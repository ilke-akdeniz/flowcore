package app

import (
	"github.com/google/uuid"
	"github.com/mike-akdeniz/flowcore"
)

const claimAssessmentName = "Claim Assessment"

// claimAssessmentDefinition is the second scenario, and it exists to show the
// library carrying something that has nothing to do with the first.
//
// One scenario demonstrates a workflow. It cannot demonstrate that FlowCore is
// subject-agnostic, which is the first claim its README makes — and the workflow
// editor does not help, because the subject never appears in it. Only a second
// subject of a different kind shows that.
//
// Seven steps, two of them agents, four human groups, two terminal statuses, and
// a loop back to the claimant. The escalation paths are the point: a flagged
// narrative goes to the fraud unit rather than to an adjuster, and an adjuster can
// refer upward rather than only approving or denying.
func claimAssessmentDefinition() flowcore.WorkflowDefinition {
	var (
		inAssessment = uuid.Must(uuid.NewV7())
		settled      = uuid.Must(uuid.NewV7())
		denied       = uuid.Must(uuid.NewV7())

		completeness = uuid.Must(uuid.NewV7())
		consistency  = uuid.Must(uuid.NewV7())
		adjuster     = uuid.Must(uuid.NewV7())
		senior       = uuid.Must(uuid.NewV7())
		fraudUnit    = uuid.Must(uuid.NewV7())
		legal        = uuid.Must(uuid.NewV7())
		awaiting     = uuid.Must(uuid.NewV7())
	)

	return flowcore.WorkflowDefinition{
		Name:                    claimAssessmentName,
		InitialStepDefinitionID: &completeness,
		Statuses: []flowcore.WorkflowStatusDefinition{
			{ID: inAssessment, Name: "in assessment"},
			{ID: settled, Name: "settled"},
			{ID: denied, Name: "denied"},
		},
		Steps: []flowcore.StepDefinition{
			{
				ID:                         completeness,
				WorkflowStatusDefinitionID: inAssessment,
				Name:                       "completeness check",
				AssigneeID:                 "agent:intake@v1",
				Actions: []flowcore.ActionDefinition{
					{Name: "complete", NextStepDefinitionID: &consistency},
					{Name: "missing documents", NextStepDefinitionID: &awaiting},
				},
			},
			{
				ID:                         consistency,
				WorkflowStatusDefinitionID: inAssessment,
				Name:                       "narrative consistency",
				AssigneeID:                 "agent:fraud@v1",
				Actions: []flowcore.ActionDefinition{
					{Name: "consistent", NextStepDefinitionID: &adjuster},
					{Name: "inconsistent", NextStepDefinitionID: &fraudUnit},
				},
			},
			{
				ID:                         adjuster,
				WorkflowStatusDefinitionID: inAssessment,
				Name:                       "adjuster review",
				AssigneeID:                 "group:adjusters",
				Actions: []flowcore.ActionDefinition{
					{Name: "approve", TerminalWorkflowStatusDefinitionID: &settled},
					{Name: "refer upward", NextStepDefinitionID: &senior},
					{Name: "deny", TerminalWorkflowStatusDefinitionID: &denied},
				},
			},
			{
				ID:                         senior,
				WorkflowStatusDefinitionID: inAssessment,
				Name:                       "senior adjuster",
				AssigneeID:                 "group:senior-adjusters",
				Actions: []flowcore.ActionDefinition{
					{Name: "approve", TerminalWorkflowStatusDefinitionID: &settled},
					{Name: "refer to legal", NextStepDefinitionID: &legal},
					{Name: "deny", TerminalWorkflowStatusDefinitionID: &denied},
				},
			},
			{
				ID:                         fraudUnit,
				WorkflowStatusDefinitionID: inAssessment,
				Name:                       "fraud investigation",
				AssigneeID:                 "group:siu",
				Actions: []flowcore.ActionDefinition{
					{Name: "cleared", NextStepDefinitionID: &adjuster},
					{Name: "confirmed", TerminalWorkflowStatusDefinitionID: &denied},
				},
			},
			{
				ID:                         legal,
				WorkflowStatusDefinitionID: inAssessment,
				Name:                       "legal review",
				AssigneeID:                 "group:legal",
				Actions: []flowcore.ActionDefinition{
					{Name: "clear", TerminalWorkflowStatusDefinitionID: &settled},
					{Name: "deny", TerminalWorkflowStatusDefinitionID: &denied},
				},
			},
			{
				ID:                         awaiting,
				WorkflowStatusDefinitionID: inAssessment,
				Name:                       "awaiting documents",
				AssigneeID:                 "user:claimant",
				Actions: []flowcore.ActionDefinition{
					{Name: "resubmit", NextStepDefinitionID: &completeness},
				},
			},
		},
	}
}
