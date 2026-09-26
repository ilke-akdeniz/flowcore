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

## Orientation: one client, two UI layers

The client has had two UI layers. The first was server-rendered Go templates with HTMX; the second,
from decision 10 onward, is React with a JSON API. Only the UI layer was replaced — `internal/app`,
which is the client's whole half of the boundary, carried over.

So most of the early entries are still in force, and it is worth knowing which before reading them:

| Entry | |
| --- | --- |
| 1 module lives in this repo · 4 agent queue dispatch · 5 detect-a-key checkers · 7 called the client · 8 `w, r` in handlers | **still in force** |
| 6 per-session isolation | **in force, amended by 15** — database rows rather than memory |
| 2 HTMX over an SPA | **superseded by 10** — and not because it was wrong; the goals changed |
| 3 the release and claim scenarios | **superseded by 12** — claims and policy applications |
| 9 the HTMX editor rebuild | **moot** — that UI layer is gone |

Nothing is deleted or rewritten. This table is the map.

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

---

## 8. Handler parameters stay `w, r`

**Context.**
`CLAUDE.md` requires full, complete-word identifiers and rejects truncation, with narrow exceptions:
Go's structural particles (`err`, `ok`, `ctx`, loop indices), a method receiver, and a function's
single dominant parameter. An HTTP handler has two parameters of comparable weight, so it fits none
of them.

**Decision.**
`w http.ResponseWriter, r *http.Request` throughout the web layer.

**Why.**
The rule's own test is whether an identifier needs project-specific memory to decode or is
self-evident everywhere it appears. `w` and `r` in a handler signature are fixed in meaning across
all Go code — closer to `ctx` and `err` than to `def` for definition — and `writer, request` would
read as strange to any Go reader without making anything clearer.

Recorded because it is a visible deviation from a stated rule, and silence would leave a reader
unsure whether it was considered or careless. The rule holds everywhere else here: one truncation,
`envOr`, was found and renamed to `environmentOr`.

**Consequence.**
The exception is exactly this signature, not a general licence for short names in the web layer.

---

## 9. The editor was rebuilt to actually use the stack decision 2 chose

**Context.**
Shown the finished client, the owner's verdict on the workflow editor was that it looked
unsalvageable "with current html thing" — specifically: a diagram, then oversized buttons, then a
weird form, and a full page reload on every edit.

**The finding that reframed it: there was no HTMX in the client at all.**
Decision 2 chose HTMX and gave the reasons. Phase 2 then wrote "plain forms plus redirect for now,
HTMX arrives when polling needs it", and it never arrived — so the editor shipped as nine separate
forms doing nine full page reloads. That is precisely the *classic server-rendered* option decision 2
considered and rejected by name.

So the interface being judged was not the one that was chosen, and "server-rendered HTML cannot do
this" was a conclusion drawn from a stack nobody had built.

**Options.**
Rebuild the editor on the chosen stack.
Abandon server rendering for a React canvas, as decision 2 had priced at roughly twice the cost.

**Decision.**
Rebuild. Each of the three complaints had a cause that was not the stack:

- *Page reloads* — HTMX absent. Edits now `hx-post` and swap the whole editor fragment in place.
- *Oversized buttons* — Pico styles every `<button>` inside a form as full width. One rule opts out.
- *The weird form* — the layout was four stacked forms inside a fieldset per step, all on screen at
  once. Now the step list is the navigation and one step is edited in a panel beside the graph.

**Why the whole fragment rather than fine-grained swaps.**
Every edit returns the entire editor and replaces it. A status rename changes the picker, the step
form's dropdown, the action targets and the diagram, so partial updates would mean four coordinated
swaps and a chance of them disagreeing. One fragment is always internally consistent, and at roughly
10 KB against a 23 KB page the saving is real without the bookkeeping.

**Consequence.**
Errors now return inline with the fragment — a duplicate name renders in place at HTTP 200 rather
than round-tripping through a redirect and a query parameter.

Nothing depends on JavaScript: handlers check `HX-Request` and fall back to the redirect they used
before, so the editor still works with scripting off. That fallback is why this was a rebuild of the
presentation rather than of the application.

The React question stays open and unchanged. What was rejected here is concluding it from an
interface that never used the alternative.

---

## 10. Rebuilding the UI on React, and what that supersedes

**Context.**
Shown the finished HTMX client, the owner judged the interface bad, then narrowed the diagnosis to
information architecture — "you are trying to do 10 different things in a single page" — and decided
to rebuild on a modern front-end stack.

The goals moved at the same time, and that is the part worth recording:

