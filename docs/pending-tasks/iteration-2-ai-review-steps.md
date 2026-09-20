# Iteration 2 — AI Review Steps

## What this file is

Source material for iteration 2, gathered in conversation on 2026-09-20, before any design work started.

**Nothing here is ratified.**
Design decisions land in `docs/system-design.md` and `docs/decisions.md` first, and the owner resolves them.
Claims are tagged so their weight is visible:

- **[repo]** — already recorded in the repository's docs or code.
- **[owner]** — a position the owner stated in that conversation; not yet written into the design docs.
- **[idea]** — an unratified suggestion from the conversation, offered as a starting point to argue with.

## Where iteration 2 starts from

**[repo]** `system-design.md` opens by saying the library is "designed to support human-in-the-loop AI review steps (iteration 2)", and iteration 1 explicitly excludes them, along with synchronization, failure handling, and scale.

**[repo]** `system-design.md` already carries an increment 2 candidate, quoted in full because it is the closest thing to a prior decision:

> automated advisory step type (findings + human override + audit)
> a step type exists whose executor is external, async, fallible, and advisory rather than deciding.
> flow idea: "AI director pre-check" -> finds 7 issues and shows warnings -> salesperson reviews the issues and makes changes or overrides the warnings...

**[repo]** The Engine's surface today is `Start`, `CompleteStep`, `GetState`, `GetHistory`.
The worklist flow (`Get Assigned Steps`) is described in the design doc's flows but has no method — the data to serve it exists as `assignee_id` on `step_visit`.

**[repo]** `SubjectVersionToken` is recorded and stamped, never interpreted or validated by the library; the client compares it.

**[repo]** `docs/decisions.md` ends at decision 38, so iteration 2's decisions continue from 39.

**Failure handling moves into play.** The increment 2 note calls the executor *fallible*, and iteration 1 put failure handling out of scope.
Whatever shape the step takes, iteration 2 is the first slice that has to say what happens when the thing completing a visit crashes, times out, or never answers.

## The central open question: advisory or deciding?

Two shapes were discussed, and they are genuinely different designs.
This is the first thing to settle, because most of the other questions resolve differently under each.

**Shape A — advisory. [repo]** The step's executor produces findings and does not choose the route.
A human sees the findings and decides, possibly overriding them.
This is what the increment 2 candidate note describes, and it is the older, more considered position.

**Shape B — deciding. [idea]** The step is an ordinary step whose `assignee_id` is an opaque agent identifier such as `agent:brand-check@v3`.
A runner outside the library notices the open visit, calls the model, and calls `CompleteStep` with `completedBy: "agent:brand-check@v3"` and a selected action.
The Engine never awaits a model, holds no prompt, and stores no model output beyond the selected action.

The two are not exclusive — a confidence threshold could route between them (see below) — but the default shape should be chosen deliberately rather than falling out of the implementation.

Under Shape A the open questions become: does the advisory executor's run occupy a visit at all, or does it attach to the human's visit?
If it attaches, what writes it, and when?

## Why the integration may need no new mechanism

**[idea]** The reason an agent integrates cleanly is structural, not lucky.
`assignee_id` and `completed_by` are opaque by principle — the library already cannot tell an agent from a person and was never permitted to try.
So `agent:brand-check@v3` needs no new column, no agent step type, no flag.
An agent is an actor because every actor is a string the library refuses to interpret.

Two consequences worth testing against the real design:

**Confidence routing is already expressible. [idea]** "Pass if confident, otherwise send it to a human" is two actions on one step pointing at different next steps.
The executor picks the edge.
No new structure.
This is also the natural bridge between Shape A and Shape B.

**Retry safety may already exist. [idea]** An at-least-once runner that crashes after completing but before acknowledging will retry, and the partial unique index over open step visits rejects the duplicate in the database.
The mechanism built for two humans double-clicking would cover flaky agent infrastructure for free.
Worth verifying against the actual index before relying on it.

## Open design questions

