-- +goose Up

-- The console's own schema, separate from the library's.
--
-- FlowCore owns `flowcore` and ships its own migrations; this owns `console`.
-- Two schemas in one database, two migration histories, no overlap — which is
-- what a library that is not a service looks like from the caller's side.
create schema console;

-- One visitor's slice of the world.
--
-- Hosting means concurrent visitors, and without isolation two people signing in
-- as the same person would work the same claim. Everything below except staff
-- carries a session id, and the session's rows are copied from a template on
-- first arrival.
create table console.session (
    id           text primary key,
    created_at   timestamptz not null,
    last_seen_at timestamptz not null
);

-- The cast: shared across every session, read-only.
--
-- What belongs to a visitor is the work, not the people — there is one Dana
-- Whitfield. `reference` is what FlowCore records as completedBy and matches
-- against assignees; the library never interprets it.
--
-- Named `staff` rather than `user` because `user` is a reserved word in Postgres
-- and would need quoting at every site.
create table console.staff (
    reference  text primary key,
    name       text not null,
    title      text not null,
    -- The groups this person also answers to. Expanding a person into their
    -- group references is work the library deliberately does not do.
    groups     text[] not null default '{}',
    sort_order int not null
);

-- Which workflow is active for which kind of submission.
--
-- This table is the whole mechanism behind "specify when a workflow applies".
-- FlowCore takes a definition id and starts a run; it has no notion of a
-- submission type and will not acquire one. The console stores what a graph is
-- for.
create table console.workflow_registry (
    id                     uuid primary key,
    session_id             text not null references console.session (id) on delete cascade,
    submission_type        text not null,
    name                   text not null,
    -- Recorded, never enforced: a foreign key across schemas into the library's
    -- tables would couple the console's lifecycle to FlowCore's.
    flowcore_definition_id uuid not null,
    active                 boolean not null,
    created_at             timestamptz not null,
    constraint ck_registry_type check (submission_type in ('claim', 'application'))
);

-- At most one active workflow per {session, submission type}. Retired ones stay,
-- because runs that started under them are still answerable.
create unique index ux_registry_active
    on console.workflow_registry (session_id, submission_type) where active;

create index ix_registry_session on console.workflow_registry (session_id);

-- What the queue needs, common to both kinds of submission.
create table console.submission (
    id                     uuid primary key,
    session_id             text not null references console.session (id) on delete cascade,
    type                   text not null,
    reference              text not null,
    -- A draft has no run. Submitting is the only thing that starts one.
    status                 text not null,
    created_at             timestamptz not null,
    submitted_at           timestamptz,
    -- The workflow it was submitted under, frozen at submission. Activating a
    -- different workflow later must not rewrite this: runs in flight keep what
    -- they started with, which is FlowCore's guarantee and the console's job not
    -- to undermine.
    flowcore_definition_id uuid,
    -- What FlowCore was given. Stored rather than re-derived so a lookup needs no
    -- string building.
    subject_reference      text,
    constraint ck_submission_type check (type in ('claim', 'application')),
    constraint ck_submission_status check (status in ('draft', 'submitted')),
    -- A draft has neither; a submission has both.
    constraint ck_submission_started check (
        (status = 'draft') = (submitted_at is null)
        and (status = 'draft') = (flowcore_definition_id is null)
        and (status = 'draft') = (subject_reference is null)
    )
);

create unique index ux_submission_reference on console.submission (session_id, reference);
create index ix_submission_session on console.submission (session_id, created_at);

create table console.claim_detail (
    submission_id      uuid primary key references console.submission (id) on delete cascade,
    policy_number      text not null,
    claimant_name      text not null,
    amount             numeric(12, 2) not null,
    occurred_at        date not null,
    -- The claimant's own account, in their words. One of the two texts the
    -- narrative-consistency agent reads.
    incident_narrative text not null
);

create table console.application_detail (
    submission_id uuid primary key references console.submission (id) on delete cascade,
    proposer_name text not null,
    cover_type    text not null,
    sum_insured   numeric(12, 2) not null,
    -- Free text; what the risk-screen agent reads.
    disclosures   text not null
);

-- Documents are records carrying text, not files.
--
-- The documents that matter are prose: a police report and a repair estimate are
-- what the agents compare against the claimant's account, so the text is the
-- document. A photograph is a row with a name, a date and no body.
create table console.document (
    id            uuid primary key,
    submission_id uuid not null references console.submission (id) on delete cascade,
    name          text not null,
    kind          text not null,
    received_at   date not null,
    body          text,
    constraint ck_document_kind check (
        kind in ('police_report', 'estimate', 'photograph', 'correspondence'))
);

create index ix_document_submission on console.document (submission_id);

-- +goose Down

drop schema console cascade;
