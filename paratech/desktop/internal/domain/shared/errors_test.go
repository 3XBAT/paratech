package shared

import (
	"errors"
	"fmt"
	"testing"
)

func TestConflictErrorsRelations(t *testing.T) {
	cases := []struct {
		name string
		err  error
		is   []error
		not  []error
	}{
		{"version conflict", ErrVersionConflict, []error{ErrConflict, ErrVersionConflict}, []error{ErrDuplicate, ErrNotFound}},
		{"duplicate", ErrDuplicate, []error{ErrConflict, ErrDuplicate}, []error{ErrVersionConflict, ErrNotFound}},
		{"plain conflict", ErrConflict, []error{ErrConflict}, []error{ErrVersionConflict, ErrDuplicate}},
		{"wrapped version conflict", fmt.Errorf("save job j1: %w", ErrVersionConflict), []error{ErrConflict, ErrVersionConflict}, []error{ErrDuplicate}},
		{"wrapped duplicate", fmt.Errorf("save delivery: %w", ErrDuplicate), []error{ErrConflict, ErrDuplicate}, []error{ErrVersionConflict}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, want := range tc.is {
				if !errors.Is(tc.err, want) {
					t.Errorf("errors.Is(%v, %v) = false, want true", tc.err, want)
				}
			}
			for _, not := range tc.not {
				if errors.Is(tc.err, not) {
					t.Errorf("errors.Is(%v, %v) = true, want false", tc.err, not)
				}
			}
		})
	}
}

func TestStorageErrorsRelations(t *testing.T) {
	narrow := []error{ErrStoragePermission, ErrStorageFull, ErrStorageBusy}
	for _, e := range narrow {
		if !errors.Is(fmt.Errorf("write: %w", e), ErrStorage) {
			t.Errorf("errors.Is(%v, ErrStorage) = false", e)
		}
		if errors.Is(e, ErrConflict) || errors.Is(e, ErrNotFound) {
			t.Errorf("%v must not match conflict or not found", e)
		}
		for _, o := range narrow {
			if o != e && errors.Is(e, o) {
				t.Errorf("errors.Is(%v, %v) = true", e, o)
			}
		}
	}
	if errors.Is(ErrStorage, ErrStoragePermission) {
		t.Error("the general class must not match a narrower error")
	}
}

// ErrInvalidSecret is a caller error, not a storage failure or a missing value.
func TestInvalidSecretIsDistinct(t *testing.T) {
	for _, other := range []error{ErrStorage, ErrNotFound, ErrInvalidID, ErrConflict} {
		if errors.Is(ErrInvalidSecret, other) || errors.Is(other, ErrInvalidSecret) {
			t.Errorf("ErrInvalidSecret and %v must not match", other)
		}
	}
}
