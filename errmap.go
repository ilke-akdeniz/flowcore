package flowcore

import (
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// Constraint and index names as declared in the migrations. The mapper keys on
// these, so they must stay in lockstep with the schema; a constraint-mapping
// test provokes each one to fail CI loudly if a migration renames one.
const (
	ixStatusName = "ux_workflow_status_definition_name"
	ixStepName   = "ux_step_definition_name"
	ixActionName = "ux_action_definition_name"

	ckActionTerminalXOR   = "ck_action_definition_terminal_xor"
	ckWFDefinitionNameLen = "ck_workflow_definition_name_len"
	ckStatusNameLen       = "ck_workflow_status_definition_name_len"
	ckStepNameLen         = "ck_step_definition_name_len"
	ckActionNameLen       = "ck_action_definition_name_len"
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
// the lowercased index expression). A foreign-key violation on a write means a
// reference pointed outside the definition.
func mapWriteErr(err error, name string) error {
	if err == nil {
		return nil
	}
	pg := asPgError(err)
	if pg == nil {
		return err
	}
	if mapped := mapCommon(pg, name); mapped != nil {
		return mapped
	}
	if pg.Code == sqlstateForeignKeyViolation {
		return &CrossDefinitionError{}
	}
	return err
}

// mapDeleteErr maps a database error from a Delete path to the domain taxonomy.
// entity and id name the row being deleted, used to populate ReferencedError
// (the raw error identifies the referencing table, not the target being
// deleted). A foreign-key violation on a delete means the target is still
// referenced.
func mapDeleteErr(err error, entity string, id uuid.UUID) error {
	if err == nil {
		return nil
	}
	pg := asPgError(err)
	if pg == nil {
		return err
	}
	if mapped := mapCommon(pg, ""); mapped != nil {
		return mapped
	}
	if pg.Code == sqlstateForeignKeyViolation {
		return &ReferencedError{Entity: entity, ID: id}
	}
	return err
}

// mapCommon handles the intent-independent signals shared by both paths:
// duplicate name (unique index) and the check violations (action XOR, name
// length). Returns nil if pg is not one of these, letting the caller apply its
// intent-specific foreign-key branch.
func mapCommon(pg *pgconn.PgError, name string) error {
	switch pg.Code {
	case sqlstateUniqueViolation:
		switch pg.ConstraintName {
		case ixStatusName:
			return &DuplicateNameError{Entity: entityStatus, Name: name}
		case ixStepName:
			return &DuplicateNameError{Entity: entityStep, Name: name}
		case ixActionName:
			return &DuplicateNameError{Entity: entityAction, Name: name}
		}
	case sqlstateCheckViolation:
		switch pg.ConstraintName {
		case ckActionTerminalXOR:
			return &InvalidActionError{}
		case ckWFDefinitionNameLen, ckStatusNameLen, ckStepNameLen, ckActionNameLen:
			return &InvalidNameError{}
		}
	}
	return nil
}

func asPgError(err error) *pgconn.PgError {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg
	}
	return nil
}
