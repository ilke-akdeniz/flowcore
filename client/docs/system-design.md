# Overview

The design for FlowCore's reference client: an internal case console for an insurer, built on the library.

This is the authoritative design document for the client — boundary, actors, flows, state, screens, responsibilities, and invariants.
It changes as decisions land.

The rationale behind individual decisions, and the alternatives they beat, lives in `client/docs/decisions.md`.
This document records what the client is; the decision log records why.

The library has its own pair of documents at `docs/system-design.md` and `docs/decisions.md`.
Nothing here overrides them, and no decision recorded here changes the library.

# Boundary

_What are we building? What are we not building?_

An **internal case console for an insurer**: the screens its own staff use to process work that has arrived.
It handles two kinds of submission — **claims** and **new policy applications** — each moving through a configurable workflow, with some steps decided by people and some by an AI agent.

It is a real application, not a harness.
The previous client existed to make the library's boundary legible, and carried explanatory prose and a call log on every screen; that goal is gone, along with those.
A working application argues for the library by working.

**Three goals, in the owner's order.**

1. A UI portfolio piece: a backend developer working competently with modern front-end tools.
2. A workflow application that shows what is possible with FlowCore, especially its AI steps.
3. An extension of 2: configuring a workflow is part of the product, not a settings page.
   Configure a workflow easily, and understand one easily.

**Out of scope.**

Claimant-facing submission.
Those applications are commodity — a form with uploads — and a processing console is the essential half, working without one.
An _internal_ submission form is in scope, because a handler takes details over the phone or from an email.

Real authentication, user management, and permissions.
The cast is seeded and sign-in is a choice from a list.

File upload and binary storage.
Documents are records carrying text.

Anything that changes the library.
Workflow selection, agent dispatch, tenancy and identity are all the client's, which is how FlowCore is designed.

# Actors

_Who uses this, and what do they do?_

One shared cast for the whole application.
Both workflows are worked by the same organisation, so there is one Dana Whitfield and not one per workflow.

- **Intake** — takes submissions, chases missing documents.
- **Adjusters** — assess and settle claims.
- **SIU** — investigate claims the fraud check flags.
- **Underwriters** and **senior underwriters** — decide new policy applications.

**Agents** are actors too, and that is the whole point of how FlowCore treats them.
An agent is a step whose assignee happens to be `agent:triage` rather than `group:adjusters`.
The library cannot tell the difference and never needs to.

**The visitor** is whoever opened the application.
They sign in as one of the cast, and may switch.

# Flows

_The main journeys._

_Take a submission, and start its workflow_

A handler enters a claim or an application from a phone call or an email.
It exists as a draft until they submit it for assessment.

Submitting is what starts a FlowCore run.
The client looks up which workflow is active for that submission type, and calls `Start` with its definition id.
Triage is therefore two levels, and only the second is a human's:

- The client picks the **workflow**, from the submission's type.
- An AI step inside the workflow picks the **path** — fast-track or full assessment — and the run branches.

A seeded submission arrives as a draft with no run, so a visitor's first action is submitting it and watching the first AI step fire.

_Work my queue_

Sign in, see what is waiting, open it, decide, optionally leave a remark.

The queue is FlowCore's worklist, filtered to this visitor's own data.
The client expands the signed-in person into the references they answer to — themselves plus their groups — because deciding group membership needs an identity model the library deliberately does not have.

_Follow a case_

Where it stands, what has happened, who did what and why.
This includes what the agents found: an agent's finding is the remark on its visit, recorded in the same transaction as its decision.

_Configure a workflow_

Build a workflow, understand one, and choose which is active for a submission type.

Understanding and editing are separate screens, because they want opposite things.
Understanding wants one large, uncluttered diagram; editing wants forms and destructive buttons.

Activating a new workflow affects only submissions made afterwards.
Cases already running keep the workflow they started under, which is FlowCore's snapshot guarantee and needs nothing from the client but a changeable pointer.

# State

_What the client must remember._

FlowCore stores the workflow graph and the record of work performed.
Everything below is the client's own, in the client's own tables, because the library holds no subjects.

_Submission_ — what the queue needs, common to both types

- id
- session id // which visitor's copy this is; see Session scoping
- type // claim | application
- reference // "C-1042", "P-2087"
- status // draft | submitted
- created_at, submitted_at
- flowcore_definition_id // the workflow it was submitted under, nullable while a draft
- subject_reference // what FlowCore was given, derived and stored so lookups need no re-derivation

_Claim detail_ — everything the claim screens show

- submission_id
- policy_number, claimant_name
- amount
- incident_narrative // the claimant's own account, in their words
- occurred_at

_Application detail_ — the other screens

