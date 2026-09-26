# Reference client — UI rebuild

## What this file is

The agreed plan for rebuilding the reference client's UI layer on React, settled by interview on
2026-09-25.

**The slices below are [agreed] and binding.**
Follow them in order; if the work suggests a different sequence, or a slice turns out to be
unnecessary, stop and say so rather than re-sequencing quietly.

This file holds the plan only.
What the client *is* lives in [`client/docs/system-design.md`](../../client/docs/system-design.md);
why it is that way lives in [`client/docs/decisions.md`](../../client/docs/decisions.md), entries 10
to 19.
Neither is restated here.

It supersedes the UI half of [`client-htmx.md`](client-htmx.md).
That file's phases 1 to 6 are complete and its reasoning about the Go side still holds; only its
stack and screens are overtaken.

## Why the order is what it is

Risk first, not the user's journey.

The novel work is the workflow canvas — React Flow is a library, but reconciling graph edits back
into granular `Catalog` calls is genuinely new, and the worst time to discover it is hard is after
four slices have been built assuming it.
So the read-only canvas comes second, immediately after the skeleton, proving the data shape, the
layout engine and the rendering with none of the reconciliation.

The second submission type is slice 5 to test a design bet rather than to add content: the shared
`submission` table plus per-type detail is a guess, and policy applications are what prove it.
Leaving it last would risk finding the split wrong after everything is built on it.

## The slices

### 1 — Walking skeleton

- Client schema and migrations: `submission`, `claim_detail`, `application_detail`, `document`,
  `user`, `workflow_registry`, each session-scoped except `user`.
- Per-session seeding as a data copy: the two workflows through `Catalog.Create`, two drafted
  submissions, their documents.
- Vite + React + TypeScript + Mantine, built and embedded in the Go binary with `embed`.
- Sign-in screen listing the seeded cast, and My work showing one real row.
- **Delete `internal/web` and its templates.**

Done when `go run .` serves a React application reading real data, and no HTMX remains.

Deleting the old UI here rather than at the end is deliberate: between slices 1 and 6 the
application does less than it used to, which is normal for a rewrite and costs nothing, and one
honest gap beats two half-wired UIs.

### 2 — Understanding workflows

- Workflow list: name, which submission type it serves, active or retired.
- Read-only canvas: React Flow with automatic layout, no stored coordinates.

Done when a workflow can be understood at a glance.
This is the de-risking slice; if the canvas is going to be a problem, it surfaces here.

### 3 — Working a case

- Claim detail with its documents, and a panel showing where the run stands.
- Complete a step, with a remark.
- The agent dispatcher adapted to database subjects, so AI steps run and their findings appear.

Done when the core loop works: open, decide, watch it move — including the steps nobody touches.

### 4 — Submitting

- New submission form, draft then submit.
- Workflow lookup from the registry, then `Start`.

Done when the visitor's first action is submitting a seeded draft and watching triage decide.

### 5 — The second submission type

- Policy applications: detail table, detail screen, its workflow, its risk screen.

Done when one queue carries both kinds and the two detail screens share nothing but a header.

### 6 — Configuring workflows

- The editor on the same canvas: click a node to edit it, drag an edge to create an action, add and
  delete steps and statuses, set the entry step.
- Activate a workflow for a submission type.

Done when a workflow can be built and a type switched onto it — and cases already running keep the
one they started under.

### 7 — Close out

- `client/README.md` and the library `README.md`.
- A polish pass.
- Delete whatever is dead.
- Propose *Complete*; the owner decides.

## Out of scope, and why

Recorded so none of it is relitigated mid-build.

- **Claimant-facing submission.** Commodity, and the console works without one. An internal
  submission form is in scope.
- **Real authentication and user management.** Sign-in is a choice from a seeded list.
- **File upload.** Documents are text records (decision 19).
- **Hand-positioned graph nodes.** Automatic layout, no stored coordinates (decision 14).
- **Any library change.** If building reveals one, it goes to the owner and into
  `docs/system-design.md` and `docs/decisions.md` first — not into this plan.
