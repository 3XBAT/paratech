package proposal

import (
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

func TestAttaches(t *testing.T) {
	pinned := mustDigest(t, shaA, 11)
	tests := []struct {
		name string
		p    *Proposal
		d    Digest
		want bool
	}{
		{"validated send with that file", inStatus(t, outEmail, StatusValidated), pinned, true},
		{"another file", inStatus(t, outEmail, StatusValidated), mustDigest(t, shaB, 11), false},
		{"same hash, other size", inStatus(t, outEmail, StatusValidated), mustDigest(t, shaA, 12), false},
		{"no digest", inStatus(t, outEmail, StatusValidated), Digest{}, false},
		{"confirmed send", inStatus(t, outEmail, StatusConfirmed), pinned, false},
		{"rejected send", inStatus(t, outEmail, StatusRejected), Digest{}, false},
		{"deadline", inStatus(t, outDeadline, StatusValidated), pinned, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.Attaches(tt.d); got != tt.want {
				t.Fatalf("Attaches = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWaitsAsDeadline(t *testing.T) {
	due := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC) // outDeadline's due
	msk := time.FixedZone("MSK", 3*3600)
	tests := []struct {
		name  string
		p     *Proposal
		a     string
		title string
		due   time.Time
		want  bool
	}{
		{"same", inStatus(t, outDeadline, StatusValidated), "a1", "Algebra homework", due, true},
		{"same instant in another zone", inStatus(t, outDeadline, StatusValidated), "a1", " Algebra homework ", due.In(msk), true},
		{"another due", inStatus(t, outDeadline, StatusValidated), "a1", "Algebra homework", due.Add(time.Hour), false},
		{"another title", inStatus(t, outDeadline, StatusValidated), "a1", "Geometry", due, false},
		{"another assignment", inStatus(t, outDeadline, StatusValidated), "a2", "Algebra homework", due, false},
		{"already confirmed", inStatus(t, outDeadline, StatusConfirmed), "a1", "Algebra homework", due, false},
		{"declined", inStatus(t, outDeadline, StatusDeclined), "a1", "Algebra homework", due, false},
		{"a send", inStatus(t, outEmail, StatusValidated), "a1", "", time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.WaitsAsDeadline(shared.AssignmentID(tt.a), tt.title, tt.due); got != tt.want {
				t.Fatalf("WaitsAsDeadline = %v, want %v", got, tt.want)
			}
		})
	}
}
