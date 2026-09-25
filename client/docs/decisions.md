# Client Decision Log

Design decisions for FlowCore's reference client, with the alternatives weighed and the reasoning
that settled each one.

This log is separate from the library's `docs/decisions.md` deliberately.
That one is about schema invariants, isolation levels, error taxonomy and boundary discipline, and
its value is that it is consistently about those things; application choices like a template engine
would dilute it.
The split also matches what `docs/system-design.md` says the library is — not a service, web API, or
application — so application decisions do not belong in its record.

**The one exception runs the other way.**
Anything building this client reveals *about the library* is a FlowCore decision and belongs in
`docs/decisions.md`, not here.
The client is the library's first real caller, so friction it exposes in the API is the library's
problem to record and fix.

Entries are append-only.
A later decision that reverses an earlier one gets its own entry and notes what it supersedes.

Decisions 1–7 were settled together in the design interview of 2026-09-21, before any code.

---

## 1. The client lives in this repository, as its own module

**Context.**
FlowCore needed a realistic caller. Where it lives determines whether the library's own rules apply
to it, what it may depend on, and whether anyone finding the library also finds it.

**Options.**
Same repository, same module.
Same repository, separate module.
A separate repository.

**Decision.**
Same repository, in `client/`, with its own `go.mod` and
`replace github.com/mike-akdeniz/flowcore => ../`.

**Why.**
Same-module was rejected on a measured property. Library decision 38 probed the dependency weight and
recorded that "with module-graph pruning only four non-stdlib modules actually compile in, and
`go.sum` stays at ten lines". A client in the same module would put a web stack into the library's own
`go.mod` — not compiled into a consumer's binary, but sitting in the file anyone opens to judge the
library's weight. That undoes the thing decision 38 verified.

A separate repository was rejected for reach. Splitting them means someone who finds the library does
not find the client, and a client nobody navigates to barely exists.

A separate module gets both. Go excludes nested modules from the parent's package tree, so the
library's dependency graph never learns about this one, and the client may depend on anything it
likes without a conversation. The `replace` directive means it always builds against the working
tree, so it cannot drift from the library it demonstrates and no release must be tagged to keep it
current.

**Consequence.**
Nested modules do not compose under one command: `go test ./...` at the root does not reach here, so
the Makefile needs a second target and CI — which does not exist yet — needs a second job.
The library's stack constraints stop at this module boundary, which `CLAUDE.md` now records.

---

## 2. Server-rendered with HTMX, not a single-page application

**Context.**
The configuration UI is the interdependent part: adding a step must update the routing choices on
every other step, because actions point at steps. That interdependence is what usually argues for a
client-side framework.

**Options.**
Classic server-rendered forms with full page reloads.
HTMX returning HTML fragments.
A JSON API with a React or Vue front end.

**Decision.**
`html/template` plus HTMX, with mermaid.js rendering the definition as a graph, and a CDN stylesheet.
No build step, no `node_modules`, no second toolchain.

**Why.**
HTMX solves the interdependence without client-side state: the server returns the fragment that
changed and HTMX swaps it in. The whole application stays one language and `go run .` stays the
entire setup, which matters for something meant to be cloned and read.

A React front end was priced rather than dismissed. Its cost is not the canvas — React Flow makes a
graph editor cheap — but two things that are easy to miss: reconciling free-form graph edits back
into granular `Catalog` calls, and needing a JSON API across the *whole* application rather than only
the editor. Estimated at roughly twice the HTMX build, against a stated willingness to pay about 30%
more for a drag-and-drop builder.

A drag-and-drop canvas is also more impressive to watch than to use for a six-step workflow, and a
janky drag interaction reads as unpolished on a repository whose argument is craft.

**On the concern that server-rendered looks unserious**, which was raised directly and is worth
answering in the record: HTMX endpoints are ordinary endpoints. Only the final write differs — a
template execution rather than a JSON encoder — and the application layer that matters is unchanged.
That layer resolves identity, holds subjects, runs the agent dispatcher and translates the library's
typed errors, and it *is* the client half of the boundary. What reads as unserious is an unlayered
handler, which is independent of the stack.

**Consequence.**
A plainer interface than a design-system application, and CSS written by hand over a CDN base.
Nothing is foreclosed: the `Catalog` API is identical either way, so a canvas can arrive later as a
second front end over the same endpoints without wasting the form work.

---

## 3. Two scenarios, both text-shaped

**Context.**
One scenario demonstrates a workflow. It cannot demonstrate that the library is subject-agnostic,
which is the first claim the library's README makes. The configuration UI does not help here — the
subject never appears in it.

**Options.**
One scenario.
Expense approval plus a second, simple one.
Two substantial scenarios.

**Decision.**
**Software release approval** and **insurance claim**, both with text subjects and text model calls.
Expense approval — the library's own running example — is not among them.

**Why.**
Two subject types of genuinely different kinds are what demonstrate subject-agnosticism. One cannot,
however good it is.

Expense approval was dropped because both scenarios should be substantive: one that is easy to grasp
and one that shows the library handling real complexity, rather than a toy plus a real one. The cost
is the loss of a two-minute on-ramp, accepted because the configuration UI is a better on-ramp anyway
— building a two-step workflow yourself is simpler than any seeded scenario and it is participatory.
The release workflow is the default view, since the likeliest visitor ships software.