**1. Nothing notices.**
FlowCore has no outbound anything — no events, no webhooks, no queue.
Something must observe that a visit assigned to an agent is open.
Options raised: the runner polls, or the client emits on `CompleteStep`.
The worklist query would make polling cheap, and does not exist yet.
By the project's own no-speculative-structure rule, none of this should be built until iteration 2 actually forces it — but iteration 2 probably does force it, which is what makes it a real question now rather than a deferred one.

**2. Failure handling.**
The executor is fallible.
A human who cannot decide simply leaves the visit open, and that is fine.
An agent that errors also leaves it open, and something must decide whether to retry, escalate to a human, or surface a stuck run.
Note that "leave it open" is already a valid state — the question is who notices and what they do.

**3. Where the model's output lives.**
FlowCore is not a document store, and the agent's full report — findings, confidence, model version, bounding boxes — is bulk output that does not belong in it.
But the *reason for a decision* is different from a *report about the subject*.
See the comment field below; the line proposed was **the reason comes in, the report stays out**.

**4. The visit comment field.**
See the next section.
This is arguably a prerequisite: **[owner]** most integrations will be an agent doing work and then posting results to FlowCore along with a comment.

**5. The worklist.**
Designed in the flows, unbuilt.
Both the agent-pickup question and the human-queue question run through it.

## The visit comment field

**[owner]** A free-text comment or note on `StepVisit` is needed regardless of agents.
"I approved this, but…" and "I approved this because…" are ordinary workflow-engine concerns with a caller today and no agent anywhere in sight.

**[owner]** It does not violate the not-a-document-store boundary.
That boundary refuses to hold the *workflow subject* — the artwork, the expense form.
A rationale for a decision FlowCore itself owns is an attribute of that decision, sitting beside `completed_by` and `selectedAction` with the same rhythm: written once at completion, never rewritten.

**[owner]** It also removes a failure mode rather than mitigating one.
If the decision and its reason are stamped in the same transaction, there is no write-ordering convention to get right, no retention mismatch between two systems, and no orphaned decision whose rationale was lost.
Keeping decision data outside the engine because the engine lacks somewhere to put it manufactures a second source of truth for something FlowCore already owns.

**[owner]** A workflow-shaped thing living outside FlowCore and integrating back — a person approving and annotating in another system — should not happen.
If it does happen for a good reason, whoever designs that has to decide what to do with FlowCore's note field and who is the source of truth for what.

Open questions for that design conversation **[idea]**:

- **Does it join the atomic completion stamp?**
  Probably as an optional member: completion without a note is allowed, a note without a completion is not — so `completed_at` / `completed_by` / `selectedAction` / comment stay one indivisible write and a half-stamped visit remains unrepresentable.
- **Is it immutable?**
  The same reasoning that makes `completed_by` immutable applies.
  Clients will ask to edit it; the answer being "no" is what makes it evidentiary.
- **One note, or many?**
  One note at completion is the line that stops this becoming a comment system.
  Threading and attachments are the slope to avoid.

## Positioning: what iteration 2 is not

Useful for keeping scope honest when agent-platform material suggests features.

**The distinction. [idea]** Agent-platform recipes focus on the quality, cost and safety of a single unit of model judgment — tool access, retrieval, context management, evaluation, budgets, observability.
State exists there to serve the turn, not to outlive it.
FlowCore routes decisions reliably, holds the invariants of configurations and instances, protects instances against concurrency and durability failures, and provides auditability.
Compactly: **the platform decides, FlowCore remembers** — the platform is about what happens inside a decision, FlowCore about what happens between decisions.

**The axis is where the pause lives. [idea]** What makes an approval gate in an agent framework *not* a workflow is not that it has one step.
A single-step FlowCore workflow is still a workflow, and a ten-step agent pipeline with an approval is still not one.
The difference is that in an agent framework a process is *running* and stops to ask — the stack is warm, the human is an interrupt handler, and killing the process evaporates the pending approval because it was only ever a suspended continuation.
In FlowCore nothing is running; the state is at rest in a row, and work resumes because someone queries and acts.

