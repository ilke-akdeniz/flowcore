package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/mike-akdeniz/flowcore/client/internal/store"
)

// SubjectText renders a submission as the prose an agent step reads.
//
// This is the console's whole contribution to an AI step. FlowCore holds an
// opaque reference — "s7f3a2:claim:C-1042" — and nothing else, so the material a
// model needs in order to judge anything has to be assembled here, from the
// console's own tables.
//
// It is also why the library cannot call a model itself, and not merely why it
// chooses not to: it does not have this text and could not obtain it without
// either storing subjects or calling back into its caller.
func (a *App) SubjectText(ctx context.Context, sessionID, reference string) (string, error) {
	kind, _, found := strings.Cut(reference, ":")
	if !found {
		return "", fmt.Errorf("malformed subject reference %q", reference)
	}

	submission, err := a.Store.SubmissionByReference(ctx, sessionID, referenceOf(reference))
	if err != nil {
		return "", err
	}

	switch store.SubmissionType(kind) {
	case store.TypeClaim:
		return a.claimText(ctx, submission)
	case store.TypeApplication:
		return a.applicationText(ctx, submission)
	}

	return "", fmt.Errorf("unknown submission kind %q", kind)
}

func (a *App) claimText(ctx context.Context, submission store.Submission) (string, error) {
	detail, err := a.Store.ClaimDetail(ctx, submission.ID)
	if err != nil {
		return "", err
	}

	documents, err := a.Store.Documents(ctx, submission.ID)
	if err != nil {
		return "", err
	}

	var text strings.Builder
	fmt.Fprintf(&text, "Claim: %s\nPolicy: %s\nClaimant: %s\nAmount: %s\nDate of incident: %s\n\n",
		submission.Reference, detail.PolicyNumber, detail.ClaimantName,
		detail.Amount, detail.OccurredAt.Format("2 January 2006"))

	fmt.Fprintf(&text, "Claimant's account:\n%s\n\n", detail.IncidentNarrative)

	text.WriteString("Documents on file:\n")
	for _, document := range documents {
		fmt.Fprintf(&text, "- %s (%s, received %s)\n",
			document.Name, document.Kind, document.ReceivedAt.Format("2 January 2006"))

		// A photograph is a row with no body. The ones that carry text are what
		// the narrative-consistency step actually compares against the account
		// above — the text is the document.
		if document.Body != nil {
			fmt.Fprintf(&text, "  %s\n", *document.Body)
		}
	}

	return text.String(), nil
}

func (a *App) applicationText(ctx context.Context, submission store.Submission) (string, error) {
	detail, err := a.Store.ApplicationDetail(ctx, submission.ID)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(
		"Application: %s\nProposer: %s\nCover: %s\nSum insured: %s\n\nDisclosures:\n%s",
		submission.Reference, detail.ProposerName, detail.CoverType,
		detail.SumInsured, detail.Disclosures), nil
}

// referenceOf strips the kind, leaving the console's own reference:
// "claim:C-1042" becomes "C-1042".
func referenceOf(reference string) string {
	_, rest, found := strings.Cut(reference, ":")
	if !found {
		return reference
	}

	return rest
}

// SubjectReference is what FlowCore records for a submission: the session, the
// kind, and the reference. Opaque to the library, and prefixed with the session
// so two visitors working the same seeded claim have two separate runs.
func SubjectReference(sessionID string, submission store.Submission) string {
	return fmt.Sprintf("%s:%s:%s", sessionID, submission.Type, submission.Reference)
}
