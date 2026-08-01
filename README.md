# FlowCore

A subject-agnostic workflow library for Go, with human-in-the-loop AI review steps.

FlowCore is a code library plus Postgres schema and migrations — not a service, API, or application.
Clients import it and call it directly.
The library records opaque references — subject, assignee, completer — and never interprets them; authorization and identity stay in the client.

## Status

Early development, iteration 1: configure a workflow, start one, read where it stands, complete a step.

## Requirements

Go 1.25.7+, Postgres 13+, and a database role with CREATE privilege — the first migration creates a dedicated `flowcore` schema.

The test suite runs against Postgres 13 and 17.

## Install

```
go get github.com/ilke-akdeniz/flowcore
```

## Apply the schema

FlowCore owns a dedicated `flowcore` Postgres schema and ships the migrations for it.
`Migrate` applies them, so nothing has to be installed alongside your binary:

```go
func main() {
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	if err := flowcore.Migrate(ctx, pool); err != nil {
		log.Fatalf("flowcore migrate: %v", err)
	}

	// ... then start serving
}
```

Call it from every instance.
It takes a Postgres advisory lock, so simultaneous start-ups serialize — one migrates while the rest wait, then they find nothing to do.
It is also idempotent, so running it on every boot costs a couple of queries once the schema is current.

If you would rather keep schema changes out of application start-up, put the same call in a small command of its own and run it as a deploy step — an init container, a release job, whatever your pipeline uses.

**Or use a tool.**
The migrations are plain SQL files in `migrations/`, so `psql -f` works, and so does the goose CLI:

```
goose -dir migrations -table public.flowcore_goose_db_version postgres "$DATABASE_URL" up
```

The `-table` flag is not optional.
FlowCore keeps its migration history in `public.flowcore_goose_db_version` so that it can never collide with your own migrations if you also use goose; omitting the flag puts FlowCore's history in goose's default table, alongside yours.

## Configure a workflow

A **definition** is a template: statuses, steps, and the actions that leave each step.
Every action either routes to another step or ends the run in a terminal status.

Ids are yours to supply, so an action can point at a step declared later in the same literal:

```go
catalog := flowcore.NewCatalog(pool)

inProgress := uuid.Must(uuid.NewV7())
approved := uuid.Must(uuid.NewV7())
rejected := uuid.Must(uuid.NewV7())
managerReview := uuid.Must(uuid.NewV7())
directorReview := uuid.Must(uuid.NewV7())
managers := "group:managers"

definition, err := catalog.Create(ctx, flowcore.WorkflowDefinition{
	Name:                    "Expense Approval",
	InitialStepDefinitionID: &managerReview,
	Statuses: []flowcore.WorkflowStatusDefinition{
		{ID: inProgress, Name: "in progress"},
		{ID: approved, Name: "approved"},
		{ID: rejected, Name: "rejected"},
	},
	Steps: []flowcore.StepDefinition{
		{
			ID:                         managerReview,
			Name:                       "manager review",
			WorkflowStatusDefinitionID: inProgress,
			AssigneeID:                 &managers, // opaque; never interpreted
			Actions: []flowcore.ActionDefinition{
				{Name: "approve", NextStepDefinitionID: &directorReview},
				{Name: "reject", TerminalWorkflowStatusDefinitionID: &rejected},
			},
		},
		{
			ID:                         directorReview,
			Name:                       "director review",
			WorkflowStatusDefinitionID: inProgress,
			Actions: []flowcore.ActionDefinition{
				{Name: "approve", TerminalWorkflowStatusDefinitionID: &approved},
			},
		},
	},
})
```

The whole tree is written in one transaction, so a reader never sees a half-built definition.
Individual pieces are edited afterwards through `AddStep`, `UpdateStep`, `DeleteAction`, and their siblings.

## Run one

```go
engine := flowcore.NewEngine(pool)

state, err := engine.Start(ctx, flowcore.StartParams{
	WorkflowDefinitionID: definition.ID,
	SubjectReference:     "expense-123", // opaque: whatever identifies your subject
	SubjectVersionToken:  &revision,     // optional: which revision this run began on
})

state.CurrentStep.Name    // "manager review"
state.CurrentStep.Actions // "approve", "reject" — present these to the user
```

Completing a step names the visit being acted on, plus the action chosen:

```go
state, err = engine.CompleteStep(ctx, flowcore.CompleteParams{
	VisitID:             state.CurrentStep.VisitID,
	ActionID:            approveActionID,
	CompletedBy:         "user:mike", // opaque; recorded, never checked
	SubjectVersionToken: &revision,
})
```

The run advances to the next step, or finishes:

```go
if state.CurrentStep == nil {
	// finished — state.WorkflowStatusName is the terminating action's status
}
```

Read where a run stands, or its full history, at any time:

```go
state, err := engine.GetState(ctx, "expense-123", definition.ID)
history, err := engine.GetHistory(ctx, "expense-123", definition.ID)
```

`GetHistory` returns one entry per _visit_, oldest first, each recording who acted, what they chose, and which revision of the subject they acted on.
A step reached twice by a loop appears twice, and a completed visit is never rewritten — so "which revision did they approve, and who were they" stays answerable for every decision in the run.

## Two rules worth knowing before building on it

**Opaque references.**
Subject, subject version token, assignee, and completer are recorded and compared for equality, never interpreted.
FlowCore will not tell you whether the person completing a step was allowed to: group membership and authorization need your identity model, which the library deliberately does not have.

**A definition is a template; a run is a snapshot.**
Starting a workflow copies the definition's whole graph.
Editing or deleting that definition afterwards does not reach runs already in flight, and every finished run stays answerable for how it reached its outcome.

## Errors

Errors are a small typed taxonomy.
Branch coarsely with `errors.Is`, or pull the detail out with `errors.As`:

```go
switch {
case errors.Is(err, flowcore.ErrActiveWorkflowExists):
	// a run is already open for this subject and definition
case errors.Is(err, flowcore.ErrVisitNotOpen):
	// someone else advanced the run; re-read the state
}

var duplicate *flowcore.DuplicateNameError
if errors.As(err, &duplicate) {
	log.Printf("%s named %q already exists", duplicate.Entity, duplicate.Name)
}
```

## Stack

Go, Postgres, pgx v5 used natively, hand-written SQL in a repository layer, plain SQL migrations.

## Design

The authoritative design lives in [`docs/system-design.md`](docs/system-design.md): boundary, flows, state, responsibilities, invariants, and trade-offs.
[`docs/decisions.md`](docs/decisions.md) is a running log of the design decisions behind it — each with the options weighed and the reasoning that settled it.
[`docs/code-map.md`](docs/code-map.md) shows how the pieces fit together in code.

## How this is built

Design decisions are worked out and recorded before code is written.
Each is stress-tested through its alternatives, settled with a human in the loop, and landed in the design doc first; implementation follows the doc.
`docs/decisions.md` is the trail.

One deliberate idiom deviation: identifiers spell out full domain words rather than Go's typical short local names, with a narrow receiver-like exception for a function's single dominant parameter. Reasons recorded in decision 18.

## Development

Tests run against a real Postgres.

```
docker compose up -d
make create-test-db
make test
```

## License

MIT — see [LICENSE](LICENSE).
