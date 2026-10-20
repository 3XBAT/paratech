package job

import (
	"errors"
	"testing"
	"time"
)

// TASK-097: only a RUNNING job yields; any other status refuses it and stays unchanged.
func TestYieldOnlyFromRunning(t *testing.T) {
	now := t0.Add(time.Hour)
	for _, from := range Statuses() {
		t.Run(from.String(), func(t *testing.T) {
			j := inStatus(t, from)
			before := *j
			err := j.Yield(now)
			if from == StatusRunning {
				if err != nil || j.Status() != StatusQueued {
					t.Fatalf("Yield = %v, status %s; want QUEUED", err, j.Status())
				}
				return
			}
			if !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("Yield = %v, want ErrInvalidTransition", err)
			}
			if *j != before {
				t.Fatal("a refused yield changed the job")
			}
		})
	}
}

// A yielded job is due at once behind the jobs due before it, gives its attempt back, keeps its progress and drops the
// error of an earlier failed step.
func TestYieldGoesToTheEndOfTheQueue(t *testing.T) {
	tests := []struct {
		name         string
		maxAttempts  int
		failedBefore int // failed attempts before the step that yields
		wantAttempts int
	}{
		{"first step", 3, 0, 0},
		{"after a failed step", 3, 1, 1},
		{"step on the last attempt", 2, 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j, err := New(Spec{ID: "j1", Type: "lecture_text", Key: "k", MaxAttempts: tt.maxAttempts}, t0)
			if err != nil {
				t.Fatal(err)
			}
			now := t0
			for range tt.failedBefore {
				now = now.Add(time.Minute)
				if err := firstErr(j.Claim(now), j.Retry(now, now, "network reset"), j.Requeue(now)); err != nil {
					t.Fatal(err)
				}
			}
			now = now.Add(time.Minute)
			if err := j.Claim(now); err != nil {
				t.Fatal(err)
			}
			p, _ := NewProgress(25, "1/4")
			if err := j.ReportProgress(p, now); err != nil {
				t.Fatal(err)
			}

			at := now.Add(time.Second)
			if err := j.Yield(at); err != nil {
				t.Fatal(err)
			}
			if j.Status() != StatusQueued || !j.NextRunAt().Equal(at) || !j.UpdatedAt().Equal(at) || j.Attempts() != tt.wantAttempts {
				t.Fatalf("status %s next %v attempts %d; want QUEUED at %v with %d attempts",
					j.Status(), j.NextRunAt(), j.Attempts(), at, tt.wantAttempts)
			}
			if j.Progress() != p || j.LastError() != "" || j.Cause() != CauseNone || !j.FinishedAt().IsZero() {
				t.Fatalf("progress %+v error %q cause %q finished %v", j.Progress(), j.LastError(), j.Cause(), j.FinishedAt())
			}
			if err := j.Claim(at); err != nil {
				t.Fatalf("the next step cannot be claimed: %v", err)
			}
		})
	}
}
