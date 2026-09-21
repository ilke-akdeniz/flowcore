-- +goose Up

-- assignee_id becomes required everywhere it appears.
--
-- Decision 9 made it nullable in a single clause and never argued for it; the
-- reason was to let a definition be authored before its assignees were known. That
-- purpose is better served by a value than by NULL, because this column is an
-- opaque string the library never interprets: a step whose owner is undecided can
-- say so with 'unassigned', a pool with 'pool:support', a machine with
-- 'system:auto'. Every one of those is findable.
--
-- NULL is not. The worklist added in 00004 matches with `assignee_id = any($1)`,
-- and NULL matches nothing — not even a wildcard. So an unassigned visit is open,
-- completable, and in nobody's queue: work that exists and cannot be found by the
-- one query whose job is finding work. Nullability bought nothing a convention
-- string does not buy better, and cost exactly that.
--
-- It also removes the hazard decision 22 was built to defend against, where
-- UpdateStep with an unset AssigneeID silently unassigns a step. A constraint at
-- the source beats machinery guarding the call site, which is decision 22's own
-- framing — it exists for "the one params field a constraint does not protect",
-- and this is that constraint.
--
-- No backfill. The library has no released version and no client databases, so
-- nothing is being migrated; inventing an assignee for rows that do not exist
-- would be worse than failing loudly if any ever did.
--
-- The existing ck_*_assignee_len checks (1..500) still apply, so the pair now
-- guarantees a present, non-empty identifier at all three levels.

-- The definition's default.
alter table flowcore.step_definition
    alter column assignee_id set not null;

-- The snapshot's frozen default, copied from the definition at start. It cannot
-- be null once its source cannot be.
alter table flowcore.step
    alter column assignee_id set not null;

-- The live assignee for one visit, seeded from the frozen default on entry and
-- reassignable afterwards. Reassignment takes a required value, so neither the
-- seed nor any later write can produce NULL.
alter table flowcore.step_visit
    alter column assignee_id set not null;

-- +goose Down

alter table flowcore.step_visit
    alter column assignee_id drop not null;

alter table flowcore.step
    alter column assignee_id drop not null;

alter table flowcore.step_definition
    alter column assignee_id drop not null;