1. A UI portfolio piece: a backend developer working competently with modern front-end tools.
2. A workflow application showing what is possible with FlowCore, especially its AI steps.
3. An extension of 2: configuring a workflow is part of the product, not a settings page.

**Decision.**
React, TypeScript and Vite, as a single-page application against a JSON API served by the existing
Go binary. Vite builds to static assets, Go embeds them, and `go run .` still serves everything.

**This supersedes decision 2, and not because decision 2 was wrong.**
That entry priced a React front end at roughly twice the HTMX build and declined it. The estimate
stands. What changed is that "UI portfolio" was not a goal when it was taken — it is now the first
one, and it is judged precisely on the thing decision 2 economised on.

**Why not Next.js.**
No SSR need: an internal console has no SEO, no public pages, no first-paint pressure. It would add
a Node runtime to production and a second deployment story for features this application does not
use. SvelteKit is nicer to write and a weaker portfolio signal, because fewer reviewers can assess
it at a glance.

**Why the Go side barely moves.**
`internal/app` is 2,242 lines of identity resolution, subject storage, agent dispatch, seeding and
error translation. None of it is presentation. Only `internal/web` — 18 routes and five templates —
is stack-specific.

**Consequence.**
A second toolchain: npm, a build step, `node_modules` beside a Go module with four dependencies.
That is the cost decision 2 declined, accepted now for a reason that did not exist then.

**One thing the rebuild is not.**
The stack was not the reason the old editor read badly. Decision 9 records that the client shipped
with no HTMX in it at all, so the interface being judged was the *classic server-rendered* option
decision 2 had rejected by name. The rebuilt HTMX editor fixed the mechanics and the pages were
still overloaded, which is what isolated the real problem as information architecture. React does
not fix pages that do too much; the screen inventory in `system-design.md` does.

---

## 11. Mantine, and the goal it was chosen against

**Context.**
The component and styling layer decides whether an application looks bespoke or looks like a kit
with someone's data in it. For goal 1 it matters more than the framework choice did.

**Options.**
Tailwind with shadcn/ui — copy-in primitives, assemble your own design system.
Mantine — comprehensive, modern defaults.
MUI — the safest "knows the standard tool" signal, unmistakably Material.
Ant Design — purpose-built for dense internal consoles, and visually dated.

**Decision.**
Mantine.

**Why, and the correction that produced it.**
The first recommendation was Tailwind with shadcn/ui, on the argument that hand-assembled components
read as design work rather than as a kit. The owner corrected the goal:

> I'm a backend developer that can work with modern UI tools - libraries. I don't claim to be a
> modern UI genious and this client app is trying to showcase that what a good app using flowcore
> can do.

That inverts the answer. Assembling a design system from primitives is what a front-end specialist
does; for a backend developer it is a large amount of time spent on the part that is not the point,
with a real risk of looking half-finished. Half-finished bespoke reads worse than a kit used well,
and a kit used competently demonstrates judgment about what not to build.

The stronger reason is in the owner's own sentence: the application exists to show what a good
application *using FlowCore* can do. The queue, the case file, the graph and the AI steps landing are
the subject. Hand-rolling a button component is time taken from them.

**Consequence.**
The saved effort goes into workflow features — which is what made a React Flow canvas worth building
in decision 14, having been declined at twice the price under the previous stack.

This is the second time in the same interview that a goal was inflated past what the owner stated;
the first was treating the library's boundary as the client's purpose. Worth watching for.

---

## 12. A specific application, not a generic tool

**Context.**
With the teaching goal dropped, what is the client *for*? The previous one seeded two hardcoded Go
types and let you configure any workflow — a canned demo wearing a configurable hat, since the
configuration had nothing real to run against.

**Options.**
A richer canned demo with fixed domains.
A generic workflow tool where subjects are things you create.
One specific, realistic application for one domain.

**Decision.**
One specific application: an insurer's internal case console, handling **claims** and **new policy
applications**.

**Why, in the owner's words.**

> As much as possible, the client should look like a real app. Not "generic tool", not "demo whatever
> caller". It's a specific app... Designed the way a modern "insurance claim app" should look and
> function. Then it integrates with flowcore.

The recommendation that lost was the generic tool, argued on the grounds that it would show the
library's flexibility — which is the dropped teaching goal again, in different clothing. A generic
tool is *less* convincing, not more: nobody looks at a configurable widget and concludes the author
builds good applications.

**On the second submission type.**
The first proposal was two claim sub-types routing to different workflows. The owner rejected it as
"confusing and a weak example" and asked for two genuinely different kinds of submission with
different documents. A **complaint** was proposed; the owner chose **new policy application**:
"more realistic, who really files complaints with workflows, very few I would guess."

