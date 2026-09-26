package app

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mike-akdeniz/flowcore"
	"github.com/mike-akdeniz/flowcore/client/internal/store"
)

// SeedSession gives a visitor their own copy of the example data.
//
// Per-session copies exist because hosting means concurrent visitors: without
// them, two people signing in as Dana would work the same claim and one would
// settle it while the other was reading. The console owns tenancy because
// FlowCore has none, which is the same arrangement the library's `CLAUDE.md`
// describes when it names `tenant_id` as structure with no caller.
//
// Each example is a workflow, one drafted submission, and **no run**. The
// visitor's first action is submitting the draft, so they watch the workflow
// begin rather than arriving part-way through one.
func (a *App) SeedSession(ctx context.Context, sessionID string) error {
	if err := a.seedClaimExample(ctx, sessionID); err != nil {
		return err
	}

	return a.seedApplicationExample(ctx, sessionID)
}

func (a *App) seedClaimExample(ctx context.Context, sessionID string) error {
	definition, err := a.Catalog.Create(ctx, claimAssessmentDefinition())
	if err != nil {
		return fmt.Errorf("seed claim workflow: %w", err)
	}

	if err := a.register(ctx, sessionID, store.TypeClaim, definition); err != nil {
		return err
	}

	submissionID := uuid.Must(uuid.NewV7())

	// A draft: no run, no workflow stamped, no subject reference. The schema
	// enforces that those three are absent together.
	if err := a.Store.InsertSubmission(ctx, store.Submission{
		ID:        submissionID,
		SessionID: sessionID,
		Type:      store.TypeClaim,
		Reference: "C-1042",
		Status:    "draft",
		CreatedAt: time.Now(),
	}); err != nil {
		return err
	}

	if err := a.Store.InsertClaimDetail(ctx, store.ClaimDetail{
		SubmissionID: submissionID,
		PolicyNumber: "MP-90114",
		ClaimantName: "Rosa Lindqvist",
		Amount:       "11200.00",
		OccurredAt:   date(2026, 9, 14),
		IncidentNarrative: "The car was hit while parked overnight outside my house on the 14th. " +
			"I found the damage when I came out at 07:00 and reported it straight away.",
	}); err != nil {
		return err
	}

	// The police report contradicts the account above on both the circumstances
	// and the timing. That contradiction is what the narrative-consistency agent
	// is for, and it is the reason this claim is the seeded one.
	documents := []store.Document{
		{
			Name: "Repair estimate", Kind: "estimate", ReceivedAt: date(2026, 9, 16),
			Body: text("Front nearside wing and door, replace and respray. Headlamp unit. " +
				"Parts £6,940, labour £3,180, paint £1,080. Total £11,200."),
		},
		{
			Name: "Police report", Kind: "police_report", ReceivedAt: date(2026, 9, 16),
			Body: text("Report filed 16th at 11:40. Caller stated the vehicle was damaged in a " +
				"collision while being driven on the evening of the 14th. No third party " +
				"identified. No injuries reported."),
		},
		{Name: "Damage photographs", Kind: "photograph", ReceivedAt: date(2026, 9, 15)},
	}

	for _, document := range documents {
		document.ID = uuid.Must(uuid.NewV7())
		document.SubmissionID = submissionID

		if err := a.Store.InsertDocument(ctx, document); err != nil {
			return err
		}
	}

	return nil
}

func (a *App) seedApplicationExample(ctx context.Context, sessionID string) error {
	definition, err := a.Catalog.Create(ctx, underwritingDefinition())
	if err != nil {
		return fmt.Errorf("seed underwriting workflow: %w", err)
	}

	if err := a.register(ctx, sessionID, store.TypeApplication, definition); err != nil {
		return err
	}

	submissionID := uuid.Must(uuid.NewV7())

	if err := a.Store.InsertSubmission(ctx, store.Submission{
		ID:        submissionID,
		SessionID: sessionID,
		Type:      store.TypeApplication,
		Reference: "P-2087",
		Status:    "draft",
		CreatedAt: time.Now(),
	}); err != nil {
		return err
	}

	return a.Store.InsertApplicationDetail(ctx, store.ApplicationDetail{
		SubmissionID: submissionID,
		ProposerName: "Halvard Aune",
		CoverType:    "Comprehensive motor",
		SumInsured:   "48000.00",
		Disclosures: "Two speeding convictions in the last three years, most recently March. " +
			"Vehicle is kept on the street. Business use one day a week. " +
			"A previous insurer declined cover in 2023.",
	})
}

// register records which FlowCore definition serves which kind of submission.
// FlowCore takes a definition id and starts a run; which definition a claim
// should use is a question only the console can answer.
func (a *App) register(
	ctx context.Context,
	sessionID string,
	submissionType store.SubmissionType,
	definition flowcore.WorkflowDefinition,
) error {
	return a.Store.RegisterWorkflow(ctx, store.RegisteredWorkflow{
		ID:                   uuid.Must(uuid.NewV7()),
		SessionID:            sessionID,
		SubmissionType:       submissionType,
		Name:                 definition.Name,
		FlowcoreDefinitionID: definition.ID,
		Active:               true,
		CreatedAt:            time.Now(),
	})
}

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func text(value string) *string { return &value }