- submission_id
- proposer_name
- cover_type, sum_insured
- disclosures // free text; what the risk screen reads

_Document_

- id, submission_id
- name, kind // police_report | estimate | photograph | correspondence
- received_at
- body // text, nullable — a photograph is a row with no body

Documents carry text rather than files.
The two claim AI steps read them, so the text is the point; a binary would add upload, storage and a media story for nothing.

_User_ — the seeded cast, shared and read-only

- reference // "user:dana", opaque to FlowCore
- name, title
- groups // the references they also answer to

_Workflow registry_ — which workflow is active for which type

- id
- session id
- submission_type
- name
- flowcore_definition_id
- active // one per {session, submission_type}

This table is the entire mechanism behind "specify when a workflow applies".
FlowCore takes a definition id and starts a run; it has no notion of a claim type, and will not acquire one.
The client stores what a graph is _for_.

_Session scoping_

Every row above except `user` carries a session id.

Hosting means concurrent visitors, and without isolation two people signing in as Dana would work the same claim.
On first arrival the client copies a template dataset into rows tagged with that visitor's session: the two workflows through `Catalog.Create`, the two drafted submissions, and their documents.

The cast is shared and read-only.
What belongs to a visitor is the work, not the people.

This is the same principle the previous client used — the client owns tenancy because FlowCore has none — expressed in rows rather than in memory.

# Screens

Three navigation items and one action.

| | |
| --- | --- |
| **My work** | the shared queue; the landing page after sign-in |
| **Cases** | every submission, searchable — for looking something up |
| **Workflows** | the configurations |
| **+ New submission** | a button |

1. **Sign in** — pick from the seeded cast. Also where the application explains what it is and links to the repository, so that material reaches every visitor without appearing on a single working screen.
2. **My work** — what is waiting on this person. Reference, type, step, how long it has waited.
3. **Case detail, claim** — the claim file, its documents, and a panel showing where it stands and what can be done.
4. **Case detail, application** — the same shape, entirely different content.
5. **New submission** — pick a type, fill the form, save as a draft or submit.
6. **Workflow view** — the graph, large and readable. Read only.
7. **Workflow editor** — changing it.

The queue is shared across both submission types, because "what should I do next" does not sort by type.
The detail screens are not shared, because a claim and an application have nothing in common below the header.

Deliberately absent: a dashboard, user administration, settings, and any explanation of the library inside a working screen.

# Responsibilities

_Who owns which decision._

**React** renders and collects input.
It holds no domain rules.
It does not know what a visit is; it returns the value it was given.

**The Go API** composes what a screen needs, in the application's vocabulary.
One request per screen, returning a claim or an application — not a workflow, a run and a visit for the browser to assemble.
The exception is the workflow editor, whose endpoints mirror `Catalog`, because that screen genuinely is about definitions, steps and actions.

**`internal/app`** is the client half of the boundary and survives the rewrite nearly whole.
It resolves identity into references, stores and reads subjects, selects the workflow for a submission, dispatches agent steps onto its own queue, and turns the library's typed errors into sentences.

**FlowCore** stores the workflow graph, routes a run through it, records who decided what and why, and answers what is waiting on a set of references.
It interprets nothing it is given.

# Diagram

```
  Browser (React + Mantine)
      |  JSON, one request per screen, in claims and applications
      v
  Go API  (internal/api)
      |
      v
  internal/app  — identity, subjects, workflow selection,
      |           agent dispatch, error translation, session scoping
      +--> client tables      submissions, details, documents, workflow registry
      |
      +--> FlowCore           Catalog (configure) · Engine (start, complete,
                              worklist, reassign)
      |
      +--> Anthropic API      only from the agent worker, only when a key is set
```

Nothing in the browser talks to FlowCore.
Nothing in FlowCore talks to a model.

# Invariants

_Rules that must not break._

- **A submission belongs to exactly one session**, and no query returns another session's rows.
  Tenancy is the client's, enforced in the client, because the library has no tenant column.
- **A submitted submission has exactly one FlowCore run**, and a draft has none.
  Submitting is the only thing that starts one.
- **A run keeps the workflow it started under.**
  Activating a different workflow changes what future submissions use and reaches nothing already running.
  This is FlowCore's guarantee, not the client's, and the client must not undermine it by rewriting `flowcore_definition_id` on an existing submission.
- **The browser never decides routing.**
  Which actions exist, and where each leads, come from the library through the API.
- **An agent step is an ordinary step.**
  Nothing in the schema, the API, or the library marks one as special; only the client's `agent:` prefix convention decides that its worker picks it up.
- **A remark is written with the decision it explains**, in one call, so a failure cannot separate them.
