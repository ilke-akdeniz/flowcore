package flowcore

import (
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// Constraint and index names as declared in the migrations. The mapper keys on
// these explicitly — an unrecognized constraint is never guessed at, it becomes
// an UnmappedConstraintError — so they must stay in lockstep with the schema. A
// constraint-mapping test provokes each to fail CI loudly if a migration renames
// one, and a separate test asserts an unrecognized name maps to no domain error.
const (
	ixStatusName = "ux_workflow_status_definition_name"
	ixStepName   = "ux_step_definition_name"
	ixActionName = "ux_action_definition_name"

	ckActionTerminalXOR         = "ck_action_definition_terminal_xor"
	ckWorkflowDefinitionNameLen = "ck_workflow_definition_name_len"
	ckStatusNameLen             = "ck_workflow_status_definition_name_len"
	ckStepNameLen               = "ck_step_definition_name_len"
	ckActionNameLen             = "ck_action_definition_name_len"

	// The same-definition reference FKs. On a write a violation means a reference
	// pointed outside its definition (CrossDefinitionError); on a delete it means
	// the target is still referenced (ReferencedError). fkInitialStep's write side
	// is unreachable (Create's stepExists pre-flight intercepts it), but its
	// delete side (deleting the entry step) is live.
	fkStepStatus           = "fk_step_definition_status"
	fkActionNextStep       = "fk_action_definition_next_step"
	fkActionTerminalStatus = "fk_action_definition_terminal_status"
	fkInitialStep          = "fk_workflow_definition_initial_step"

	// The cascade-driver FKs to the parent row. A violation means the parent is
	// gone, which the caller experiences as not-found; mapInsertErr turns them
	// into NotFoundError. Only an insert can violate them — parent-membership
	// columns are immutable, so no update ever writes them — and their delete side
	// cascades rather than blocking.
	fkStatusWorkflow = "fk_workflow_status_definition_workflow"
	fkStepWorkflow   = "fk_step_definition_workflow"
	fkActionStep     = "fk_action_definition_step"
)

// SQLSTATE codes we map. Kept as local constants to avoid a dependency on a
// separate error-code package.
const (
	sqlstateUniqueViolation     = "23505"
	sqlstateForeignKeyViolation = "23503"
	sqlstateCheckViolation      = "23514"
)

// mapWriteErr maps a database error from an Add/Update path to the domain
// taxonomy. name is the name the operation attempted to write, used to populate
// DuplicateNameError in the caller's original casing (the raw error only carries
// the lowercased index expression). A foreign-key violation on a recognized
// reference FK means a reference pointed outside the definition.
func mapWriteErr(err error, name string) error {
	if err == nil {
		return nil
	}

	pg := asPgError(err)
	if pg == nil {
		return err
	}

	if mapped, ok := mapConstraintCommon(pg, name); ok {
		return mapped
	}

	if pg.Code == sqlstateForeignKeyViolation {
		switch pg.ConstraintName {
		case fkStepStatus, fkActionNextStep, fkActionTerminalStatus, fkInitialStep:
			return &CrossDefinitionError{}
		}
		return unmapped(pg)
	}

	return err
}

// mapInsertErr maps a database error from an insert path to the domain taxonomy.
// It is a third operation intent alongside mapWriteErr and mapDeleteErr, on the
// same reasoning that split those two: an insert is the only operation that can
// violate a cascade-driver foreign key, since parent-membership columns are
// immutable and no update statement writes them. Such a violation means the
// parent row does not exist, so the wrapper carries the parent's entity and id —
// the raw error names the referencing table, not the missing parent, which is
// the same gap that makes mapDeleteErr take an entity and id.
//
// Everything else an insert can violate is intent-independent and delegates to
// mapWriteErr, including the fail-loud fallback for an unrecognized constraint.
func mapInsertErr(err error, name string, parentEntity string, parentID uuid.UUID) error {
	if err == nil {
		return nil
	}

	pg := asPgError(err)
	if pg == nil {
		return err
	}

	if pg.Code == sqlstateForeignKeyViolation {
		switch pg.ConstraintName {
		case fkStatusWorkflow, fkStepWorkflow, fkActionStep:
			return &NotFoundError{Entity: parentEntity, ID: parentID}
		}
	}

	return mapWriteErr(err, name)
}

// mapDeleteErr maps a database error from a Delete path to the domain taxonomy.
// entity and id name the row being deleted, used to populate ReferencedError
// (the raw error identifies the referencing table, not the target being
// deleted). A foreign-key violation on a recognized reference FK means the
// target is still referenced.
func mapDeleteErr(err error, entity string, id uuid.UUID) error {
	if err == nil {
		return nil
	}

	pg := asPgError(err)
	if pg == nil {
		return err
	}

	if mapped, ok := mapConstraintCommon(pg, ""); ok {
		return mapped
	}

	if pg.Code == sqlstateForeignKeyViolation {
		switch pg.ConstraintName {
		case fkStepStatus, fkActionNextStep, fkActionTerminalStatus, fkInitialStep:
			return &ReferencedError{Entity: entity, ID: id}
		}
		return unmapped(pg)
	}

	return err
}

// mapConstraintCommon handles the intent-independent violations shared by both
// paths: duplicate name (unique index) and the check violations (action XOR,
// name length). The bool reports whether pg was a unique/check violation and so
// handled here — recognized names return their domain error, an unrecognized
// name of one of those codes returns an UnmappedConstraintError (fail loud). It
// returns ok=false only when pg is neither a unique nor a check violation,
// leaving the foreign-key case to the caller.
func mapConstraintCommon(pg *pgconn.PgError, name string) (error, bool) {
	switch pg.Code {
	case sqlstateUniqueViolation:
		switch pg.ConstraintName {
		case ixStatusName:
			return &DuplicateNameError{Entity: entityStatus, Name: name}, true
		case ixStepName:
			return &DuplicateNameError{Entity: entityStep, Name: name}, true
		case ixActionName:
			return &DuplicateNameError{Entity: entityAction, Name: name}, true
		}
		return unmapped(pg), true
	case sqlstateCheckViolation:
		switch pg.ConstraintName {
		case ckActionTerminalXOR:
			return &InvalidActionError{}, true
		case ckWorkflowDefinitionNameLen, ckStatusNameLen, ckStepNameLen, ckActionNameLen:
			return &InvalidNameError{}, true
		}
		return unmapped(pg), true
	}

	return nil, false
}

func unmapped(pg *pgconn.PgError) error {
	return &UnmappedConstraintError{Constraint: pg.ConstraintName, Code: pg.Code, cause: pg}
}

func asPgError(err error) *pgconn.PgError {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg
	}

	return nil
}
