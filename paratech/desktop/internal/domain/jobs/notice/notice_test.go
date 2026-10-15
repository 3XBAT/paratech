package notice

import (
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

func TestNotifiable(t *testing.T) {
	want := map[job.Status]bool{job.StatusCompleted: true, job.StatusFailed: true}
	for _, s := range job.Statuses() {
		if got := Notifiable(s); got != want[s] {
			t.Errorf("Notifiable(%s) = %v, want %v", s, got, want[s])
		}
	}
	if Notifiable("BOGUS") {
		t.Error("an unknown status is notifiable")
	}
	for _, s := range Statuses() {
		if !Notifiable(s) || !s.IsTerminal() {
			t.Errorf("Statuses lists %s, which is not a notifiable terminal status", s)
		}
	}
	if len(Statuses()) != len(want) {
		t.Errorf("Statuses = %v, want the %d notifiable ones", Statuses(), len(want))
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name   string
		id     shared.JobID
		typ    job.Type
		status job.Status
		now    time.Time
		ok     bool
	}{
		{"completed", "job-1", "stt", job.StatusCompleted, t0, true},
		{"failed", "job-1", "summarize", job.StatusFailed, t0, true},
		{"cancelled is not notifiable", "job-1", "stt", job.StatusCancelled, t0, false},
		{"running is not notifiable", "job-1", "stt", job.StatusRunning, t0, false},
		{"waiting is not notifiable", "job-1", "stt", job.StatusWaitingForPower, t0, false},
		{"retry wait is not notifiable", "job-1", "stt", job.StatusRetryWait, t0, false},
		{"unknown status", "job-1", "stt", "DONE", t0, false},
		{"empty job id", "", "stt", job.StatusCompleted, t0, false},
		{"malformed job id", "job 1", "stt", job.StatusCompleted, t0, false},
		{"empty type", "job-1", "", job.StatusCompleted, t0, false},
		{"type with a space", "job-1", "s t", job.StatusCompleted, t0, false},
		{"type with a control character", "job-1", "stt\n", job.StatusCompleted, t0, false},
		{"no time", "job-1", "stt", job.StatusCompleted, time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := New(tt.id, tt.typ, tt.status, tt.now)
			if !tt.ok {
				if !errors.Is(err, ErrInvalidNotice) || n != nil {
					t.Fatalf("New = %v, %v; want ErrInvalidNotice", n, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if n.Job() != tt.id || n.Type() != tt.typ || n.Status() != tt.status || !n.NotifiedAt().Equal(tt.now) {
				t.Fatalf("accessors = %s %s %s %v", n.Job(), n.Type(), n.Status(), n.NotifiedAt())
			}
		})
	}
}

func TestRestoreRoundTrip(t *testing.T) {
	n, err := New("job-1", "stt", job.StatusCompleted, t0)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Restore(n.State())
	if err != nil {
		t.Fatal(err)
	}
	if back.State() != n.State() {
		t.Fatalf("Restore(State()) = %+v, want %+v", back.State(), n.State())
	}

	bad := n.State()
	bad.Status = job.StatusCancelled
	if _, err := Restore(bad); !errors.Is(err, ErrInvalidNotice) {
		t.Fatalf("Restore of a CANCELLED notice: err = %v, want ErrInvalidNotice", err)
	}
}
