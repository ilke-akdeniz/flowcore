-- +goose Up

-- The instance side. A workflow is a running instance started from a definition;
-- `step` and `action` are a snapshot of that definition's graph, frozen at start,
-- and `step_visit` is the record of work actually performed.
--
-- Two properties shape every table here.
--
-- The snapshot is eager and whole: start copies every status, step, and action, so
-- the run never reads the definition again. Immunity to later definition edits is
-- therefore structural, not a matter of timing.
--
-- Every instance row records the id of the definition row it was copied from, with
-- no foreign key. Those ids are the rename-stable handle for cross-run queries
-- ("all steps that were Director Approval"), and they keep grouping correctly
-- after the definition row they name has been renamed or deleted. Recording an
-- identifier and enforcing nothing about it is the same pattern already used for
-- assignee_id and the subject reference — principle 1, applied to the library's
-- own ids.

-- A running workflow. completed_at is the structural open/complete marker: the
-- library never infers whether a run is finished from a status name, because it
-- does not interpret status names.
--
-- The status pair is stamped by the Engine at every transition — from the entered
-- step's snapshot status on a routing transition, from the terminating action's
-- terminal status on a terminating one. It is authoritative stored state, not a
-- cached derivation, and it is the only mutable redundant data in the schema:
-- no CHECK can keep the two columns consistent, only the Engine's transaction.
create table flowcore.workflow (
    id                            uuid primary key,
    -- Provenance, and the key (with the subject) of the one-active-run invariant.
    -- No foreign key: a run outlives the definition it started from.
    workflow_definition_id        uuid not null,
    name                          text not null,
    -- Opaque. The library compares it for equality and never interprets it.
    subject_reference             text not null,
    -- Opaque, captured at start. Nullable, so a client that does not version its
    -- subjects is not made to fabricate a token; that makes the immutability
    -- audit available to every client and guaranteed by the library for none.
    subject_version_token         text,
    workflow_status_definition_id uuid not null,
    workflow_status_name          text not null,
    started_at                    timestamptz not null,
    completed_at                  timestamptz,
    constraint ck_workflow_name_len
        check (char_length(name) between 1 and 200),
    constraint ck_workflow_subject_reference_len
        check (char_length(subject_reference) between 1 and 500),
    constraint ck_workflow_subject_version_token_len
        check (char_length(subject_version_token) between 1 and 500),
    constraint ck_workflow_status_name_len
        check (char_length(workflow_status_name) between 1 and 200)
);

-- Only one active run per {subject, definition}. Enforced here rather than by an
-- application check: this is not the library interpreting the subject reference,
-- it is using equality to keep its own state unambiguous, and it is the
-- reversible direction — dropping a unique index later is trivial, adding one
-- after clients hold duplicate rows is not.
--
-- subject_reference is NOT NULL for this index's sake, not for tidiness: Postgres
-- treats NULLs as distinct in a unique index, so a nullable column would not
-- weaken this invariant, it would make it not apply at all.
create unique index ux_workflow_active
    on flowcore.workflow (subject_reference, workflow_definition_id)
    where completed_at is null;

-- Deliberately NOT partial. ux_workflow_active excludes completed runs, so a
-- GetCurrentStep against a finished workflow — which must answer "complete, no
-- current step" rather than not-found — would otherwise have no index at all.
create index ix_workflow_subject
    on flowcore.workflow (subject_reference, workflow_definition_id);

-- A step of the frozen graph. This row is written once at start, one per
-- definition step, and carries no execution state whatsoever: no completed_at, no
-- completer, no selected action. Whether a step has been reached is answered by
-- the presence of a step_visit row, never by a null column here.
create table flowcore.step (
    id                            uuid primary key,
    workflow_id                   uuid not null,
    -- Provenance, no foreign key.
    step_definition_id            uuid not null,
    name                          text not null,
    -- Frozen at start. The status the run shows while sitting on this step.
    workflow_status_definition_id uuid not null,
    workflow_status_name          text not null,
    -- The frozen *default* assignee, copied from the step definition at start and
    -- immutable thereafter. The live assignee for a particular visit lives on
    -- step_visit; keeping the default here is what lets a second visit (via a
    -- loop) reset to it rather than inherit the previous visit's reassignment,
    -- and what keeps a reassignment from rewriting who past decisions were
    -- assigned to.
    assignee_id                   text,
    constraint fk_step_workflow
        foreign key (workflow_id)
        references flowcore.workflow (id) on delete cascade,
    -- Redundant with the primary key; exists only as the target for the
    -- same-workflow composite FKs that reference (workflow_id, id).
    constraint uq_step_workflow_id unique (workflow_id, id),
    -- The snapshot copies each definition step exactly once per workflow.
    constraint uq_step_workflow_step_definition
        unique (workflow_id, step_definition_id),
    constraint ck_step_name_len
        check (char_length(name) between 1 and 200),
    constraint ck_step_status_name_len
        check (char_length(workflow_status_name) between 1 and 200),
    constraint ck_step_assignee_len
        check (char_length(assignee_id) between 1 and 500)
);

