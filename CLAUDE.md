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