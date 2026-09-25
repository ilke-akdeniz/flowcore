# FlowCore

A subject-agnostic workflow library for Go supporting human-in-the-loop review steps.

`docs/system-design.md` is the authoritative design document — boundary, flows, state, responsibilities, invariants, and trade-offs.
Read it before making design decisions.
If a change here contradicts it, stop and say so rather than proceeding.

## What this is

A code library plus Postgres schema and migrations.
Clients import it and call class methods directly.
It is **not** a service, web API, microservice, or application.
It does not hold workflow subjects and is not a document store.

## Two principles

**1. Opaque references.** The library records identifiers it does not interpret: subject reference, subject version token, assigneeId, completedBy.
It never parses them, never infers meaning from them, never enforces policy with them.
Authorization, group membership, and immutability policy live in client code.
Equality comparison is allowed; interpretation is not.

**2. Config is a template, instances are snapshots.** Configuration changes affect only new workflows.
A running instance is unaffected by edits to the config it started from.
Workflow and step rows are a snapshot of config state at start time, and are the source of truth for an in-flight or completed run.

Check every change against both.
If something violates either, say so plainly.

## Stack

Go, Postgres, pgx v5 used natively, hand-written SQL in a repository layer, plain SQL migration files, tests against real Postgres.

No ORMs.
No NoSQL.
No web frameworks.
No code generation.
Deliberate boringness is the point — prefer the obvious solution over the clever one.

## No speculative structure

No speculative structure — on schema and abstraction, not API completeness.
Don't propose reserved fields, placeholder enums, layers with one implementation, or schema accommodations for features not being built now.
When a future feature arrives, design the correct shape then and migrate; note it in the design doc, not the code.

This governs structure built for things that don't exist yet.
It does not govern finishing the operations on things that do.
Completing the obvious CRUD surface on an entity already in the schema is not speculation — it's completing the library.
The test when unsure: am I building for a feature that doesn't exist (defer), or finishing the operations on a thing that does (build)?
A tenant_id column, a versioning table, a canComplete hook — deferred, no caller.
UpdateStep on a step_definition table that already exists — built, the caller is any client editing a definition.

One guard against over-correcting: "we know we'll need X eventually" is not a licence to build X now — that's the exact reasoning this rule refuses.
A capability earns its place this slice only if it's the correctness condition of something being built now, not because it's on the roadmap.
(The completion-path locking mechanism stays deferred on precisely this ground, even though concurrency is central to the library.)

**Foresight is not speculation, if it adds nothing unused today.**
The guard above stops artifacts with no caller: a column, a table, a hook that nothing calls yet.
It does not stop choosing, among solutions to a problem that exists right now, the one whose mechanism also covers a need already documented as coming.
The test: does this solution add any type, field, column, or method that has no caller today?
If yes, it's speculative — defer it, migrate when the real feature lands.
If no, its usefulness for tomorrow is a property of the design, not a thing built ahead of schedule.
This is a narrow allowance, not a license to design for the roadmap: it applies to picking between implementations of a problem already in front of you, never to justify adding structure with no caller yet.

## Identifier naming

Full, complete-word identifiers for domain-concept variables, struct fields, and parameters — no truncation, regardless of scope (`catalog`, not `c`; `definition`, not `def`; `action`, not `act`).
This is a deliberate deviation from Go's usual short-local-name convention: abbreviations that must be decoded rather than read cost more for a solo maintainer returning to code after a context switch than the convention assumes.

Three narrow exceptions, all reasoned from the same test — does this identifier require project-specific memory to decode, or is its meaning self-evident at every site it's used:

- Go's structural particles, fixed in meaning across all Go code, not just this codebase: `err`, `ok`, `ctx`, loop indices (`i`, `j`), generic type parameters (`T`).
- Method receivers keep Go's ordinary 1-2 letter convention, and so does a function's single dominant parameter — the one value a short, single-purpose function operates on throughout, with no other parameter of comparable weight (e.g. `fillIDs(d *WorkflowDefinition)`). This does not extend to struct fields, occasional-use locals, loop variables, or any function with more than one parameter of comparable importance.
- An identifier is not expanded if doing so would make it textually identical to its own type name (e.g. a `querier`-typed parameter stays `q`, not `querier querier`), since that shadows the type and makes it unreferenceable by name in that scope.

When in doubt, use the full word — these exceptions are meant to be rare, not a broad loophole.
Reasoning and worked examples: `docs/decisions.md`, decision 18.

