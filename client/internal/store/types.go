package store

import (
	"time"

	"github.com/google/uuid"
)

// Staff is one of the seeded cast. Shared across sessions and read-only: what
// belongs to a visitor is the work, not the people.
type Staff struct {
	Reference string
	Name      string
	Title     string
	// Groups are the references this person also answers to. Expanding a person
	// into their groups is what the console does before asking FlowCore for a
	// worklist, because the library has no identity model to do it with.
	Groups []string
}

// SubmissionType is what arrived. It selects the workflow, the detail table, and
// which screen renders it.
type SubmissionType string

const (
	TypeClaim       SubmissionType = "claim"
	TypeApplication SubmissionType = "application"
)

// Submission is the queue's view of a piece of work, common to both types.
type Submission struct {
	ID          uuid.UUID
	SessionID   string
	Type        SubmissionType
	Reference   string
	Status      string // draft | submitted
	CreatedAt   time.Time
	SubmittedAt *time.Time
	// FlowcoreDefinitionID is the workflow this was submitted under, frozen at
	// submission. Nil while a draft. Never rewritten — a run keeps the workflow it
	// started with, and rewriting this is the one way the console could undermine
	// that.
	FlowcoreDefinitionID *uuid.UUID
	// SubjectReference is what FlowCore was given: opaque to the library, and
	// prefixed with the session so two visitors working the same seeded claim have
	// two separate runs.
	SubjectReference *string
}

func (s Submission) IsDraft() bool { return s.Status == "draft" }

// ClaimDetail is everything the claim screens show and the claim agents read.
type ClaimDetail struct {
	SubmissionID      uuid.UUID
	PolicyNumber      string
	ClaimantName      string
	Amount            string
	OccurredAt        time.Time
	IncidentNarrative string
}

// ApplicationDetail is the same for a new policy application.
type ApplicationDetail struct {
	SubmissionID uuid.UUID
	ProposerName string
	CoverType    string
	SumInsured   string
	Disclosures  string
}

// Document is a record carrying text, not a file. The documents that matter are
// prose, and the text is what the agents read.
type Document struct {
	ID           uuid.UUID
	SubmissionID uuid.UUID
	Name         string
	Kind         string
	ReceivedAt   time.Time
	Body         *string
}

// RegisteredWorkflow associates a submission type with a FlowCore definition.
//
// The library takes a definition id and starts a run. Which definition a claim
// should use is a question it cannot answer, so the console answers it here.
type RegisteredWorkflow struct {
	ID                   uuid.UUID
	SessionID            string
	SubmissionType       SubmissionType
	Name                 string
	FlowcoreDefinitionID uuid.UUID
	Active               bool
	CreatedAt            time.Time
}
