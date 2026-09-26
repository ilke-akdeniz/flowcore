package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/mike-akdeniz/flowcore/client/internal/store"
)

// QueueItem is one piece of work waiting on somebody.
//
// Two kinds of thing appear in one list, because "what should I do next" does not
// sort by where the work came from:
//
//   - a **draft**, which nobody has submitted yet, and
//   - an **open step**, which FlowCore is holding for whoever it is assigned to.
//
// Only the second exists in the library. A drafted submission is work the console
// knows about and FlowCore has never heard of, which is why the queue is assembled
// here rather than being a projection of the worklist.
type QueueItem struct {
	Reference    string
	Type         store.SubmissionType
	SubmissionID uuid.UUID
	// StepName is the workflow step waiting, or empty for a draft.
	StepName string
	Assignee string
	// VisitID is what a decision acts on. Nil for a draft.
	VisitID      *uuid.UUID
	WaitingSince time.Time
	IsDraft      bool
}

// Queue is what is waiting on this person: their drafts, and the open steps
// assigned to them or to a group they belong to.
func (a *App) Queue(ctx context.Context, sessionID string, identity Identity) ([]QueueItem, error) {
	submissions, err := a.Store.Submissions(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	items := make([]QueueItem, 0, len(submissions))

	// Drafts sit with intake, whose job is taking details and submitting them.
	if identity.CanActAs("group:intake") {
		for _, submission := range submissions {
			if !submission.IsDraft() {
				continue
			}

			items = append(items, QueueItem{
				Reference:    submission.Reference,
				Type:         submission.Type,
				SubmissionID: submission.ID,
				Assignee:     "group:intake",
				WaitingSince: submission.CreatedAt,
				IsDraft:      true,
			})
		}
	}

	assigned, err := a.Worklist(ctx, sessionID, identity)
	if err != nil {
		return nil, err
	}

	byReference := make(map[string]store.Submission, len(submissions))
	for _, submission := range submissions {
		if submission.SubjectReference != nil {
			byReference[*submission.SubjectReference] = submission
		}
	}

	for _, step := range assigned {
		submission, ok := byReference[step.SubjectReference]
		if !ok {
			continue
		}

		visitID := step.VisitID
		items = append(items, QueueItem{
			Reference:    submission.Reference,
			Type:         submission.Type,
			SubmissionID: submission.ID,
			StepName:     step.StepName,
			Assignee:     step.AssigneeID,
			VisitID:      &visitID,
			WaitingSince: step.EnteredAt,
		})
	}

	return items, nil
}