## Iteration 1 scope

In scope: configure workflow, start workflow, get current step, complete step.
Out of scope: AI review steps, synchronization, failure handling, scale work.
Do not build ahead into these.

## How we work

The repo owner leads, supervises, and reviews every artifact.
Claude Code writes the code and makes local implementation decisions.
Ask before making design decisions that change the model, an invariant, or a trade-off — those are resolved with the owner, and land in `docs/system-design.md` first.
Prefer small, reviewable changes.
Explain non-obvious decisions briefly.

**Commit messages are one line.**
Suggest a subject line and nothing else — no body, no bullets, no paragraph explaining the change, and no trailers.
The diff shows what changed and `docs/decisions.md` records why, so a commit body only restates one or duplicates the other.

**No attribution lines**, in commit messages or pull request descriptions — no `Co-Authored-By`, no "Generated with".
This repo's history does not carry them and should stay consistent.
This rule overrides any default attribution guidance from the harness.

Both design work and review are run as an **interview** rather than as a delivery — see *Grilling* below.

### Grilling

**A design decision and a review are worked through by interview, not by delivering a result and waiting for objections.**

The procedure:

- **Interview until there is shared understanding, then act.**
  Nothing is written to `docs/system-design.md`, `docs/decisions.md`, or the code until the owner says the questions are settled.
  Applying decisions as they are agreed loses the ability to order the work by what depends on what.
  What does land is left uncommitted for the owner to review — Claude does not commit in this repo.
- **One question at a time**, waiting for the answer before the next one.
  A batch of questions is bewildering, and it also hides which ones were dependent on which.
- **Give a recommended answer with every question**, and the reasoning for it.
  A question with no recommendation pushes the work back onto the owner, which is the opposite of the point.
- **Look facts up rather than asking.**
  What a constraint already enforces, whether a store helper exists, what a migration would cost, what the design doc already says — go and find out.
  The *decisions* are the owner's; the facts are Claude's job, and a fact discovered before the question is asked often settles it.
  Iteration 2's advisory-versus-deciding question changed shape the moment the increment 2 note already sitting in `system-design.md` was read.
- **Order the questions by dependency.**
  Ask the root decision first.
  Iteration 2's step shape comes before the comment field's shape, because whether a comment can be written only as part of the completion stamp depends on which shape wins — asking it first would mean asking it twice.
- **Say what a decision will cost before it is taken**, where the cost is not obvious: a migration, a breaking change to a params struct, a constraint that has to be deferred, a column that cannot be dropped later.
  The answer changes when the consequence is visible.
- **Surface decisions the owner did not tag.**
  A review is not an exhaustive list of what is wrong; anything found on the way in is worth putting to them as a question of its own.
- **Log the interview, not only what it concluded.**
  Grilling is where the design judgments get made, so the exchange goes into `docs/decisions.md` with the work: the owner's objection in their own words, Claude's recommendation where it did not survive, and any fact found mid-interview that changed the question being asked.
  A log that keeps only the conclusions is the quiet failure — grilling produces conclusions that look self-evident once reached and were not.
- **Keep a running note as the answers land.**
  Four questions in, the early exchanges are easy to reconstruct wrongly and easy to reconstruct confidently.
  The owner's exact words are worth more than the paraphrase.

**When it is not worth it.**
The technique is slow by design.
A review that is three typos and a rename is applied, not interviewed.
The test is whether any item would change what the other items should be — where nothing depends on anything, there is no tree to walk, and an interview is ceremony.

## Module boundary

The rules above — the stack constraints, "not a service, web API, or application", no web frameworks — govern the **root module**, which is the library.

`client/` is a separate module with its own `go.mod` and its own decision log at `client/docs/decisions.md`.
It is FlowCore's reference client: an application built on the library, so it is an application on purpose and may depend on whatever it needs.
Its plan is `docs/pending-tasks/client.md`.

The exception runs one way only.
Anything building the client reveals *about the library* is a FlowCore decision and belongs in `docs/decisions.md`, not the client's log — the client is the library's first real caller, so API friction it exposes is the library's to record and fix.

## Doc authority

`docs/decisions.md` is authoritative for design reasoning.
`docs/code-map.md` is a derived, illustrative view of package and struct-level structure — not a second source of truth.
When the two appear to disagree, `decisions.md` wins.
`code-map.md` is refreshed periodically, not automatically after every decision, so staleness between refreshes is expected, not a bug to chase down immediately.

