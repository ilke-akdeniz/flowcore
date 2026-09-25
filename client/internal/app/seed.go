package app

import (
	"context"
	"fmt"

	"github.com/mike-akdeniz/flowcore"
)

// EnsureSeeded gives a session its starting content the first time it is seen.
//
// Seeding happens per session on first request, never at startup. That is what
// lets local and hosted use run the same code with no mode flag: alone on your
// laptop you are simply the only session, and the isolation costs nothing.
//
// Two scenarios, because one cannot show that the library does not care what a
// workflow is about. They share no code below the definition — different steps,
// different groups, different agents, different subjects — and the library treats
// them identically.
func (a *App) EnsureSeeded(ctx context.Context, session *Session) error {
	if len(session.DefinitionIDs) > 0 {
		return nil
	}

	if err := a.seed(ctx, session, releaseApprovalDefinition(), seededReleases()); err != nil {
		return err
	}

	return a.seed(ctx, session, claimAssessmentDefinition(), seededClaims())
}

// seed creates one definition and starts a run of it for each subject.
func (a *App) seed(ctx context.Context, session *Session, tree flowcore.WorkflowDefinition, subjects []Subject) error {
	definition, err := a.Catalog.Create(ctx, tree)
	if err != nil {
		return fmt.Errorf("seed %s: %w", tree.Name, err)
	}

	session.DefinitionIDs = append(session.DefinitionIDs, definition.ID)

	for _, subject := range subjects {
		session.Runs[subject.Reference()] = Run{Subject: subject, DefinitionID: definition.ID}

		token := subject.VersionToken()

		// The subject itself stayed in the line above. FlowCore gets the reference
		// and the version token, and never learns what either one means.
		state, err := a.Engine.Start(ctx, flowcore.StartParams{
			WorkflowDefinitionID: definition.ID,
			SubjectReference:     session.SubjectReference(subject),
			SubjectVersionToken:  &token,
		})
		if err != nil {
			return fmt.Errorf("start run for %s: %w", subject.Reference(), err)
		}

		// The run now sits on an agent step with nobody attending it. Handing the
		// state to the dispatcher is case-4 dispatch in one line: whoever advanced
		// the run already knows whether an agent owns what comes next.
		a.Dispatcher.Dispatch(session, definition.ID, state)
	}

	return nil
}

// seededReleases branch on the first agent's verdict, so one of each is needed to
// show both paths: the low-risk one runs both agent steps and lands on QA, the
// high-risk one escalates to security review where a person must decide.
func seededReleases() []Subject {
	return []Subject{
		Release{
			Version:   "v2.4.0",
			Commit:    "a3f91c2",
			TitleText: "Add rate limiting to the public API",
			Changelog: "Adds per-key rate limiting. No breaking changes.",
			DiffStat:  "14 files changed, 512 insertions(+), 38 deletions(-)",
		},
		Release{
			Version:   "v2.5.0-rc1",
			Commit:    "7d10b84",
			TitleText: "Migrate session storage to Redis",
			Changelog: "Internal refactor only.",
			DiffStat:  "9 files changed, 214 insertions(+), 186 deletions(-)",
		},
	}
}

// seededClaims do the same for the claim workflow: one that reads cleanly, and
// one whose account does not match the police report.
func seededClaims() []Subject {
	return []Subject{
		Claim{
			Number:   "C-1042",
			Revision: "r1",
			Policy:   "MP-88213",
			Amount:   "£3,480",
			Incident: "I was stationary at the lights on Mill Road when a van went into " +
				"the back of me at about 08:15 on the 3rd. The other driver admitted " +
				"fault at the scene and we exchanged details.",
			PoliceReport: "Attended Mill Road 08:31 on the 3rd following a report of a " +
				"rear-end collision. Two vehicles, no injuries. Driver of vehicle 2 " +
				"accepted responsibility at the scene.",
			Documents: []string{"repair estimate", "photographs", "exchange of details"},
		},
		Claim{
			Number:   "C-1043",
			Revision: "r1",
			Policy:   "MP-90114",
			Amount:   "£11,200",
			Incident: "The car was hit while parked overnight outside my house on the " +
				"14th. I found the damage when I came out at 07:00 and reported it " +
				"straight away.",
			PoliceReport: "Report filed 16th at 11:40. Caller stated the vehicle was " +
				"damaged in a collision while being driven on the evening of the 14th. " +
				"No third party identified.",
			Documents: []string{"repair estimate", "photographs", "police reference"},
		},
	}
}
