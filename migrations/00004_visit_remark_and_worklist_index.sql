-- +goose Up

-- Decision 41. The completer's account of this decision: an agent's findings, or
-- a human's "approved, but flagging the Q3 overage". It is an attribute of a
-- decision the library already owns, not of the workflow subject, which is what
-- keeps it on the right side of the not-a-document-store boundary.
--
-- Immutable in the same sense as completed_by: nothing here prevents an UPDATE,
-- and the visit is append-only *in effect* because no method rewrites a closed
-- one. The remark inherits exactly that status rather than a stronger one.
alter table flowcore.step_visit
    add column remark text;

-- A remark is an account of a completion, so it cannot exist without one.
--
-- Deliberately a separate constraint rather than a fourth clause in
-- ck_step_visit_completion. That group is what a completion *must* carry, and a
-- remark is optional — folding it in would mandate one on every completion,
-- which is the mandate a nullable column exists to avoid. This is the same
-- reasoning that kept subject_version_token out of that group in 00003, applied
-- to the one direction that does hold: optional at completion, impossible
-- without it.
alter table flowcore.step_visit
    add constraint ck_step_visit_remark_requires_completion
    check (remark is null or completed_at is not null);

-- The same rule for subject_version_token, which 00003 left unprotected in this
-- one direction. That migration explained why the token is not in
-- ck_step_visit_completion's all-or-nothing group — it is optional, so requiring
-- it would re-impose the mandate its nullability exists to avoid — but that is
-- the "must it be present at completion" direction. Whether it could exist
-- *without* a completion was never addressed, and it cannot: the token records
-- which revision a decision was made against, and an open visit has no decision.
--
-- Landed here rather than by editing 00003, which is already applied and
-- committed; the remark's constraint above is what made the asymmetry visible.
alter table flowcore.step_visit
    add constraint ck_step_visit_subject_version_token_requires_completion
    check (subject_version_token is null or completed_at is not null);

-- 3000 is a statement of intent, not a performance limit: a remark is a sentence
-- or a page, and past that it is a document, which belongs in the client keyed by
-- the subject. Recorded explicitly because a request to raise it would win on
-- benchmark grounds — it would not be measurably slow — and the answer has to be
-- "that is not a remark".
--
-- Higher than every other text column here (500 for opaque identifiers, 200 for
-- names) because this one holds prose meant to be read, not a machine identifier.
-- The CHECK passes for NULL, so "no remark" stays expressible, and the bound of 1
-- rejects '', which no caller can mean.
alter table flowcore.step_visit
    add constraint ck_step_visit_remark_len
    check (char_length(remark) between 1 and 3000);

-- The worklist: open visits by assignee, which is the whole of "what is assigned
-- to me". Decision 42 brings it into scope; decision 32 deferred it with the cost
-- already stated, and decisions 24 and 25 hold the numbers — 0.06 ms / 70 buffers
-- as this index probe, against 19.6 ms / 6,591 for the join it replaces.
--
-- Partial for the same reason ux_step_visit_open is: the open set is sized by
-- work in flight rather than accumulated history, so it stays cached while the
-- visit table grows. ux_step_visit_open cannot serve this query itself — it is
-- keyed by workflow_id, and the worklist probes by assignee across all runs.
--
-- Decision 28 protected this shape in advance: the nullable-override assignee it
-- rejected would have made the real assignee a coalesce across a join, which no
-- index on this table could serve.
--
-- The other two deferred indexes, and step_visit.step_definition_id with them,
-- stay deferred — decision 42 separates the assignee half from the
-- step_definition_id half, and only this half has a caller in iteration 2.
create index ix_step_visit_open_assignee
    on flowcore.step_visit (assignee_id) where completed_at is null;

-- +goose Down

drop index flowcore.ix_step_visit_open_assignee;

alter table flowcore.step_visit
    drop constraint ck_step_visit_remark_len;

alter table flowcore.step_visit
    drop constraint ck_step_visit_subject_version_token_requires_completion;

alter table flowcore.step_visit
    drop constraint ck_step_visit_remark_requires_completion;

alter table flowcore.step_visit
    drop column remark;