A reasonable name for the agent-framework pattern is a **supervised turn**: one agent turn placed under human confirmation.

**The actor inverts. [idea]** In a supervised turn the agent does the work and the human checks it.
In FlowCore the humans do the work and the system routes it between them.
An AI review step flips the actor into the supervised-turn shape while keeping FlowCore's spine — which is why it can be one step type rather than a new mechanism.

## Worked example: campaign asset approval

**[idea]** A scenario to test iteration 2's design against.
It exercises loops, reassignment, subject versioning, escalation, and an agent step in one run.

A consumer brand.
An agency submits a campaign key visual.
It must clear an automated brand check (logo clearspace, palette, mandatory disclaimers), a brand manager, and — if it carries a health or nutrition claim — legal.
Any reviewer can send it back to the agency, which resubmits a new version.

| Step | Assignee | Actions → destination |
| --- | --- | --- |
| automated brand check | `agent:brand-check@v3` | `pass` → brand manager review · `fail` → agency revision |
| brand manager review | `group:brand-managers` | `approve` → **approved** · `needs legal` → legal review · `request changes` → agency revision |
| legal review | `group:legal` | `clear` → **approved** · `reject` → **rejected** · `request changes` → agency revision |
| agency revision | `user:agency-lead` | `resubmit` → automated brand check |

The last row is a cycle, deliberately.

### The nine-day timeline

Each event exists to stress one property.

- **Day 1, 09:00** — Agency submits v1.
  `Start` writes the workflow, the whole snapshot, and the first visit on *automated brand check*.
- **Day 1, 09:01** — The agent selects `pass`.
  Visit closed with `completed_by: agent:brand-check@v3`.
  New visit opens on *brand manager review*.
- **Day 2, 11:00** — The client deploys a routine release; every process restarts.
  Nothing happens to the run, because there was never a process — only a row with `completed_at IS NULL`.
- **Day 3, 09:00** — The brand manager is on leave; the open visit is reassigned.
  The closed visit from day 1 is untouched, so history still says the agent did the brand check.
- **Day 3, 14:00** — `request changes` → *agency revision*.
- **Day 4, 10:00** — Agency uploads v2 and selects `resubmit` → back to *automated brand check*.
  That is a second visit row on the same step, and later a second one on brand manager review.
  Both preserved, both addressable, each with its own assignee, timestamp and version token.
- **Day 5** — Someone adds a fifth step to the *definition* while ~200 assets are in flight.
  All of them finish under the rules they started with, structurally, because the snapshot is taken eagerly and whole at start.
- **Day 6, 15:00** — `needs legal` → *legal review*.
- **Day 7, 09:00 / 11:00 / 16:00** — A lawyer opens v2; the agency swaps in v3; the lawyer clears it, sending `subjectVersionToken: "v2"`.
  FlowCore stamps v2 and does not block — the client compares and decides.
  The evidence that the decision was made against superseded artwork is permanent.
- **Day 7, 16:00:02** — The brand manager, on a page rendered before the escalation, clicks `approve` with a stale `visitId`.
  Rejected by the partial unique index, not by an application check.
- **Day 9** — A regulator asks who approved this, against which version, in what order, and whether it was re-reviewed after the change.
  `GetHistory` answers it, because the audit trail is the state.

### The properties this scenario is meant to prove

An agent framework can produce the judgment; these are what the engine contributes.

1. A pending decision exists as a **record rather than a suspended process**, surviving deploys, crashes, and a nine-day wait.
2. Open work is **queryable across runs** by assignee.
3. An open decision can be **reassigned** without rewriting the decision already made.
4. A loop produces **two addressable review events**, not one run with a retry counter.
5. In-flight runs are **immune to definition edits**, structurally.
6. Each decision carries **which subject version it was made against**.
7. Concurrent completion is resolved **in the database**.
8. **State and audit are the same row in the same transaction**, so they cannot drift apart.

## Guardrails carried into iteration 2

