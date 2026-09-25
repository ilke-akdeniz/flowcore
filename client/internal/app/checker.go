package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/mike-akdeniz/flowcore"
)

// CheckRequest is what an agent step needs in order to decide.
//
// Note what has to be assembled here. FlowCore supplies the step and its actions;
// the release comes from this client's own store, because the library holds only
// an opaque reference to it. Neither half is enough alone, which is the shape of
// every agent integration: the engine knows where the work is, the client knows
// what the work is about.
type CheckRequest struct {
	Agent    string
	StepName string
	Release  Release
	Actions  []flowcore.Action
}

// Verdict is an agent's answer: which action to take, and why.
//
// The remark is the point as much as the action. It gets stamped on the visit in
// the same transaction as the decision, so the finding and the decision it caused
// cannot be separated by a crash between two writes.
type Verdict struct {
	ActionID uuid.UUID
	Remark   string
}

// Checker decides an agent step.
//
// The interface exists so the client can run with or without an API key. Both
// implementations produce the same shape, take the same path through the
// dispatcher, and stamp the same kind of record — only the source of the judgment
// differs.
type Checker interface {
	Check(ctx context.Context, request CheckRequest) (Verdict, error)
	// Mode names the implementation for the interface, so a visitor can see
	// whether they are watching a real model or a script.
	Mode() string
}

// actionNamed resolves an action name the checker chose to the id CompleteStep
// needs, and rejects anything the step does not offer.
//
// This validation is the client's job and cannot be skipped. A model asked to
// pick from a list will occasionally return something else, and FlowCore would
// reject an action belonging to another step anyway — the schema enforces the
// pairing — but failing here gives a legible error instead of a foreign-key one.
func actionNamed(actions []flowcore.Action, name string) (uuid.UUID, error) {
	for _, action := range actions {
		if strings.EqualFold(action.Name, strings.TrimSpace(name)) {
			return action.ID, nil
		}
	}

	available := make([]string, 0, len(actions))
	for _, action := range actions {
		available = append(available, action.Name)
	}

	return uuid.Nil, fmt.Errorf(
		"checker chose %q, which is not an action of this step (have: %s)",
		name, strings.Join(available, ", "))
}