Both are text-shaped on purpose. A second scenario costs about a day when it shares the shape of the
first and about a week when it changes medium. Photographs in the claim scenario would add upload,
storage, binary assets in the repository and vision calls, for a judgment that reads perfectly well
as text: a claimant's account of the incident contradicting the police report's timeline.

Each scenario carries two agent steps, and they must be *different kinds* of judgment rather than two
flavours of checking text against guidelines. Release: classify risk from a diff, then verify a
changelog against what actually changed. Claim: completeness, then narrative consistency.

**Consequence.**
The library's documentation and the client now use different examples. Accepted: the docs' example
explains the library, the client's examples show applications, and the two jobs are different.
Photographs stay on the candidates list.

---

## 4. Agent steps dispatch through the client's own queue

**Context.**
Library decision 43 accepted two integration shapes: the caller dispatching inline from the response
it already holds, and the caller enqueuing onto its own job queue. The client can only demonstrate
one as its default.

**Options.**
Inline, within the web request.
The client's own queue, completed by a worker.

**Decision.**
The queue. The request returns as soon as the run reaches an agent step; a worker goroutine picks it
up, calls the model, and calls `CompleteStep`.

**Why.**
Inline dispatch completes the whole loop inside one HTTP request, and a skeptic can fairly say that
is a function call chain rather than something needing a workflow engine. They would have a point:
nothing in that sequence ever stops.

The queue makes the run visibly sit open with nobody attending it. That is the durability claim, and
it is the actual distinction from an agent framework's supervised turn, where the pause lives in a
process rather than in a row. It also puts the worklist to work for a non-human consumer, which is
what the library's iteration 2 built it for.

The cost is small, and specifically small in Go: a goroutine, a channel, and one HTMX polling
attribute.

**Consequence.**
The call log becomes two phases with a gap rather than one readable top-to-bottom sequence, which is
slightly harder to follow and slightly harder to capture in a short recording.

---

## 5. Model calls detect a key and fall back to canned findings

**Context.**
Four agent steps across two scenarios means a visitor with no API key would otherwise see a demo that
stops working at the first interesting moment.

**Options.**
Always call the model.
Always use canned responses.
Detect a key at startup and choose.

**Decision.**
Detect. `ANTHROPIC_API_KEY` present means real calls; absent means pre-written findings authored
against the seeded data. One `Checker` interface, two implementations, a badge in the interface
showing which is live.

**Why.**
Everything else is identical — same step, same `CompleteStep` call, same remark stamped on the visit,
same screens. Only the source of the finding text differs. So someone who clones and runs with
nothing configured gets the whole thing working in thirty seconds, and someone who wants to verify
the integration is real exports a key and restarts.

**Consequence.**
The canned responses are maintained alongside the prompts and will drift if the seed data changes
without them.

---

## 6. Per-session isolation, entirely in client code

**Context.**
Hosting means concurrent visitors sharing one database. Someone will delete a seeded workflow while
someone else is using it. A reset button does not solve this — it only undoes damage while causing
more, wiping out whatever an active visitor was doing.

**Options.**
No isolation, with periodic reseeding.
Per-session isolation in the client.
A tenant column in the library.

**Decision.**
A session cookie, and every piece of state scoped to it by the client. The library is unchanged.

**Why.**
It needs no library change, and one part of that is a happy accident. `Catalog` has no `List` method,
only `Get(id)`, so the client must already track which definition ids it created; keying that by
session makes "your workflows" naturally only yours. Subjects are the client's own store. Runs are
keyed by `{subjectReference, definitionID}`, and prefixing the subject with the session id gives two
visitors on one seeded workflow two separate runs. The worklist returns rows across sessions, and the
client filters them by `AssignedStep.WorkflowDefinitionID` against its own set — so assignee strings
are never prefixed, and a visitor who types `group:security` gets exactly that stored.

A tenant column in the library was never seriously in play. `CLAUDE.md` names `tenant_id` as its
canonical example of structure with no caller, and this is the caller arriving and *not* needing it.
A client doing multi-tenancy in its own code over a library with no tenant column demonstrates the
"authorization and identity live in the client" claim rather than asserting it.

**Consequence.**
Sessions need expiry when hosted, so a janitor deletes old ones and the cascades clean up the rest.
`CLIENT_SESSION_TTL` defaults to `0`, meaning never expire and no janitor. It is set when deploying
publicly. Defaulting to never fails in the safe direction — a forgotten setting grows the database
slowly, where the reverse loses a local user's work.

Seeding must happen per session on first request, never at startup. That is the detail that lets
local and hosted use run the same code path, and retrofitting it means untangling seed logic later.

---

## 7. Called the client, not the demo

**Context.**
The working name through the design interview was "demo".

**Decision.**
`client/`, described in prose as the **reference client**.

**Why.**
`docs/system-design.md`'s Actors section already uses "Client" for code that calls the library, so
this is the project's own word for the role rather than a new label. It also says what the thing is
for: something to run, read, lift snippets from, or fork as the starting point for a real
application. "Demo" implies *look at this*; "client" implies *you could build on this*.

**Consequence.**
`something/client` reads as an SDK in Go — a wrapper for calling a remote service — which is exactly
backwards for a library that is not a service. Prose therefore says "reference client", and the
README must open by saying it is an application built on FlowCore.

The name also raises the promise slightly, so the README has to be honest about what this is not:
authentication is faked and there is no user management, because those demonstrate nothing about the
library and would be the largest code in the repository.