**Consequence.**
Claims carry the complex workflow, applications the simple one. The seeded data lives in the
database rather than in Go types, so the application's own schema is real.

Each seed is a workflow, a drafted submission, and **no workflow instance** — the owner's detail, and
the sharpest one. A visitor's first action is submitting a draft, so they see the beginning rather
than arriving mid-story, and everything after is their own doing.

---

## 13. The API speaks claims, not workflows

**Context.**
Where the domain logic lives once a browser is involved. This is the way a React rewrite most often
goes wrong: business rules leak forward into the front end, one convenient call at a time.

**Options.**
Generic REST — `/api/workflows`, `/api/runs/{id}`, `/api/visits/{id}` — with React composing them.
Endpoints shaped to screens, speaking the application's own vocabulary.

**Decision.**
The API speaks claims and applications. One request per screen, returning a composite: the case, its
type-specific detail, its current step with the actions available, and its history. React never
learns FlowCore's model.

The workflow editor is the single carved-out exception. Its endpoints mirror `Catalog` closely,
because that screen genuinely *is* about definitions, steps and actions, and it is the one place the
library's vocabulary belongs on screen.

**Why.**
React renders; it does not orchestrate. There is no "fetch the state, then fetch the actions, then
work out which are available" — that composition happens in Go, where a UI taking a shortcut cannot
bypass it.

`visitId` passes through the browser opaquely. The front end does not know what a visit is; it
returns the value unchanged. That is what preserves FlowCore's stale-view protection without the
client having to understand why it exists.

`internal/app` survives nearly whole. It already assembles exactly this shape — subject from the
client's own store, state from the library, identity resolved, errors translated. Today it hands
that to a template; afterwards it marshals it to JSON. Workflow selection, agent dispatch and error
translation all stay in Go.

And goal 3 says the user should be looking at *claims*. Generic REST would drag the library's
vocabulary into the front end, which is the opposite.

**Consequence.**
Endpoints are shaped to screens rather than to resources, so `/api/cases/{reference}` returns a
composite that is not a table row, and a purist would call it un-RESTful. For an application with
seven known screens, designing generic resources buys nothing and costs round trips. Accepted.

---

## 14. An auto-laid-out canvas, with no stored positions

**Context.**
The workflow editor becomes a React Flow canvas, which decision 2 had priced at roughly 2x under
HTMX and declined. Under React the canvas is a library; what costs is reconciling graph edits back
into `Catalog` calls, and that cost exists with or without a canvas.

**The problem underneath it: FlowCore stores no coordinates.**
A definition is steps and routing. It has no idea where anything sits on a page.

**Options.**
Free positioning, with the client storing `(definition_id, step_id, x, y)` in its own schema.
Automatic layout computed from the graph, storing nothing.

**Decision.**
Automatic layout. Nodes cannot be dragged; edges are dragged between them to create actions, and a
node is clicked to edit it.

**Why.**
Stored positions mean orphan rows when a step is deleted and missing positions when one is added
through any other path — a permanent maintenance tax on something that is not the point.

The diagram stays readable, which is goal 3's actual requirement. Hand-arranged graphs are tidy for
about a week.

And the read-only workflow view and the editor become one component in two modes, which is the
split decision 16 records, implemented once.

**Consequence.**
An awkward edge crossing cannot be nudged, and a layout engine occasionally makes a choice a person
would not. Tolerable at six to ten steps; worse as graphs grow.

---

## 15. Per-visitor copies, with a shared cast

**Context.**
Two agreed things collide. Seeds live in the database, which implies one dataset; hosting implies
concurrent visitors. Together: two people sign in as Dana, open the same claim, and one settles it
while the other is reading it.

The previous client solved this with per-session isolation seeded through the API. Seeding in the
database changes the shape of the problem.

**Options.**
A single shared dataset with a visible reset.
Per-visitor copies of the seeded data.

**Decision.**
Per-visitor copies. On first arrival the client copies a template dataset into rows tagged with that
visitor's session: the two workflows through `Catalog.Create`, the two drafted submissions, and their
documents. The cast of users stays **shared and read-only**.

**Why.**
A shared dataset breaks the moment two people use it at once, and the point of this rebuild is that
the application should not feel like a demonstration.

Sign-in still means something: the roster is fixed, and what belongs to a visitor is the work rather
than the people. There is one Dana Whitfield.

It is the same principle the previous client used — the client owns tenancy because FlowCore has
none — expressed in rows instead of in memory. `CLAUDE.md` names `tenant_id` as its canonical example
of structure with no caller, and this is the caller arriving and still not needing it.

