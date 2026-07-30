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

	// The instance side.
	//
	// ixWorkflowActive enforces one active run per {subject, definition}.
	ixWorkflowActive = "ux_workflow_active"

	// The length CHECKs on opaque identifiers. Distinct from the name CHECKs
	// above: these cap at 500 and map to InvalidIdentifierError, which names the
	// field, because one type serves all of them.
	ckStepDefinitionAssigneeLen = "ck_step_definition_assignee_len"
	ckWorkflowSubjectLen        = "ck_workflow_subject_reference_len"
	ckWorkflowTokenLen          = "ck_workflow_subject_version_token_len"
	ckStepAssigneeLen           = "ck_step_assignee_len"
	ckStepVisitAssigneeLen      = "ck_step_visit_assignee_len"
	ckStepVisitCompletedByLen   = "ck_step_visit_completed_by_len"
	ckStepVisitTokenLen         = "ck_step_visit_subject_version_token_len"

	// The instance-side name CHECKs, which do cap at 200 and so map to
	// InvalidNameError alongside the definition-side ones. They are unreachable
	// through Start, which copies names the definition side already validated at
	// the same limit; they exist in the table so a direct store call or a future
	// caller cannot produce an UnmappedConstraintError for them.
	ckWorkflowNameLen             = "ck_workflow_name_len"
	ckWorkflowStatusNameLen       = "ck_workflow_status_name_len"
	ckStepNameInstanceLen         = "ck_step_name_len"
	ckStepStatusNameLen           = "ck_step_status_name_len"
	ckActionNameInstanceLen       = "ck_action_name_len"
	ckActionTerminalStatusNameLen = "ck_action_terminal_status_name_len"

	// The instance-side action CHECKs. Both are unreachable through Start for the
	// same reason — the definition side rejects the equivalent shape first — and
	// are mapped so a direct store call fails as a domain error rather than loudly.
	ckActionTerminalXORInstance  = "ck_action_terminal_xor"
	ckActionTerminalPairInstance = "ck_action_terminal_pair"

	// ux_step_visit_open is deliberately absent. Two concurrent completions of one
	// visit serialize on the row lock, so the loser's conditional UPDATE matches
	// nothing and returns VisitNotOpenError — it never reaches an insert. The index
	// can only fire if Complete ignores its rows-affected result, which is a library
	// defect, and UnmappedConstraintError is the correct way for a defect to surface.
	//
	// ck_step_visit_completion and ck_step_visit_temporal are absent on the same
	// grounds: the Engine writes all three completion columns in one statement and
	// takes both timestamps from now(), so a violation means the library is wrong,
	// not the caller.
	//
	// The instance cascade-driver FKs (fk_step_workflow, fk_action_step,
	// fk_step_visit_workflow) are absent too. Unlike their definition-side
	// counterparts in decision 20, no concurrent delete can race them: Start creates
	// every parent inside the same transaction as its children, so a missing parent
	// is again a library defect rather than a caller's not-found.
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

// mapWorkflowInsertErr maps a database error from starting a workflow. It exists
// for the same reason mapInsertErr does: the domain error needs data the raw
// violation does not carry. ux_workflow_active reports the colliding pair as an
// index expression, not as a subject and a definition, so the wrapper is handed
// both.
//
// It lives here rather than in the store helper so that every constraint name
// stays in one file — a renamed index is then a one-line change, which is the
// property the central mapper exists to keep.
func mapWorkflowInsertErr(err error, subjectReference string, workflowDefinitionID uuid.UUID) error {
	if err == nil {
		return nil
	}

	pg := asPgError(err)
	if pg == nil {
		return err
	}

	if pg.Code == sqlstateUniqueViolation && pg.ConstraintName == ixWorkflowActive {
		return &ActiveWorkflowExistsError{
			SubjectReference:     subjectReference,
			WorkflowDefinitionID: workflowDefinitionID,
		}
	}

	return mapWriteErr(err, "")
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
		case ckActionTerminalXOR, ckActionTerminalXORInstance, ckActionTerminalPairInstance:
			return &InvalidActionError{}, true
		case ckWorkflowDefinitionNameLen, ckStatusNameLen, ckStepNameLen, ckActionNameLen,
			ckWorkflowNameLen, ckWorkflowStatusNameLen, ckStepNameInstanceLen,
			ckStepStatusNameLen, ckActionNameInstanceLen, ckActionTerminalStatusNameLen:
			return &InvalidNameError{}, true
		}

		if field, ok := identifierField(pg.ConstraintName); ok {
			return &InvalidIdentifierError{Field: field}, true
		}

		return unmapped(pg), true
	}

	return nil, false
}

// identifierField maps a length CHECK on an opaque identifier to the field name
// InvalidIdentifierError reports. Kept as a lookup rather than a switch arm
// returning a bare error, because the field name is the whole value of that error
// — a caller storing four different opaque identifiers needs to know which one
// the database rejected.
func identifierField(constraint string) (string, bool) {
	switch constraint {
	case ckStepDefinitionAssigneeLen, ckStepAssigneeLen, ckStepVisitAssigneeLen:
		return "assigneeId", true
	case ckWorkflowSubjectLen:
		return "subjectReference", true
	case ckWorkflowTokenLen, ckStepVisitTokenLen:
		return "subjectVersionToken", true
	case ckStepVisitCompletedByLen:
		return "completedBy", true
	}

	return "", false
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
