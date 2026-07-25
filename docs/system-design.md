# Overview

The evolving system design for FlowCore, and the scope of each iteration.
This is the authoritative design document: boundary, flows, state, responsibilities, invariants, and trade-offs.
It changes as decisions land — decisions are recorded here first, then implemented.

The rationale behind individual decisions — the options weighed and rejected — lives in `docs/decisions.md`.
This document records what the system is; the decision log records why.

# Boundary

_What are we designing? What we are not designing?_

A workflow library, designed to support human-in-the-loop AI review steps (iteration 2).

This is a code library that can called by the clients via class methods.
It's not a web API, Microservice, application...
You can just add the library to your codebase, migrate the db, and use - extend it.
It would be straightforward for devs to use this library as a foundation for an API or Microservice if the need arises.

Library is "workflow subject" agnostic.
Workflow subject could be a logo design or expense form. or...
Library doesn't hold these subjects, it doesn't try to be a document store.

Library owns a Postgres schema and ships migrations for it.
Devs run those migrations into their own Postgres instance; the tables are the library's source of truth for configuration and workflow state.
The schema lives in a dedicated `flowcore` Postgres schema, and every query is schema-qualified in Go — the library never relies on the client's `search_path`.

Library is also identity-agnostic.
It records who a step is assigned to and who completed it, but never interprets those identifiers and never enforces permissions.
Group membership, roles and authorization policy live in the client system.

In short, the boundary is a code library and lightweight db offering AI enhanced workflow functionality.

# Actors

Library
Dev: Developer using the library
Caller: Client code that calls the library.
Client System: The system that interacts with the Library via Caller.

# Flows

_What are the main user/system journeys?_

This is an overview, a starting point to discover important structure and move forward.
We are not aiming for perfection or extensive discovery.

_A note on terminology:_ the configuration-side entities are **definitions** — a workflow definition is a template.
A **workflow** is a running instance started from a definition.
Definitions are edited through the `Catalog` component; instances are run by the `Engine`.

*Configure Workflow*
Dev creates a new workflow definition ("Expense Approval") and defines:
- workflow statuses > > "not started", "in progress", "approved", "rejected"...
- the workflow steps    >  manager review
- workflow status on each step
- the entry step the workflow begins on
- possible actions for each step, and for each action either the next step it leads to or the terminal status it ends the workflow in.

How does the dev perform the configuration?
 > Library has methods for configuration.
Dev is responsible for making the calls via any suitable method. (Dev can create a console app, management page - portal...)

*Start Basic Workflow*
Caller submits "subject id, subject version token, workflow definition id," to the library.
Library starts a new workflow for that subject at the definition's declared entry step, returns current step: {stepId, actions[{id:1, name: "approve"}, {id:2, name:"reject"}]}

