package flowcore

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

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
