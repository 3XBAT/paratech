package backoff

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

func TestClassify(t *testing.T) {
	base := errors.New("boom")
	tests := []struct {
		name string
		err  error
		want Class
	}{
		{"nil", nil, ClassTransient},
		{"unclassified", base, ClassTransient},
		{"deadline", context.DeadlineExceeded, ClassTransient},
		{"transient marker", Transient(base), ClassTransient},
		{"permanent marker", Permanent(base), ClassPermanent},
		{"permanent wrapped further", fmt.Errorf("stage: %w", Permanent(base)), ClassPermanent},
		{"permanent wins over transient", Transient(Permanent(base)), ClassPermanent},
		{"storage permission", fmt.Errorf("write: %w", shared.ErrStoragePermission), ClassPermanent},
		{"storage full", shared.ErrStorageFull, ClassTransient},
		{"invalid id", shared.ErrInvalidID, ClassPermanent},
		{"invalid artifact ref", shared.ErrInvalidArtifactRef, ClassPermanent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.err); got != tc.want {
				t.Fatalf("Classify(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestMarkersKeepTheCause(t *testing.T) {
	base := errors.New("boom")
	for _, err := range []error{Transient(base), Permanent(base)} {
		if !errors.Is(err, base) {
			t.Errorf("%v lost its cause", err)
		}
	}
	if Transient(nil) != nil || Permanent(nil) != nil {
		t.Error("wrapping nil must stay nil")
	}
	if !errors.Is(Transient(base), ErrTransient) || errors.Is(Transient(base), ErrPermanent) {
		t.Error("Transient marks the wrong class")
	}
}
