package app

import (
	"context"
	"fmt"
)

// CannedChecker answers from a script rather than a model.
//
// It exists so that someone who clones this repository and runs it with nothing
// configured sees the whole application work in thirty seconds. Every other part
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
		"v2.4.0": {
			action: "low risk",
			remark: "No finding. 512 insertions across 14 files, all additive: a new " +
				"middleware package and its tests. No change to authentication, session " +
				"handling, or data migration paths.",
		},
		"v2.5.0-rc1": {
			action: "high risk",
			remark: "2 findings. Replaces the session store, which is an authentication " +
				"path, and the change is not additive — existing sessions are invalidated " +
				"on deploy. Routing to security review.",
		},
	},
	"agent:changelog@v1": {
		"v2.4.0": {
			action: "accurate",
			remark: "The changelog describes per-key rate limiting and claims no breaking " +
				"changes. Both hold: the diff adds middleware and touches no existing " +
				"handler signature.",
		},
		"v2.5.0-rc1": {
			action: "mismatch",
			remark: "1 finding. The changelog says \"internal refactor only\", but the diff " +
				"invalidates existing sessions on deploy, which users will experience as " +
				"being logged out. That belongs in the changelog.",
		},
	},
}

func (c CannedChecker) Check(_ context.Context, request CheckRequest) (Verdict, error) {
	byRelease, ok := cannedVerdicts[request.Agent]
	if !ok {
		return Verdict{}, fmt.Errorf("no canned verdicts for %s", request.Agent)
	}

	scripted, ok := byRelease[request.Release.Version]
	if !ok {
		return Verdict{}, fmt.Errorf("no canned verdict for %s on %s", request.Agent, request.Release.Version)
	}

	actionID, err := actionNamed(request.Actions, scripted.action)
	if err != nil {
		return Verdict{}, err
	}

	return Verdict{ActionID: actionID, Remark: scripted.remark}, nil
}
