# Reference Client

## What this file is

The plan for FlowCore's reference client, settled by interview on 2026-09-21.

It is called the *client*, not the *demo*, deliberately.
`system-design.md`'s Actors section already uses "Client" for code that calls the library, and naming it that says what it is for: something to run, read, lift snippets from, or fork as the starting point for a real application.
"Demo" would undersell that.
The risk is that `something/client` reads as an SDK in Go, so prose should say **reference client** — an application built on FlowCore, not a wrapper for calling it.

**The plan below is marked [agreed] and is binding.**
Everything else here is context for it.

The client's own design decisions live in `client/docs/decisions.md`, not in the library's log — see the
plan's phase 0.
Anything building the client reveals *about the library* is a FlowCore decision and goes in
`docs/decisions.md` from 45, which is the one exception.

## Why the client exists at all

**[agreed]** The owner's argument, which is the reason this is worth building:

> The benefits of having a separate workflow library that defers many things to the client but does
> some things very well is hard to understand without a realistic client.

A boundary is invisible from one side.
Every central claim of this project — opaque references, the library never calling a model, the
client owning dispatch, not being a document store — is unreadable from the API alone.
Each becomes legible only when a client is shown doing the other half.

So the client is documentation of the boundary, not a marketing artifact.
That is the test to apply when scoping any part of it: does this make the seam visible?

Three goals, in the owner's priority order: a personal technical exploration first, a published
example project second, a portfolio piece third.

## The plan — agreed 2026-09-21

**[agreed]** Follow it in order.
If the work suggests a different sequence, or a phase turns out to be unnecessary, stop and say so —
drifting silently is the failure this section exists to prevent.

### Phase 0 — Frame

1. Create the `client/` module: its own `go.mod`, with `replace github.com/mike-akdeniz/flowcore => ../`
   so it always builds against the working tree.
2. Create `client/docs/decisions.md` and record what this interview settled, numbered from 1.
3. Add the subject to `docs/status.md`.
   Recommended name: **Reference client**, not "Iteration 3" — iterations 1 and 2 were library
   slices and this is not one, so the iteration sequence stays available for the next one.
   The owner's call.
4. Add one line to `CLAUDE.md` recording that the library's stack constraints and "not an
   application" rule apply to the root module, and `client/` is governed separately.

### Phase 1 — Walking skeleton

5. `docker compose` bringing up Postgres alongside the client, so a clone is one command.
6. Session cookie, and a session store mapping a session to the definition ids it owns.
7. **Seed per session on first request, never at startup.**
   This is the detail that makes local and hosted use the same code path; starting with
   startup-seeding means untangling it later.
8. One route that renders where a seeded release workflow stands.
   End to end, ugly, no styling — the point is that every layer is connected.

### Phase 2 — Runtime

9. Runtime screens: current step, available actions, complete with a remark.
10. User switcher — fake identities, no auth. It resolves a session into an opaque reference plus
    group memberships, which is the identity deferral made literal.
11. Worklist, filtered to the session's own definition ids via `AssignedStep.WorkflowDefinitionID`.
12. Reassign.

### Phase 3 — Agent steps

13. An internal queue and a worker goroutine: case 4 dispatch, per decision 43.
    The web request returns as soon as the run reaches an agent step; the worker completes it.
14. A `Checker` interface with two implementations, selected by whether `ANTHROPIC_API_KEY` is set.
    Canned findings are authored against the seed data so a visitor with no key sees the whole thing working.
15. The two release-scenario agent steps: risk classification from a diff, and changelog
    verification against it.

### Phase 4 — Config UI

16. Forms over the whole `Catalog` surface — definition, statuses, steps, actions, entry step.
17. Live mermaid graph, regenerated from the definition as it is edited.
18. Error translation: FlowCore's typed errors rendered as sentences a person can act on.
    This is where API friction will surface, because it is the first thing to use the whole Catalog
    API in anger. Record what it finds.

