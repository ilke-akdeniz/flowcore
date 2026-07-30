-- +goose Up

-- Decision 9 left assignee_id with no CHECK at all, reasoning that a length cap
-- needs a reason beyond hygiene and this column sits in no btree index. Decision
-- 29 overturns that: leaving it uncapped is a correctness defect once the
-- instance side exists, because Engine.Start copies this column into a capped
-- snapshot column. A client could set a 2 MB assignee_id here, have
-- Catalog.AddStep accept it, and then find the definition unstartable when the
-- copy fails a CHECK. So the cap cannot live on the instance side alone.
--
-- 500 rather than 200: this is an opaque machine identifier (a group reference, a
-- user id, an email, a directory name), not a human-facing label. The bound of 1
-- also rejects '', which no caller can mean.
--
-- The CHECK passes for NULL, so "unassigned" stays expressible.
alter table flowcore.step_definition
    add constraint ck_step_definition_assignee_len
    check (char_length(assignee_id) between 1 and 500);

-- +goose Down

alter table flowcore.step_definition
    drop constraint ck_step_definition_assignee_len;