**Consequence.**
Seeding becomes a real operation rather than two `Create` calls, and every query grows a session
filter — tenancy discipline that has to be kept honest. The existing session janitor grows to delete
copied rows.

---

## 16. One active workflow per submission type, and read/edit split apart

**Context.**
Goal 3 asks for two things that pull in different directions: configure a workflow easily, and
understand one easily. And with two submission types mapping to two workflows, "specify when a
workflow applies" is otherwise trivial enough to be uninteresting.

**Decision.**
Each submission type points at one **active** workflow. Others are retired rather than deleted.
Configuring when a workflow applies is: build it, then activate it for a type.

Understanding and editing are **separate screens** — a read-only workflow view, and an editor.

**Why the active pointer earns its place.**
Activating a new workflow demonstrates something real: cases already running keep the workflow they
started under, while new submissions get the new one. Two cases can sit side by side following
different processes because one was submitted before the switch.

That is FlowCore's snapshot invariant — one of the library's two founding principles — shown by
using the application rather than explained in a footnote, and it costs nothing, because the library
already guarantees it. The client only has to make the pointer changeable, and must never rewrite it
on an existing submission.

**Why the screens split.**
Understanding wants one large uncluttered diagram; editing wants forms, dropdowns and destructive
buttons. Putting both on one page is what produced the screen the owner called unsalvageable, and
most visits to a workflow are to look at it rather than change it.

**Consequence.**
A copy action, an active flag, and a list showing which is live. A retired workflow with no runs left
simply sits there; nothing collects it.

---

## 17. The test for keeping a step, and the two it cut

**Context.**
The claim workflow was proposed with nine steps. The owner asked for it to be simplified, and gave
the test rather than the answer: "Remove cases where not much is demonstrated... What does 'legal
review' and 'senior adjuster' demonstrate for the workflow?"

**Decision.**
Seven steps. `legal review` and `senior adjuster` are cut.

**Why.**
Audited against the test, every other step demonstrates something structural that no other step does:
an AI entry step that branches; one side of the branch with a cross-over out; an AI step whose
failure loops back for more input; the loop target, where an AI step is re-run on revised input; an
AI step that diverts to a specialist; the main human decision with a cross-over back; and a side
branch that rejoins the main path or terminates.

The two that were cut are both "another human approves," which `adjuster review` already shows. They
added a node to the diagram and a group to the cast and demonstrated nothing new.

Escalation is not lost with them: `fraud referral` demonstrates it, and more interestingly than a
more senior person looking at the same thing.

**Consequence.**
The cast loses `group:legal` and `group:senior-adjusters`.

The new policy application workflow keeps its senior underwriter and stays at four steps. Its job is
to be the simple one, and a plain referral is worth showing once — just not twice.

The test itself is the durable part, and is worth applying to any step, screen or field proposed
later: what does this demonstrate that something else does not already?

---

## 18. Sign-in with seeded accounts, rather than a switcher

**Context.**
Identity is faked. How it is faked is the first thing a visitor sees, and the previous client put a
dropdown in the page header — unmistakably a demonstration affordance.

**Decision.**
A sign-in screen listing the seeded cast. Click a person, no password, a line saying these are demo
accounts. Switching stays available from the account menu, where a real application puts it.

**Why.**
The shape is what makes an application read as software: every internal console starts at a sign-in.
It is also honest — it does not pretend to authenticate.

It fixes a real problem as well. Switching identity from the page header changed who you were while
you were looking at someone else's work, which was quietly confusing.

And it gives the application one place to explain itself — what this is, that it is built on
FlowCore, a link to the repository — reaching every visitor without putting a word of it on a
working screen. That is where the material deleted from every page actually belongs.

**Consequence.**
One extra click before anything is visible, and one more screen.

---

## 19. Documents are text records, not files

**Context.**
The claim workflow's first AI step judges whether the documents on file support the claim, so this
decides both what the claim screen shows and what a model actually has to work with.

**Decision.**
A document is a row: name, kind, date received, and an optional body of text. No upload, no binary
storage.

**Why.**
The documents that matter are prose. A police report and a repair estimate are what `narrative
consistency` compares against the claimant's own account, so the text *is* the document. A photograph
is a row with a name, a date and no body.

It also makes the `awaiting documents` loop real rather than mimed: someone adds a document record
and resubmits, and the AI step re-runs against a genuinely different file.

**Consequence.**
The claim file reads as a list of records rather than a folder of evidence. Photographs were already
out of scope, so this is the honest version of that decision rather than a new concession.