### Phase 5 — Second scenario

19. The insurance claim: definition, subject type, prompts, canned responses.
    **Text-only** — no photographs. Narrative-versus-police-report inconsistency is a legitimate text
    judgment, and images would add upload, storage, binary assets and vision calls for no gain here.

### Phase 6 — Close out

20. The call log panel, showing the client's own work beside each `flowcore` call.
21. A styling pass, via a CDN stylesheet. No build step.
22. `client/README.md`, stating the HTMX choice and its reason.
23. Update the library `README.md`: it still says "Early development, iteration 1", its first code
    block does not compile since decision 44, and it should link to the client.
24. Propose *Complete*; the owner decides.

### Deployment

Hosted is the intent, but not a prerequisite for any phase above.
`CLIENT_SESSION_TTL` defaults to `0`, meaning sessions never expire and the janitor does not start;
it is set to something like `24h` when deployed publicly.
Defaulting to never-expire fails in the safe direction — a forgotten setting grows the database
slowly, where the reverse loses a local user's work.

## The scenarios

**[agreed]** Two, both text-subject and text-model-call so the second costs about a day rather than
a week.

**Software release approval** — the default view, since the likeliest visitor ships software.

```
automated risk analysis   agent:diff-risk@v1    low → changelog check
                                                high → security review
changelog check           agent:changelog@v1    accurate → QA sign-off
                                                mismatch → author revision
security review           group:security        clear → QA sign-off
                                                needs legal → legal review
                                                reject → rejected
QA sign-off               group:qa              pass → approved
                                                fail → author revision
legal review              group:legal           clear → QA sign-off
                                                reject → rejected
author revision           user:submitter        resubmit → automated risk analysis
```

The subject is a git ref and a diff, which nobody expects a workflow engine to hold — the
not-a-document-store boundary needs no explaining.
Pushing a new commit changes the subject version token and loops the run back through both agent
steps, which is the revision story made concrete.

**Insurance claim** — completeness check and narrative consistency as agent steps; adjusters, senior
adjusters, the fraud unit and legal as groups; escalation by amount and by flagged inconsistency.

Two agent steps per scenario, and they must be *different kinds* of judgment rather than two
flavours of "check text against guidelines".

## Session scoping

**[agreed]** Needed because hosting means concurrent visitors sharing one database, and a reset
button does not solve that — it only undoes damage while causing more.

It needs no library change, and one part of that is a happy accident:

- **Definitions** — `Catalog` has no `List` method, only `Get(id)`, so the client must track the ids it
  created anyway. Keyed by session, "your workflows" is naturally only yours.
- **Subjects** — the client's own store, which FlowCore never sees.
- **Runs** — keyed by `{subjectReference, definitionID}`, and the client prefixes the subject with the
  session id, so two visitors on the same seeded workflow have different runs.
- **The worklist** — `ListAssignedSteps` returns rows across sessions, and the client filters by
  `AssignedStep.WorkflowDefinitionID` against the session's own set.
  No prefixing of assignee strings, so a visitor who types `group:security` gets exactly that stored.

**It is also worth more than it costs.**
`CLAUDE.md` names `tenant_id` as the canonical deferred structure with no caller.
A client doing per-session isolation entirely in client code, over a library with no tenant column, is
that argument demonstrated rather than asserted.

## Deliberately out of scope

- Real authentication, user management, password anything. A fake switcher shows the boundary; real
  auth shows nothing about FlowCore and is the largest chunk of code available.
- A drag-and-drop canvas builder. Estimated at roughly 2x the HTMX build rather than the 30% the
  owner was willing to pay, and it is more impressive to watch than to use for a six-step workflow.
  Not foreclosed: the `Catalog` API is the same either way, so a canvas can be added later as a
  second front end without wasting the form work.
- Photographs in the insurance scenario.
- A JSON API. The services will be there if one is ever wanted; it is a second presentation layer,
  not a rewrite.
