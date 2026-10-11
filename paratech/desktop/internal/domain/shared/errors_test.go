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