Caller can change the definition any time; running instances are not affected by that change.
  > Start "Expense Approval" for document 123

 *Get Current Step*
 Caller asks for the current step for a subject, library returns

 {	"workflow_status": "In Progress"
 		"subject": {
 			id: ...,
 			versionToken: ...
 		}
 		"current_step":
 			{
 				name: "manager review",
 				actions: [
 					{id:1, name: "approve"},
 					{id:2, name:"reject"}
 				]
} 	

 > What step is currently on "Expense Approval for document 123"? => Manager review

*Complete Step*
Caller sends {subjectId, subjectVersionToken, stepId, actionId, completedBy}.
Library validates, stamps completedBy, executes the workflow, returns workflow_status and current_step.

*Get Assigned Steps (worklist)*
Caller sends a set of opaque assignee references — typically the current user plus their group memberships, resolved client-side — and the library returns open steps whose assigneeId is in that set.
This is how assignment becomes useful without the library knowing what a group is.

# State

_What must be remembered for each flow by the library?_

The definition-side entities (`WorkflowDefinition`, `WorkflowStatusDefinition`, `StepDefinition`, `ActionDefinition`) are templates.
The instance-side entities (`Workflow`, `Step`, `Workflow Status`) are snapshots taken from a definition at start time.

*Workflow Definition*
- id
- name
- initial step definition id  // the entry step the workflow begins on; set by the aggregate create, references a step in this same definition
- status definitions
- step definitions

*Workflow Status Definition*
- id
- workflow definition id
- name

*Step Definition*
- id
- workflow definition id
- actions
- workflow status definition id
- assignee_id  // opaque reference to the person or group expected to act on this step. A default, copied to the Step at workflow start.

*Action Definition*
- id
- workflow definition id  // denormalized, carried so the same-definition composite FKs have a column to match on
- step definition id  // the step this action belongs to
- name
- next step definition id  // nullable; the step this action routes to. Exactly one of this / terminal status is set.
- terminal workflow status definition id  // nullable; the status the workflow ends in when this action completes. Exactly one of this / next step is set.

*Workflow*
- id
- subjectId
- name
- steps
- current step id

*Step*
- id
- workflow id
- subjectVersionToken
- actions
- selectedAction
- workflow status id
- assignee_id  // opaque, copied from StepDefinition at start, mutable afterwards (reassignment).
- completed_by  // opaque reference to the actor who completed the step, stamped at completion.

*Workflow Status*
- id
- workflow id
- name

# Responsibilities

_What decisions must be made, and who owns them?
Does any component emerge naturally?_

- Who starts the workflow run? => Engine
- Who is the source of truth for providing aggregated info about a workflow run? => Engine
- Who processes a step run request? => Engine
- Who is the source of truth for current step run? => Engine

*Catalog*
- Provides ergonomic workflow definition generation for clients via an aggregate.
- Collects the workflow definition needed for a workflow start.
- Realized concretely by the exported `Definitions` type (constructed with `NewDefinitions(pool)`), holding the pool and owning definition-side transactions.
- Create is the only aggregate write — the whole definition tree in one transaction.
  Everything else is granular per-entity CRUD.
- Create accepts a whole definition tree and defaults the entry step to the first step when the caller leaves it unset; an explicitly-set entry step must reference a step in the tree, checked before the transaction. 
- Update semantics are full-replace of an entity's own scalar columns, never a cascade into children: an Update owns exactly its own row, and children are managed through their own Add/Update/Delete methods.
  Parent-membership and identity columns are immutable; re-parenting is Delete + Add.
  Each mutating operation takes a dedicated params struct carrying only the columns it may set, so the contract is enforced by the input type's shape rather than by documentation; a read type's `ToUpdate()` pre-fills those params from current state.

*Engine*
- Starts a workflow.
- Provides aggregated information for a run.
- Validates and processes a step complete request.
- Provides current step for a given workflow.

*WorkflowStatusDefinition, WorkflowDefinition, StepDefinition, ActionDefinition*
- Allows granular CRUD operations for definition objects.

*Workflow*
- Provides the details for a specific workflow instance.
- Provides current step.

*Step*
- Provides which version of the subject was worked on via "subjectVersionToken",
- Provides possible actions for the current step.
- Provides the action that was selected.
- Provides who the step is assigned to (assigneeId) and who completed it (completedBy).
- Does not decide whether the completing actor was permitted to act.

# Diagram

Client -- configure workflow --> Catalog

Client -- start workflow --> Engine -- get workflow definition --> Catalog
										   --> Engine -- save workflow, step, status --> DB
										   --> Engine -- complete first step --> Step -- set current workflow status --> Workflow

Client -- complete step --> Engine -- complete step --> Step -- set current workflow status --> Workflow

# Synchronization

_Handling concurrent events, duplicates, retries, and ordering._

The dangerous concurrent operations:

- Within the same library process multiple clients are:
	- configuring the same definition.
	- starting same workflow definition  for the same subject.
	- completing any step on the same workflow.

- Duplicate calls with the same params.

- Different library processes: are:
	- configuring the same definition.
	- starting same workflow definition  for the same subject.
	- completing any step on the same workflow.

Solutions to consider:
- Single-threaded event loop
- Lock/mutex
- Database transaction
- Unique constraint
- Idempotency key
- Message queue
- Version number / optimistic concurrency
- Retry with deduplication

# Invariants

_The rules that must never be violated._

*Definitions*
- Always provides most recent and consistent version of the definition.
Never provides a definition that is still under construction, never a previous stale definition.
- Same-definition integrity: a step's status, and an action's next step, always reference rows belonging to the same definition.
  Enforced in schema by composite foreign keys, not by application checks.
- The reference foreign keys are `DEFERRABLE INITIALLY DEFERRED`, so a whole-definition delete — whose cascade transiently leaves an action pointing at an already-deleted status — is checked at commit, when the state is consistent again.
  Single-statement operations still surface violations immediately, so referenced-delete and cross-definition blocks are unaffected.

*Engine*
 - Starts only one active workflow for a {subject, workflowDefinition}
 - Completes a step if it's the current workflow step and the requested action exists in the step.
 - Completes a step only a single time.

 *Workflow*
 - Status reflects the current step's status while the run is in progress, or the terminating action's status once the run is complete.

#Failure handling

_Detect, isolate, retry, compensate, or degrade._

Complicated failure handling like queues, retries are mostly the responsibility of the client system.

Major failure modes for the library itself:
	- Runtime exceptions.
	> Is the library responsible for logging or does it propagate the exception to the caller?

	- DB connectivity loss.
	> Library can't function without a DB, halt the operation.
	We should not allow half-saved config or workflow states in DB.
	Are DB transactions enough to prevent that?

	- Stuck - killed process.
	> Managing process is the responsibility of the client system.
	Could the solution to the "DB connectivity loss" be enough for this failure mode as well?

	- Infinite workflow step loops.
	> A validation on StepDefinition could be implemented later to prevent loops. (tortoise and hare algo?)

# Scale

_Identify bottlenecks and evolve the design._

What grows?
Requests per second?
Number of users?
Number of objects?
Read traffic?
Write traffic?
Fanout?
Storage?

Most of the scale problems reside on the client system.
Client system can resolve those questions by scaling the library instances, storage options, using caching etc...

For library, current performance - scaling enablers are:
	- proper database indexes
	- constructing efficient sql queries

# Tradeoffs

_What this design optimizes and what it sacrifices._

*Scope: Library - Full Solution*
This design encapsulates workflow functionality in a library with client system handling UI, logging, scaling as needed.
Adaptability, simplicity is traded for a more complete, out of the box "workflow system".
This could be the perfect addition for any existing system that needs the workflow functionalities.

*Workflow mutation: Allow mutation - No mutation, Version Every Config Changes*
Definition objects (WorkflowDefinition, StepDefinition...) are used as templates to start and run  the actual workflow instances.
This prevents weird mutations of the inflight workflow instances when a definition is mutated.

Workflow starts for a definition with "Director Approval" step.
From the definition "Director Approval" is removed.
Running workflow still awaits the "Director Approval."

The rule is: definition changes take effect on net new workflows.
Allowing definition changes to apply to running workflows would make them subject to a "partially executed on version A, then version B" state.

We could have versioned all definition changes with version numbers so that all versions are accessible but that would create much complexity with little benefit.

With current design system offers answer to the most important questions:
	- What is the current definition for new start? => Db definition rows are the source of truth
	- What are the possible steps and actions for this workflow in flight? => Db workflow, step rows are the source of truth
	- Why this finished workflow reached this state in the end? => Db workflow, step rows are the source of truth

This is achieved by the fact that the workflow and steps are effectively a snapshot of the definition's state when the workflow has started.

*Subject mutation: No library support for immutability - Library Enforced Strict Immutability*
With the library being subject agnostic, it's foreseeable to run into cases where the approved subject changes silently:
"When I approved this expense form, the total  was 100$ and not 10000$, who changed this?"

First instinct to resolve this is to store a copy of the subject in the library for each step but that would turn the library into a document store.

We decided to follow the following mechanism instead: the library holds an opaque subject reference plus an opaque version token (a hash, a revision id — the engine doesn't care, the consumer supplies it), captured at instance start and stamped onto every recorded decision.
Now "which revision did the strategist approve" is answerable from audit history forever, and the library never learned what a logo is.
Whether a changed token invalidates prior approvals or forces re-approval — that's policy, it varies by domain, and it's precisely the kind of thing that lives above the ceiling, in consumer code or a later config flag.

This trade-off removes enforcement from the library but still gives the client the power to enforce immutability in any shape it wants.

*Assignment: engine-enforced permissions - engine-recorded identity*
A workflow engine without assignment is not useful: someone has to be able to find what is waiting for them.
The question is whether the library should also enforce that only the assignee may complete a step.

Strict enforcement would mean checking completedBy against assigneeId.
That breaks as soon as an assignee is a group, because deciding whether a person belongs to a group requires the client's identity model, which the library deliberately does not have.
Opaque identifiers can be compared for equality, but equality is not membership.

So the library records identity and does not enforce it, mirroring the subject-token decision: references are captured and stamped, interpretation stays with the client.
Assignment still earns its place in the library through the worklist query — the client resolves the user's memberships and passes the resulting set, and the library filters on it.

Definition assigneeId is a default; instance assigneeId is the truth, and is mutable so steps can be reassigned.

If engine-side enforcement is wanted later, the shape is a client-supplied check invoked before completion (canComplete(step, actorId)), not an equality test — enforcement at the library's gate, judgment in client code.
This changes no tables and can be added when a real need appears.

# Stack

Go, Postgres, pgx v5 (native API), hand-written SQL in a repository layer, plain SQL migrations, tests against real Postgres.

The library is a single package `flowcore` at the module root.
Privacy comes from identifier case, not directory layout: the store — hand-written SQL and pgx calls — is unexported in its own files; the exported surface (`Catalog`, the definition types, the params structs, the error taxonomy) sits alongside it.
`internal/` is not used; it buys nothing for a single-package module and would force a read/write type split with a mapping tax.

IDs are UUIDv7, application-generated in Go before insert (`github.com/google/uuid`), `uuid` columns with no database default.
App-generated ids let the aggregate create assign every id up front, so an action can reference a step declared later in the same transaction with no `RETURNING` round-trips, and let a client store a stable id that is identical across dev, staging, and prod.

The store functions take a `querier` — a small unexported interface (`Exec`/`Query`/`QueryRow`) satisfied by both `*pgxpool.Pool` and `pgx.Tx` — so the same helper composes into autocommit or into a transaction.
Transaction control lives in exactly one place: only the aggregate create (and later the Engine's own methods) calls Begin/Commit; helpers never begin a transaction.
The idiom at every transaction site is `Begin` then an immediate `defer func() { _ = tx.Rollback(ctx) }()`, `Commit` at the end — the post-commit rollback returns `pgx.ErrTxClosed` and is ignored, which is what guarantees no leaked connection on any error path.

Errors surface as a small typed taxonomy over the DB's rejections, mapped centrally: constraint violations are keyed on SQLSTATE plus the explicitly-named constraint, and because a referencing insert and a referenced delete are byte-identical at the FK level, the mapping is split by operation intent into a write path and a delete path.
Every typed error wraps a sentinel so both `errors.Is` and `errors.As` work.
This requires every constraint the mapper switches on — unique indexes, composite FKs, CHECKs — to be explicitly named in the migrations.
Alongside these DB-mapped errors, Create performs pre-flight input checks that produce plain field-less sentinels (`ErrNoSteps`, `ErrInitialStepNotInTree`) before any transaction; these are `errors.Is`-matchable but have no typed form, since they map no DB rejection and carry no per-occurrence detail.

Migrations: goose (github.com/pressly/goose/v3).
Chosen because FlowCore ships migrations for clients to run into their own Postgres, and goose serves both consumption paths from the same files — a CLI for local development, and an embedded programmatic entrypoint (embed.FS) so a client can apply migrations from application code without installing a tool.
Its annotations are SQL comments, so the files remain runnable under plain psql by a client DBA.
It also has no "dirty schema" state to clear by hand after a failed migration, which is the wrong burden to hand an operator of someone else's library.

Migrations run over database/sql via the github.com/jackc/pgx/v5/stdlib adapter, since goose is written against database/sql.
This is scoped to the migration path only: the repository layer uses pgxpool and the native pgx API.
Same driver, two façades, at different moments.
goose's version table is named `flowcore_goose_db_version` (via goose's `-table` flag) so the library's migration history can never collide with a client that also uses goose for their own migrations.

Open: test Postgres provisioning (testcontainers | docker-compose), concurrency mechanism for step completion (version column | SELECT FOR UPDATE | serializable isolation — decide when writing the completion path).

# Iteration 1 Scope

Flows:
	*Configure Workflow*
	*Start Basic Workflow*
	 *Get Current Step*
	 *Complete Step*

Out of scope: AI review steps, Synchronization, Failure Handling, Scale

** Increment 2 candidate: automated advisory step type (findings + human override + audit)
a step type exists whose executor is external, async, fallible, and advisory rather than deciding.
flow idea: "AI director pre-check" -> finds 7 issues and shows warnings -> salesperson reviews the issues and makes changes or overrides the warnings...
