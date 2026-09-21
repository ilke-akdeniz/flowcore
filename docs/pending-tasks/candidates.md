# Candidates

**These are open items, not a plan.**
Nothing here has been decided, scoped, or committed to.
It is a collection of things the docs already defer, plus a few that surfaced in conversation and were never written down anywhere — gathered so a future slice starts from a list rather than a re-read of the decision log.

Being on this list means only that someone once had a reason to think about it.
Several of these will never be built, and that is a fine outcome for a candidate.
When a slice is actually chosen, it gets scoped the usual way — grilled, then written into `docs/system-design.md`, then `docs/decisions.md` — and this file is not that.

Ordering within each group is not priority.

## Already recorded as out of scope

These are named in a scope section of `system-design.md`, so they are deferred on the record rather than forgotten.

- **Parallel steps and joins**, and with them N-of-M voting.
  Decision 40 sketches the migration — a branch discriminator, `unique (workflow_id, branch_id) where completed_at is null` — and says what actually breaks: the join insert is the new work, and `CurrentStep` becoming `CurrentSteps` is the API break.
  Deliberately not designed further.
- **`step_visit.step_definition_id` and the two indexes keyed on it.**
  Decision 42 separated this from the assignee half of the worklist and kept it deferred.
  One column, one backfill, three lines of DDL, and the numbers are already measured in decisions 24 and 25 — it needs no re-probing, only a caller.
- **Scale**, in the general sense, deferred since iteration 1.

## Deferred inside a decision, easy to lose

Each of these is a sentence in the middle of an entry about something else.

- **A version column for lost updates.**
  Decision 22 is blunt that no params shape fixes a lost update — "only a version column does, and that stays deferred".
  This is the remaining half of the Synchronization section: the completion path is settled (see below), but two clients editing one definition still clobber each other silently.
- **`last_visit_id` on the workflow.**
  Decision 25 measured bulk history at 157 ms / 353k buffers for the final step of 90k completed runs, and declined to fix it: it is a reporting query, and "where did this run end up" is better answered by the stamped terminal status.
  Recorded as a known cost rather than a bug.
- **Soft delete on the definition side.**
  Decision 24 calls it "available and unbuilt" — the provenance columns it would need already exist, so adopting it later costs nothing extra.
- **Display-label resolution for a deleted definition row.**
  Decision 24 leaves the choice open and suggests the most recent frozen name as the sensible default.

## Surfaced in conversation, never written down

These have no entry anywhere else, which is the main reason to record them here.

- **The README no longer compiles.**
  Line 94 builds a step with `AssigneeID: &managers`, a pointer, which decision 44 removed.
  It also predates `ListAssignedSteps`, `Reassign`, and the remark, so its tour of the API is both wrong and incomplete.
  This is the one item here that is a straightforward defect rather than a design question.
- **Nothing finds a stalled run.**
  A client can ask what is assigned to someone, but not "which runs have sat on a step for more than N days".
  Decision 43 made the worklist a recovery sweeper for exactly this case and then scoped the sweep itself out, so the mechanism exists and the query does not.
  Probably the strongest candidate on this page, because it is the gap the accepted integration shape points directly at.
- **`subject_version_token` is recorded and never compared.**
  The library stamps which revision a decision was made against and leaves the comparison entirely to the client, by design.
  Whether the library should ever offer to make that check — and what it would mean for the opaque-reference principle if it did — has never been discussed.

## Not open, despite looking like it

Recorded so nobody rediscovers these as gaps.

- **Completion-path locking is solved, not deferred.**
  `CLAUDE.md` and several decisions describe a "deferred locking mechanism", which reads like open work.
  It is not: decision 25 establishes that `ux_step_visit_open` *is* the mechanism, probed by forcing two concurrent completions — the loser gets a unique violation and exactly one open row survives.
  No `SELECT FOR UPDATE`, no version column, no serializable isolation, and nothing to build.
- **Agent retry, backoff and failure policy.**
  Decision 43 put these with the client's own job queue, permanently rather than temporarily.
  A workflow library growing its own would be building a worse queue inside itself.
- **The library calling a model, or holding a prompt.**
  Decision 43 rejected this as structurally impossible, not merely undesirable: FlowCore holds a subject reference and not the subject, so it cannot build a prompt without either storing subjects or calling back into the client.
