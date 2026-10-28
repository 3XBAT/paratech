package assignment

import (
	"errors"
	"testing"
	"time"
)

func TestRestoreWindowIsThreeDays(t *testing.T) {
	if RestoreWindow != 72*time.Hour {
		t.Fatalf("RestoreWindow = %v, want 72h (owner 2026-10-27)", RestoreWindow)
	}
}

// The window boundary (ADR-0029 addendum 2): exactly at the end «Вернуть» still works and nothing is due; one
// moment later Restore is ErrRestoreExpired and the purge is due. Both decide on the same instant.
func TestRestoreWindowBoundary(t *testing.T) {
	deletedAt := t0.Add(time.Hour)
	end := deletedAt.Add(RestoreWindow)
	cases := []struct {
		name    string
		now     time.Time
		wantErr error
		due     bool
	}{
		{"right after the delete", deletedAt, nil, false},
		{"one day later", deletedAt.Add(24 * time.Hour), nil, false},
		{"exactly at the end", end, nil, false},
		{"a nanosecond later", end.Add(time.Nanosecond), ErrRestoreExpired, true},
		{"a millisecond later", end.Add(time.Millisecond), ErrRestoreExpired, true},
		{"a week later", deletedAt.Add(7 * 24 * time.Hour), ErrRestoreExpired, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := extracted(t)
			if err := a.Delete(deletedAt); err != nil {
				t.Fatal(err)
			}
			if got := a.PurgeDue(tc.now); got != tc.due {
				t.Fatalf("PurgeDue = %v, want %v", got, tc.due)
			}
			before := a.State()
			err := a.Restore(tc.now)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("Restore: err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil && a.State() != before {
				t.Fatalf("an expired Restore changed the assignment:\n got %+v\nwant %+v", a.State(), before)
			}
			if tc.wantErr == nil && a.Deleted() {
				t.Fatal("still deleted after Restore")
			}
		})
	}
}

func TestPurgeDueOfALiveAssignment(t *testing.T) {
	a := extracted(t)
	if a.PurgeDue(t0.Add(365 * 24 * time.Hour)) {
		t.Fatal("a live assignment is due for the purge")
	}
	// Restore of one not deleted stays ErrNotDeleted however late.
	if err := a.Restore(t0.Add(365 * 24 * time.Hour)); !errors.Is(err, ErrNotDeleted) {
		t.Fatalf("Restore: err = %v", err)
	}
}

// PurgeCutoff and PurgeDue agree: deleted before the cutoff is due, at the cutoff is not (it is the window end).
func TestPurgeCutoff(t *testing.T) {
	now := t0.Add(10 * 24 * time.Hour)
	cutoff := PurgeCutoff(now)
	if !cutoff.Equal(now.Add(-RestoreWindow)) {
		t.Fatalf("cutoff = %v", cutoff)
	}
	for _, tc := range []struct {
		deletedAt time.Time
		due       bool
	}{
		{cutoff.Add(-time.Millisecond), true},
		{cutoff, false},
		{cutoff.Add(time.Millisecond), false},
	} {
		a := extracted(t)
		if err := a.Delete(tc.deletedAt); err != nil {
			t.Fatal(err)
		}
		if got := a.PurgeDue(now); got != tc.due {
			t.Fatalf("deleted at cutoff%+v: PurgeDue = %v, want %v", tc.deletedAt.Sub(cutoff), got, tc.due)
		}
	}
}
