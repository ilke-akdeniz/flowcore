# FlowCore

A subject-agnostic workflow library for Go supporting human-in-the-loop review steps.

`docs/system-design.md` is the authoritative design document — boundary, flows,
state, responsibilities, invariants, and trade-offs. Read it before making design
decisions. If a change here contradicts it, stop and say so rather than proceeding.

## What this is

A code library plus Postgres schema and migrations. Clients import it and call
class methods directly. It is **not** a service, web API, microservice, or
application. It does not hold workflow subjects and is not a document store.

## Two principles

**1. Opaque references.** The library records identifiers it does not interpret:
subject reference, subject version token, assigneeId, completedBy. It never
parses them, never infers meaning from them, never enforces policy with them.
Authorization, group membership, and immutability policy live in client code.
Equality comparison is allowed; interpretation is not.

**2. Config is a template, instances are snapshots.** Configuration changes
affect only new workflows. A running instance is unaffected by edits to the
config it started from. Workflow and step rows are a snapshot of config state at
start time, and are the source of truth for an in-flight or completed run.

Check every change against both. If something violates either, say so plainly.

## Stack

Go, Postgres, pgx v5 used natively, hand-written SQL in a repository layer,
plain SQL migration files, tests against real Postgres.

No ORMs. No NoSQL. No web frameworks. No code generation. Deliberate boringness
is the point — prefer the obvious solution over the clever one.

## No speculative structure

Do not add reserved fields, placeholder enums, unused interfaces, or schema
accommodations for features not being built now. When a future feature arrives,
the correct move is to design the right shape then and migrate. Future
possibilities are noted in the design doc, never in code.

## Iteration 1 scope

In scope: configure workflow, start workflow, get current step, complete step.

Out of scope: AI review steps, synchronization, failure handling, scale work.
Do not build ahead into these.

## How we work

The repo owner leads, supervises, and reviews every artifact. Claude Code writes
the code and makes local implementation decisions. Ask before making design
decisions that change the model, an invariant, or a trade-off — those are
resolved with the owner, and land in `docs/system-design.md` first.

Prefer small, reviewable changes. Explain non-obvious decisions briefly.