**[owner]** Agent-platform cookbooks are a source of ideas, not a specification.
Do not design FlowCore around one example integration.
Evolve it toward the shape that serves the most integration shapes.

**[owner]** Workflow-shaped behaviour should not live outside FlowCore and integrate back in.
That is asking for trouble.

**[repo]** No speculative structure still applies.
A capability earns its place this slice only if it is the correctness condition of something being built now.
The trigger mechanism, the worklist, and the comment field each need that test applied individually — some will pass it for iteration 2, and the ones that do not should be deferred with a note here rather than built.

## Still to do before design starts

- Settle advisory versus deciding.
- Decide whether the comment field is part of iteration 2 or lands ahead of it as its own slice.
- Apply the no-speculative-structure test to the trigger mechanism and the worklist.
- Write the resulting decisions into `system-design.md`, then `decisions.md` from 39.

## References — cookbook examples we looked at

Index: <https://platform.claude.com/cookbook/>

Only the index page was read in that conversation, not the individual notebooks — titles and one-line summaries come from the index, so treat the descriptions below as signposts rather than accurate accounts of what each recipe does.

**Closest to what iteration 2 has to decide**

- [Advisor pattern](https://platform.claude.com/cookbook/managed-agents-cma-consult-an-advisor) — a mid-tier working model consults a stronger model mid-turn.
  The nearest published analogue to Shape A: a second opinion that informs a decision without making it.
- [SRE incident responder](https://platform.claude.com/cookbook/managed-agents-sre-incident-responder) — an agent diagnoses, opens a fix PR, and waits for approval before merging.
  The canonical supervised turn, and the clearest example of an approval that lives in a running process rather than a record.
- [Outcomes: agents that verify their own work](https://platform.claude.com/cookbook/managed-agents-cma-verify-with-outcome-grader) — a grade-and-revise loop against a rubric.
  Relevant to whether a failed advisory check loops or escalates.
- [Content policy enforcement](https://platform.claude.com/cookbook/capabilities-content-moderation-guide) — compiles written policy into deterministic JSON and produces auditable verdicts from a rule engine that never calls the model.
  The closest philosophical match to keeping the model out of the transition path.

**Audit, state and versioning**

- [Fraud Review Agent](https://platform.claude.com/cookbook/managed-agents-cma-with-mongodb-atlas) — records decisions and an append-only audit trail in MongoDB.
  Worth reading against the comment-field question: an audit written *by* the process, alongside its state, rather than being its state.
- [Prompt versioning and rollback](https://platform.claude.com/cookbook/managed-agents-cma-prompt-versioning-and-rollback) — server-side prompt versions with regression detection and rollback to a pinned version.
  The nearest thing on the platform to snapshotting; it pins the model's instructions, not the shape of a process.
- [Build agents that remember your users](https://platform.claude.com/cookbook/managed-agents-cma-remember-user-preferences) — a memory store that persists preferences across interactions.

**Orchestration — the contrast case, not a model to copy**

- [Orchestrate subagents at scale with dynamic workflows](https://platform.claude.com/cookbook/claude-agent-sdk-08-dynamic-workflows)
- [Coordinator pattern: plan big, execute small](https://platform.claude.com/cookbook/managed-agents-cma-plan-big-execute-small)
- [Multiagent: coordinate a specialist team](https://platform.claude.com/cookbook/managed-agents-cma-coordinate-specialist-team)

These are model-driven routing decided at runtime, which is the opposite property from a declared graph with snapshotted edges.
They share the word "workflow" and almost nothing else.

**Operational, if a runner gets built**

- [Budgets: cap what a session can spend](https://platform.claude.com/cookbook/managed-agents-cma-cap-session-spend) — an enforced list-cost budget with a `budget_reached` pause.
  A runner that retries a fallible executor needs a spend ceiling somewhere; it is not FlowCore's, but it is someone's.

The index also carries categories not examined here — RAG & Retrieval, Multimodal, Skills, Evals, Observability, Fine-Tuning, Cybersecurity — worth a pass if the brand-check style of evaluation becomes concrete.