-- An action of the frozen graph.
create table flowcore.action (
    id                                     uuid primary key,
    -- Denormalized, carried so the same-workflow composite FKs have a column to
    -- match on. It cannot lie: the FK to its owning step checks the pair.
    workflow_id                            uuid not null,
    step_id                                uuid not null,
    -- Provenance, no foreign key.
    action_definition_id                   uuid not null,
    name                                   text not null,
    -- Exactly one of next_step_id / terminal status is set (see the XOR check).
    -- Under the FK's default MATCH SIMPLE, a null in the composite skips the check.
    next_step_id                           uuid,
    terminal_workflow_status_definition_id uuid,
    terminal_workflow_status_name          text,
    -- Parent step. cascade: deleting a step removes its own actions.
    constraint fk_action_step
        foreign key (workflow_id, step_id)
        references flowcore.step (workflow_id, id) on delete cascade,
    -- Routing target, same workflow. no action: deleting a step another action
    -- routes to is blocked. Deferred so a cascade that removes both sides is
    -- checked once the state is self-consistent again, rather than depending on
    -- the order Postgres happens to fire cascade triggers.
    constraint fk_action_next_step
        foreign key (workflow_id, next_step_id)
        references flowcore.step (workflow_id, id)
        on delete no action deferrable initially deferred,
    -- Redundant with the primary key; target for step_visit's selected-action FK.
    -- Leading step_id also serves "list this step's actions".
    constraint uq_action_step_id unique (step_id, id),
    -- Exactly one of next step / terminal status. (a IS NULL) <> (b IS NULL) is
    -- true only when precisely one is null.
    constraint ck_action_terminal_xor
        check ((next_step_id is null) <> (terminal_workflow_status_definition_id is null)),
    -- A terminal status is an id *and* a frozen name, or neither. The id is the
    -- rename-stable handle; the name is what a client displays without needing
    -- the definition to still exist.
    constraint ck_action_terminal_pair
        check ((terminal_workflow_status_definition_id is null)
               = (terminal_workflow_status_name is null)),
    constraint ck_action_name_len
        check (char_length(name) between 1 and 200),
    constraint ck_action_terminal_status_name_len
        check (char_length(terminal_workflow_status_name) between 1 and 200)
);

-- Postgres does not index the referencing side of a foreign key. Both of these
-- sit on a delete-check path, and without them each check is a sequential scan.
-- ix_action_workflow_step also serves "list a workflow's actions" in one query.
create index ix_action_workflow_step on flowcore.action (workflow_id, step_id);
create index ix_action_next_step on flowcore.action (workflow_id, next_step_id);

