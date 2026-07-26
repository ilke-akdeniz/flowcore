package flowcore

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// domainSentinels are the six mapped domain errors. An unmapped constraint must
// match none of them.
var domainSentinels = []error{
	ErrNotFound, ErrDuplicateName, ErrCrossDefinition,
	ErrReferenced, ErrInvalidAction, ErrInvalidName,
}

// TestUnmappedConstraintFailsLoud verifies that a constraint violation whose name
// the mapper does not recognize becomes an UnmappedConstraintError on both the
// write and delete paths — distinguishable as unmapped, matching no domain error,
// and retaining the raw pgconn detail — rather than being guessed at. This is
// what stops an unanticipated FK from silently masquerading as a domain error.
func TestUnmappedConstraintFailsLoud(t *testing.T) {
	unknown := &pgconn.PgError{Code: sqlstateForeignKeyViolation, ConstraintName: "fk_something_unmapped"}
	mapped := map[string]error{
		"write":  mapWriteErr(unknown, "n"),
		"delete": mapDeleteErr(unknown, entityStep, uuid.New()),
	}
	for path, got := range mapped {
		if !errors.Is(got, ErrUnmappedConstraint) {
			t.Errorf("%s: want ErrUnmappedConstraint, got %v", path, got)
		}
		var u *UnmappedConstraintError
		if !errors.As(got, &u) {
			t.Fatalf("%s: want *UnmappedConstraintError, got %v", path, got)
		}
		if u.Constraint != "fk_something_unmapped" || u.Code != sqlstateForeignKeyViolation {
			t.Errorf("%s: carried {%q,%q}, want {fk_something_unmapped,23503}", path, u.Constraint, u.Code)
		}
		for _, s := range domainSentinels {
			if errors.Is(got, s) {
				t.Errorf("%s: unmapped constraint wrongly matched domain sentinel %v", path, s)
			}
		}
		// Raw pgconn detail is retained in the chain for diagnosis.
		var pg *pgconn.PgError
		if !errors.As(got, &pg) {
			t.Errorf("%s: underlying pgconn error not retained", path)
		}
	}

	// An unrecognized unique/check violation is unmapped too, not just FKs.
	badUnique := &pgconn.PgError{Code: sqlstateUniqueViolation, ConstraintName: "ux_unmapped"}
	if !errors.Is(mapWriteErr(badUnique, "n"), ErrUnmappedConstraint) {
		t.Error("unrecognized unique violation should be unmapped")
	}

	// Recognized constraints still map to their domain errors (no regression).
	knownFK := &pgconn.PgError{Code: sqlstateForeignKeyViolation, ConstraintName: fkActionNextStep}
	if !errors.Is(mapWriteErr(knownFK, "n"), ErrCrossDefinition) {
		t.Error("recognized FK no longer maps to CrossDefinition on write")
	}
	if !errors.Is(mapDeleteErr(knownFK, entityStep, uuid.New()), ErrReferenced) {
		t.Error("recognized FK no longer maps to Referenced on delete")
	}
}

// TestTypedErrorsWrapSentinels verifies every typed error matches its own
// sentinel via errors.Is and no other, so a caller can branch on the sentinel.
// Without Unwrap on each type this silently returns false — the bug this guards.
func TestTypedErrorsWrapSentinels(t *testing.T) {
	id := uuid.New()
	all := []error{
		ErrNotFound, ErrDuplicateName, ErrCrossDefinition,
		ErrReferenced, ErrInvalidAction, ErrInvalidName,
	}
	cases := []struct {
		err      error
		sentinel error
	}{
		{&NotFoundError{Entity: entityStep, ID: id}, ErrNotFound},
		{&DuplicateNameError{Entity: entityStatus, Name: "approved"}, ErrDuplicateName},
		{&CrossDefinitionError{}, ErrCrossDefinition},
		{&ReferencedError{Entity: entityStatus, ID: id}, ErrReferenced},
		{&InvalidActionError{}, ErrInvalidAction},
		{&InvalidNameError{}, ErrInvalidName},
	}
	for _, c := range cases {
		if !errors.Is(c.err, c.sentinel) {
			t.Errorf("%T: errors.Is(err, %v) = false, want true", c.err, c.sentinel)
		}
		for _, other := range all {
			if other == c.sentinel {
				continue
			}
			if errors.Is(c.err, other) {
				t.Errorf("%T matched foreign sentinel %v", c.err, other)
			}
		}
	}
}

// TestTypedErrorsExtractViaAs verifies errors.As recovers the concrete type and
// its carried fields, including through a wrapping layer (fmt.Errorf %w), which
// is how callers see errors returned up the stack.
func TestTypedErrorsExtractViaAs(t *testing.T) {
	id := uuid.New()

	wrapped := fmt.Errorf("adding step: %w", &NotFoundError{Entity: entityStep, ID: id})
	var nf *NotFoundError
	if !errors.As(wrapped, &nf) {
		t.Fatalf("errors.As did not extract *NotFoundError from %v", wrapped)
	}
	if nf.Entity != entityStep || nf.ID != id {
		t.Errorf("NotFoundError fields = {%q, %v}, want {%q, %v}", nf.Entity, nf.ID, entityStep, id)
	}

	var dup *DuplicateNameError
	if !errors.As(&DuplicateNameError{Entity: entityAction, Name: "Approve"}, &dup) {
		t.Fatal("errors.As did not extract *DuplicateNameError")
	}
	if dup.Name != "Approve" {
		t.Errorf("DuplicateNameError.Name = %q, want %q (original casing preserved)", dup.Name, "Approve")
	}

	var ref *ReferencedError
	if !errors.As(&ReferencedError{Entity: entityStatus, ID: id}, &ref) {
		t.Fatal("errors.As did not extract *ReferencedError")
	}
	if ref.Entity != entityStatus || ref.ID != id {
		t.Errorf("ReferencedError fields = {%q, %v}, want {%q, %v}", ref.Entity, ref.ID, entityStatus, id)
	}
}
