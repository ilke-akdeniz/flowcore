# Iteration 2 — AI Review Steps

## What this file is

Iteration 2's working file.
It began as source material gathered before design started.
The design is now settled, and what remains here is the plan and its progress, a scenario to test the build against, and reference material.

**The settled design lives in `docs/decisions.md` (39 to 43) and `docs/system-design.md`, not here.**
This file points at them rather than restating them, so each fact has one home.

Claims that remain are tagged so their weight is visible:

- **[repo]** — recorded in the repository's docs or code.
- **[agreed]** — settled with the owner and binding on this iteration.
- **[idea]** — unratified, offered as a starting point to argue with.

The `[owner]` tag is retired.
Everything it marked was either settled into a decision or rejected, and both outcomes are in `decisions.md`.

## The plan — agreed 2026-09-20

**[agreed]** This is the agreed route through iteration 2.
Follow it in order.
If the work suggests a different sequence, or a phase turns out to be unnecessary, stop and say so — drifting from it silently is the failure this section exists to prevent.

### Phase 0 — Frame the slice — **done 2026-09-20**

1. **Grill advisory versus deciding**, following *Grilling* in `CLAUDE.md`.
   That question alone, before anything else.
   It is the decision everything else reorganizes around, and the repository currently holds two incompatible answers.
2. **Grill the scope boundary.**
   What is in iteration 2 and what defers to iteration 3: the comment field, the trigger mechanism, the worklist, failure and retry.
   Apply the no-speculative-structure test to each one individually.
3. **Write `# Iteration 2 Scope` into `system-design.md`**, mirroring the iteration 1 section — flows in scope, everything else explicitly out.
   Move iteration 2 to *In progress* in `status.md`; that is the owner's call.

### Phase 1 — Decisions, one at a time, design doc first — **done 2026-09-20**

Each decision lands in `system-design.md` (what it is), then `decisions.md` from 39 (why, and the options rejected).
Don't introduce ADR files — `decisions.md` is already the project's format, and a second one would split the authority.

4. **The step shape** — advisory or deciding.
   A model change, so the owner settles it.
5. **The comment field** — shape, mutability, one note or many.
6. **Trigger and pickup** — poll, client-emit, or explicitly deferred with a note in this file rather than built.
7. **Failure and retry semantics** — the first slice that has to answer this, because the executor is fallible.

### Phase 2 — Build, in iteration 1's order — **next**

Iteration 1's commit sequence is the template; reuse it rather than inventing one.

8. Migration — whatever schema the decisions require.
9. Go types and params.
10. Store helpers, and the errors they need.
11. Engine methods.
12. Tests against real Postgres at each layer, not batched at the end.

Small reviewable commits, with `/code-review` before each.

### Phase 3 — Close out

13. Refresh `code-map.md`, the derived view of package and struct-level structure.
14. Propose *Complete*; the owner decides.

### One dependency to respect — resolved

**Step 4 gated step 5, and it held.**
The step shape was settled first (decision 39), which is what made the remark's shape decidable: because the agent completes its own visit, its rationale goes in that visit's own completion stamp, and no write outside completion is needed.
Had the advisory shape won, a remark writable only at completion could not have held an executor's findings, and the field would have needed a different design.

## Where iteration 2 starts from

**[repo]** `system-design.md` now carries a `# Iteration 2 Scope` section, which is the authoritative statement of what is in and out.

**[repo]** The increment 2 candidate note that preceded it — "advisory rather than deciding" — is **superseded by decision 39**, which found that advisory is not a step type but graph topology.
The note is left in place rather than deleted, and the scope section records the supersession.

**[repo]** The Engine's surface today is `Start`, `CompleteStep`, `GetState`, `GetHistory`.
Iteration 2 adds the assignee-keyed worklist and reassignment.

**[repo]** `SubjectVersionToken` is recorded and stamped, never interpreted or validated by the library; the client compares it.

**[repo]** `docs/decisions.md` now ends at decision 43, so iteration 3's decisions continue from 44.

## What was settled

Phase 0 and Phase 1 were resolved by interview on 2026-09-20.
The reasoning, the rejected alternatives, and the owner's own words are recorded as decisions 39 to 43 — this table is an index, not a summary.

| Question | Resolution | Decision |
| --- | --- | --- |
| Advisory or deciding? | A false dichotomy. The agent is an ordinary actor that completes its own visit; advisory versus deciding is per-definition graph topology. | 39 |
| Can an agent annotate a human's open visit instead? | No — rejected on ordering races, invisible failure, no row, and nothing recorded on a loop. | 39 |
| Parallel steps and joins? | Sequential only, recorded as a deliberate non-capability with a migration path sketched. | 40 |
| Where do findings and rationale live? | A `remark` on the visit: optional, part of the atomic completion stamp, immutable in effect, capped at 3000 characters. | 41 |
| Does FlowCore hold prompts or call the model? | Never. It holds only a subject reference, so it cannot build a prompt without storing subjects or calling back into the client. | 43 |
| How is agent work triggered? | It is not — the client already holds `CurrentStep.AssigneeID` in the response that opened the step. The problem dissolved. | 43 |
| Agent retry and failure policy? | The client's own job queue. FlowCore grows no retry, backoff, or timeout semantics. | 43 |
| Worklist? | In scope, assignee-keyed half only. | 42 |
| Reassignment? | In scope. The worklist is what gives it a caller. | 42 |

**What iteration 2 builds:** the worklist query and method, reassignment, and the `remark` column — and no step-type structure at all, because an agent was always expressible as an opaque assignee.

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

**[agreed]** Agent-platform cookbooks are a source of ideas, not a specification.
Do not design FlowCore around one example integration.
Evolve it toward the shape that serves the most integration shapes.

**[agreed]** Workflow-shaped behaviour should not live outside FlowCore and integrate back in.
That is asking for trouble.

**[repo]** No speculative structure still applies, and it applied during Phase 1.
A capability earns its place this slice only if it is the correctness condition of something being built now.
The test was run on each candidate individually: the trigger mechanism failed it and dissolved entirely (decision 43), agent retry policy failed it and stayed with the client (decision 43), and `step_visit.step_definition_id` with its two indexes failed it and stays deferred (decision 42).
The worklist, reassignment, and the remark passed, each on a caller that exists today.
Apply the same test in Phase 2 to anything the build suggests adding.

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