-- One row per *entry into* a step. Written lazily: start opens exactly one, and
-- completing a step closes it and opens the next. A step not yet reached has no
-- row here at all, which is why "the visit with no completed_at" is unambiguous
-- rather than one of many null timestamps.
--
-- A step entered twice by a loop has two rows, and both are kept. This table is
-- append-only in effect: a closed visit is never rewritten, so reassignment
-- cannot retroactively change who a past decision was assigned to.
--
-- Its parent is the workflow, not the step. Parenting it to the step would mean
-- deleting a snapshot step silently deletes its execution history; as written,
-- the step reference below blocks that delete instead.
create table flowcore.step_visit (
    id                    uuid primary key,
    workflow_id           uuid not null,
    step_id               uuid not null,
    -- The live assignee for *this visit*, seeded from the step's frozen default
    -- on entry. Mutable, so a visit can be reassigned; no method does so this
    -- slice, which is the absence of a constraint rather than an unused feature.
    assignee_id           text,
    -- Timestamps come from SQL now(), written explicitly by the Engine. Not from
    -- Go: multiple library processes against one database would each contribute
    -- their own clock, and ordinary NTP drift could store a completion earlier
    -- than its own entry. now() is transaction timestamp, so one clock serves
    -- everything and monotonicity across transactions is structural.
    entered_at            timestamptz not null,
    -- These three are set together or not at all (see ck_step_visit_completion).
    -- completed_by is nullable only because an *open* visit has no completer;
    -- NOT NULL would make opening a step impossible. It is required at
    -- completion, and the CHECK is what enforces that.
    completed_at          timestamptz,
    completed_by          text,
    selected_action_id    uuid,
    -- Stamped at completion from the value the caller supplies. Nullable for the
    -- same reason as the workflow's token.
    subject_version_token text,
    constraint fk_step_visit_workflow
        foreign key (workflow_id)
        references flowcore.workflow (id) on delete cascade,
    -- Same-workflow integrity: a visit cannot reference a step of another
    -- workflow. no action: a step with recorded history cannot be deleted.
    constraint fk_step_visit_step
        foreign key (workflow_id, step_id)
        references flowcore.step (workflow_id, id)
        on delete no action deferrable initially deferred,
    -- Scoped to the *step*, not just the workflow. This is what makes "the
    -- requested action exists in the step" a schema guarantee rather than an
    -- application check: the pair must match, so an action belonging to a
    -- different step is rejected by the database on every path. A null
    -- selected_action_id skips the check under MATCH SIMPLE, so an open visit is
    -- unaffected.
    constraint fk_step_visit_selected_action
        foreign key (step_id, selected_action_id)
        references flowcore.action (step_id, id)
        on delete no action deferrable initially deferred,
    -- Completion is all-or-nothing. A visit completed with no completer, or with
    -- an action selected and no completion time, is unrepresentable. The token is
    -- deliberately excluded: it is optional, and including it here would
    -- re-impose the mandate the nullable column exists to avoid.
    constraint ck_step_visit_completion check (
        (completed_at is null) = (completed_by is null)
        and (completed_at is null) = (selected_action_id is null)
    ),
    -- Safe only because the library owns the clock, per entered_at above. Equal
    -- values are permitted in case an entry and its completion ever share a
    -- transaction.
    constraint ck_step_visit_temporal
        check (completed_at is null or completed_at >= entered_at),
    constraint ck_step_visit_assignee_len
        check (char_length(assignee_id) between 1 and 500),
    constraint ck_step_visit_completed_by_len
        check (char_length(completed_by) between 1 and 500),
    constraint ck_step_visit_subject_version_token_len
        check (char_length(subject_version_token) between 1 and 500)
);

-- At most one open visit per workflow: exactly one while the run is in progress,
-- zero once it is complete. This index does three jobs at once.
--
-- It enforces the invariant, which a current-step pointer column could not: two
-- open visits, or a pointer aimed at a closed one, would both be representable.
--
-- It *is* the current-step lookup, so no pointer is needed. A partial index is
-- sized by runs in flight rather than accumulated history, so it stays cached
-- indefinitely while the visit table grows.
--
-- And it serializes step completion. Two concurrent completions of one run cannot
-- both insert the next visit; the loser gets a unique violation, which is why the
-- completion path needs no version column, no SELECT FOR UPDATE, and no
-- serializable isolation.
create unique index ux_step_visit_open
    on flowcore.step_visit (workflow_id) where completed_at is null;

-- Referencing-side indexes for the two reference FKs above. The first's leading
-- column also serves the cascade from workflow and "read a run's full history";
-- note ux_step_visit_open cannot, since it excludes closed visits.
create index ix_step_visit_workflow_step
    on flowcore.step_visit (workflow_id, step_id);
create index ix_step_visit_selected_action
    on flowcore.step_visit (step_id, selected_action_id);

-- +goose Down

-- Reverse dependency order. cascade also unwinds the indexes and the deferred FKs.
drop table if exists flowcore.step_visit cascade;
drop table if exists flowcore.action cascade;
drop table if exists flowcore.step cascade;
drop table if exists flowcore.workflow cascade;
