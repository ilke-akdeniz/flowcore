package app

import (
	"context"
	"fmt"
)

// CannedChecker answers from a script rather than a model.
//
// It exists so that someone who clones this repository and runs it with nothing
// configured sees the whole application work in thirty seconds.
//
// A limitation worth knowing: a verdict is keyed by agent and subject, so it never
// changes. A run that loops back to an agent gets the same answer, which is fine
// for a demonstration and would be wrong for anything else. With a key set, the
// model sees the subject as it stands and can say something different the second
// time. Every other part
// of the path is real: the queue, the worker, the CompleteStep call, the remark
// on the visit. Only the judgment is pre-written.
//
// The findings below are authored against the seeded releases, so they will drift
// if the seed data changes without them. That is the standing cost of this
// approach, and it is accepted because the alternative — requiring an API key to
// see anything at all — is worse.
type CannedChecker struct{}

func (CannedChecker) Mode() string { return "canned" }

// cannedVerdict is a scripted answer: an action name and the finding that led to
// it, keyed by which agent is asking about which release.
type cannedVerdict struct {
	action string
	remark string
}

var cannedVerdicts = map[string]map[string]cannedVerdict{
	"agent:diff-risk@v1": {
		"release:v2.4.0": {
			action: "low risk",
			remark: "No finding. 512 insertions across 14 files, all additive: a new " +
				"middleware package and its tests. No change to authentication, session " +
				"handling, or data migration paths.",
		},
		"release:v2.5.0-rc1": {
			action: "high risk",
			remark: "2 findings. Replaces the session store, which is an authentication " +
				"path, and the change is not additive — existing sessions are invalidated " +
				"on deploy. Routing to security review.",
		},
	},
	"agent:changelog@v1": {
		"release:v2.4.0": {
			action: "accurate",
			remark: "The changelog describes per-key rate limiting and claims no breaking " +
				"changes. Both hold: the diff adds middleware and touches no existing " +
				"handler signature.",
		},
		"release:v2.5.0-rc1": {
			action: "mismatch",
			remark: "1 finding. The changelog says \"internal refactor only\", but the diff " +
				"invalidates existing sessions on deploy, which users will experience as " +
				"being logged out. That belongs in the changelog.",
		},
	},
	"agent:intake@v1": {
		"claim:C-1042": {
			action: "complete",
			remark: "All three supporting documents are on file: repair estimate, " +
				"photographs, and the exchange of details. Nothing outstanding.",
		},
		"claim:C-1043": {
			action: "complete",
			remark: "Estimate, photographs and the police reference are all on file. " +
				"Nothing outstanding on the paperwork.",
		},
	},
	"agent:fraud@v1": {
		"claim:C-1042": {
			action: "consistent",
			remark: "The account and the report agree. Claimant says 08:15 at Mill Road, " +
				"rear-ended while stationary; police attended Mill Road at 08:31 for a " +
				"rear-end collision with the second driver accepting responsibility.",
		},
		"claim:C-1043": {
			action: "inconsistent",
			remark: "2 findings. The claimant says the car was parked overnight and " +
				"unattended; the police report records it as damaged while being driven " +
				"on the evening of the 14th. The report was also filed two days after " +
				"the claimant says they discovered the damage and reported it straight " +
				"away. Referring to the fraud unit.",
		},
	},
}

func (c CannedChecker) Check(_ context.Context, request CheckRequest) (Verdict, error) {
	bySubject, ok := cannedVerdicts[request.Agent]
	if !ok {
		return Verdict{}, fmt.Errorf("no canned verdicts for %s", request.Agent)
	}

	scripted, ok := bySubject[request.Reference]
	if !ok {
		return Verdict{}, fmt.Errorf("no canned verdict for %s on %s", request.Agent, request.Reference)
	}

	actionID, err := actionNamed(request.Actions, scripted.action)
	if err != nil {
		return Verdict{}, err
	}

	return Verdict{ActionID: actionID, Remark: scripted.remark}, nil
}
