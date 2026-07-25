-- +goose Up

-- FlowCore's tables live in a dedicated schema so they never collide with a
-- client's own tables. Every query in Go is schema-qualified; the library does
-- not rely on the client's search_path.
create schema if not exists flowcore;

-- A workflow definition is a template. initial_step_definition_id (the entry
-- step a run begins on) references a step in this same definition; it is
-- nullable because the definition row is inserted before its steps exist, then
-- stamped inside the same aggregate-create transaction. Its composite FK is
-- added at the end of this migration, once step_definition exists (the two
-- tables reference each other).
create table flowcore.workflow_definition (
    id                         uuid primary key,
    name                       text not null,
    initial_step_definition_id uuid,
    constraint ck_workflow_definition_name_len check (char_length(name) between 1 and 200)
);

create table flowcore.workflow_status_definition (
    id                   uuid primary key,
    workflow_definition_id uuid not null,
    name                 text not null,
    constraint fk_workflow_status_definition_workflow
        foreign key (workflow_definition_id)
        references flowcore.workflow_definition (id) on delete cascade,
    -- Redundant with the primary key; exists only as the target for the
    -- same-definition composite FKs that reference (workflow_definition_id, id).
    constraint uq_workflow_status_definition_wd_id unique (workflow_definition_id, id),
    constraint ck_workflow_status_definition_name_len check (char_length(name) between 1 and 200)
);

-- Case-insensitive uniqueness of status name within a definition. Named
-- explicitly because the error mapper keys on the constraint name to raise a
-- DuplicateNameError; an auto-generated name would be churn-prone.
create unique index ux_workflow_status_definition_name
    on flowcore.workflow_status_definition (workflow_definition_id, lower(name));

create table flowcore.step_definition (
    id                            uuid primary key,
    workflow_definition_id        uuid not null,
    workflow_status_definition_id uuid not null,
    -- Opaque reference to the person or group expected to act on this step. The
    -- library never interprets it: no FK, no format validation. A default,
    -- later copied to the Step instance at workflow start.
    assignee_id                   text,
    name                          text not null,
    constraint fk_step_definition_workflow
        foreign key (workflow_definition_id)
        references flowcore.workflow_definition (id) on delete cascade,
    -- Same-definition integrity: the step's status must belong to the same
    -- definition. no action, so deleting an in-use status is blocked
    -- (ReferencedError) rather than silently cascading. Deferred so that
    -- deleting a whole definition works: the status is a direct child of the
    -- definition (cascade-deleted) while the referencing step is removed via a
    -- parallel cascade branch; an immediate check would fire mid-cascade before
    -- that branch clears. Deferring to commit lets the whole cascade resolve,
    -- while a standalone status delete still fails at commit (referencing rows
    -- remain), and a single-statement write still fails at the statement's
    -- implicit commit.
    constraint fk_step_definition_status
        foreign key (workflow_definition_id, workflow_status_definition_id)
        references flowcore.workflow_status_definition (workflow_definition_id, id)
        on delete no action deferrable initially deferred,
    -- Redundant with the primary key; target for the composite FKs from
    -- action_definition and workflow_definition.initial_step.
    constraint uq_step_definition_wd_id unique (workflow_definition_id, id),
    constraint ck_step_definition_name_len check (char_length(name) between 1 and 200)
);

create unique index ux_step_definition_name
    on flowcore.step_definition (workflow_definition_id, lower(name));

create table flowcore.action_definition (
    id                                     uuid primary key,
    -- Denormalized, carried so the same-definition composite FKs have a column
    -- to match on.
    workflow_definition_id                 uuid not null,
    step_definition_id                     uuid not null,
    name                                   text not null,
    -- Exactly one of next_step / terminal_status is set (see the XOR check). A
    -- null next_step_definition_id is the only end-of-flow signal; under the
    -- FK's default MATCH SIMPLE, a null in the composite skips the check.
    next_step_definition_id                uuid,
    terminal_workflow_status_definition_id uuid,
    -- Parent step. cascade: deleting a step removes its own actions.
    constraint fk_action_definition_step
        foreign key (workflow_definition_id, step_definition_id)
        references flowcore.step_definition (workflow_definition_id, id) on delete cascade,
    -- Routing target. no action: deleting a step another action routes to is
    -- blocked (ReferencedError). Deferred for the same whole-definition-delete
    -- reason as fk_step_definition_status above.
    constraint fk_action_definition_next_step
        foreign key (workflow_definition_id, next_step_definition_id)
        references flowcore.step_definition (workflow_definition_id, id)
        on delete no action deferrable initially deferred,
    -- Terminal status target, same-definition. no action: deleting an in-use
    -- terminal status is blocked (ReferencedError). Deferred for the same reason.
    constraint fk_action_definition_terminal_status
        foreign key (workflow_definition_id, terminal_workflow_status_definition_id)
        references flowcore.workflow_status_definition (workflow_definition_id, id)
        on delete no action deferrable initially deferred,
    -- Exactly one of next step / terminal status is set. (a IS NULL) <> (b IS NULL)
    -- is true only when precisely one is null.
    constraint ck_action_definition_terminal_xor
        check ((next_step_definition_id is null) <> (terminal_workflow_status_definition_id is null)),
    constraint ck_action_definition_name_len check (char_length(name) between 1 and 200)
);

-- Action names are unique within their step (not their definition): the same
-- action name may legitimately appear on different steps.
create unique index ux_action_definition_name
    on flowcore.action_definition (step_definition_id, lower(name));

-- Circular FK, added last: the entry step must belong to this same definition.
-- no action blocks deleting the entry step out from under the definition.
-- Deferred for the same whole-definition-delete reason as the FKs above.
alter table flowcore.workflow_definition
    add constraint fk_workflow_definition_initial_step
    foreign key (id, initial_step_definition_id)
    references flowcore.step_definition (workflow_definition_id, id)
    on delete no action deferrable initially deferred;

-- +goose Down

-- cascade unwinds the four tables, their indexes, and the circular FK together.
drop schema if exists flowcore cascade;
