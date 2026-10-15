package delivery

import (
	"errors"
	"testing"
)

func TestStatuses(t *testing.T) {
	want := []string{"PENDING", "SENDING", "SENT", "FAILED", "UNKNOWN"}
	got := Statuses()
	if len(got) != len(want) {
		t.Fatalf("Statuses() has %d values, want %d", len(got), len(want))
	}
	seen := map[Status]bool{}
	for i, s := range got {
		if s.String() != want[i] {
			t.Errorf("Statuses()[%d] = %q, want %q", i, s, want[i])
		}
		if seen[s] {
			t.Errorf("duplicate status %q", s)
		}
		seen[s] = true
	}
	if !seen[StatusUnknown] {
		t.Error("UNKNOWN (PRD sections 10 and 14, hard rule 7) is missing")
	}
}

func TestOutcomeErrorsAreDistinct(t *testing.T) {
	errs := []error{ErrOutcomeUnknown, ErrRejected, ErrTransient, ErrAttachmentChanged}
	for i, a := range errs {
		for j, b := range errs {
			if (i == j) != errors.Is(a, b) {
				t.Errorf("errors.Is(%v, %v) = %v", a, b, errors.Is(a, b))
			}
		}
	}
}
