package app

import (
	"fmt"

	"github.com/google/uuid"
)

// Subject is the thing a workflow is about.
//
// The interface exists because FlowCore does not have one. The library stores a
// reference string and a version token and nothing else, so it is equally happy
// routing a software release or an insurance claim — and a client that only ever
// handled one kind would demonstrate the opposite of that.
//
// Everything below the Reference is invisible to the library. This is the whole
// of "not a document store": the subject lives here, the workflow lives there,
// and the only thing crossing between them is an opaque string.
type Subject interface {
	// Reference is what identifies this subject to the application, before the
	// session prefix is added. "release:v2.4.0", "claim:C-1042".
	Reference() string
	// VersionToken is the revision a decision would be made against. FlowCore
	// records it and never compares it; noticing that a subject moved on is the
	// client's job.
	VersionToken() string
	// Kind selects which template renders it and which agent instructions apply.
	Kind() string
	// Title is a one-line label for lists.
	Title() string
	// Describe is what an agent step is shown. It is prose because the judgment
	// being asked for is about prose.
	Describe() string
}

// Run pairs a subject with the definition its workflow started from.
//
// Both halves are needed to ask FlowCore anything: GetState is keyed by
// {subject reference, definition id}, because one subject may have runs of
// several different definitions open at once.
type Run struct {
	Subject      Subject
	DefinitionID uuid.UUID
}

// Release is a software release awaiting approval.
type Release struct {
	Version   string
	Commit    string
	TitleText string
	Changelog string
	DiffStat  string
}

func (r Release) Reference() string    { return "release:" + r.Version }
func (r Release) VersionToken() string { return r.Commit }
func (r Release) Kind() string         { return "release" }
func (r Release) Title() string        { return r.Version + " — " + r.TitleText }

func (r Release) Describe() string {
	return fmt.Sprintf(
		"Version: %s\nCommit: %s\nTitle: %s\nChangelog: %s\nDiff: %s",
		r.Version, r.Commit, r.TitleText, r.Changelog, r.DiffStat)
}

// Claim is a motor insurance claim awaiting assessment.
//
// Text only, deliberately. Photographs would be the obvious thing to add and they
// would cost upload handling, storage, binary files in the repository, and vision
// calls — for a judgment that reads perfectly well as prose. A claimant's account
// contradicting the police report is exactly the kind of thing a model is for, and
// it needs no image at all.
type Claim struct {
	Number       string
	Revision     string
	Policy       string
	Amount       string
	Incident     string
	PoliceReport string
	Documents    []string
}

func (c Claim) Reference() string    { return "claim:" + c.Number }
func (c Claim) VersionToken() string { return c.Revision }
func (c Claim) Kind() string         { return "claim" }
func (c Claim) Title() string        { return c.Number + " — " + c.Amount }

func (c Claim) Describe() string {
	documents := "none"
	if len(c.Documents) > 0 {
		documents = ""
		for i, document := range c.Documents {
			if i > 0 {
				documents += ", "
			}

			documents += document
		}
	}

	return fmt.Sprintf(
		"Claim: %s\nPolicy: %s\nAmount: %s\nDocuments on file: %s\n\n"+
			"Claimant's account:\n%s\n\nPolice report:\n%s",
		c.Number, c.Policy, c.Amount, documents, c.Incident, c.PoliceReport)
}