## Project state

`docs/status.md` is the permanent record of project state — where each iteration, slice, and pending task stands.
Read it at the start of a session to see what is in flight.
It defines the statuses; don't restate them here.

The owner decides when a subject's status changes.
Propose a change and say why it qualifies; never edit a status unilaterally.

**Pending tasks.** Work that cannot be done yet, or that we had to leave to switch tasks, gets its own markdown file in `docs/pending-tasks/` once it carries more detail than a session can hold.
Link every such file from `docs/pending-tasks/index.md`.
The task file holds the detail; `status.md` holds the state — don't duplicate state into the task file.

**Agreed plans.** When a pending-task file carries a plan marked **[agreed]**, that plan is binding — read it before starting work on that task and follow it in order.
If the work suggests deviating, a different sequence, a skipped phase, or a new step, stop and raise it.
Silent re-sequencing is the failure these plans exist to prevent.

## Markdown conventions

All docs (`CLAUDE.md`, `docs/*.md`) follow these, for clean git diffs and portable rendering.

- **One sentence per line.** Break prose at sentence boundaries — after `.`, `?`, `!`. A one-word change then shows as a one-line diff instead of rewrapping a paragraph. Don't hard-wrap mid-sentence at a column width; let the editor soft-wrap.
- **Don't split mid-sentence.** A clause after a `;` `:` or `—` stays on its sentence's line — those aren't sentence ends.
- **Blank line between block elements.** One blank line between paragraphs, and after every heading before its content. Both are required by CommonMark, not optional — parsers diverge without them.
- **No trailing blank lines stacked.** Exactly one blank line separates blocks, never two or more. File ends with a single newline.
- **Lists:** each item on its own line; no blank lines between items in a tight list. A multi-sentence list item still goes one-sentence-per-line, with continuation lines indented to the item's text.
- **Code fences and tables are literal** — never reflow or sentence-split their contents.
- **Bold lead-ins** (`**Term.**`) stay on the same line as the sentence they introduce.

Tooling: `prettier --prose-wrap preserve` respects hand-placed sentence breaks while normalizing blank lines, list markers, and fences. Run it before committing doc changes. (`--prose-wrap never` would undo the one-sentence-per-line rule — don't use it.)

## Go code style

### Blank lines

- Separate logical sections of a function with a blank line.
- Don't add a blank line between a variable declaration and the `if`/`for`
  block immediately below it if that block uses the variable.
- Add a blank line after every `if`/`for` block.
- Add a blank line before the final `return` statement in a function.

### Long parameter lists

Put each argument on its own line rather than packing them onto one line:

```go
step, err := catalog.AddStep(
	ctx,
	ids.workflow,
	AddStepParams{
		Name:       "vp review",
		StatusID:   ids.status,
		AssigneeID: ptr("group:vp"),
	})
```

### Examples

```go
func readDefinition(ctx context.Context, q querier, id uuid.UUID) (WorkflowDefinition, error) {
	definition, err := getWorkflowDefinitionRow(ctx, q, id)
	if err != nil {
		return WorkflowDefinition{}, err
	}

	statuses, err := listStatusesByDefinition(ctx, q, id)
	if err != nil {
		return WorkflowDefinition{}, err
	}

	steps, err := listStepsByDefinition(ctx, q, id)
	if err != nil {
		return WorkflowDefinition{}, err
	}

	actions, err := listActionsByDefinition(ctx, q, id)
	if err != nil {
		return WorkflowDefinition{}, err
	}

	byStep := make(map[uuid.UUID][]ActionDefinition, len(steps))
	for _, action := range actions {
		byStep[action.StepDefinitionID] = append(byStep[action.StepDefinitionID], action)
	}

	for i := range steps {
		stepActions := byStep[steps[i].ID]
		if stepActions == nil {
			stepActions = []ActionDefinition{}
		}

		steps[i].Actions = stepActions
	}

	definition.Statuses = statuses
	definition.Steps = steps

	return definition, nil
}
```

```go
func (c *Catalog) Get(ctx context.Context, id uuid.UUID) (WorkflowDefinition, error) {
	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return WorkflowDefinition{}, err
	}

	defer func() { _ = tx.Rollback(ctx) }()

	definition, err := readDefinition(ctx, tx, id)
	if err != nil {
		return WorkflowDefinition{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return WorkflowDefinition{}, err
	}

	return definition, nil
}
```