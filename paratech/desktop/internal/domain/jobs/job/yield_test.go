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
			if from == StatusRunning {
				p, _ := NewProgress(10, "1/10")
				if err := j.ReportProgress(p, now); err != nil {
					t.Fatal(err)
				}
			}
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
			if tt.failedBefore > 0 && j.LastError() == "" {
				t.Fatal("the failed step left no error to clear")
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

// A step that stored no progress since the claim cannot yield: the job stays RUNNING and unchanged, so the runner
// treats the step as failed and the job uses an attempt instead of looping forever.
func TestYieldWithoutProgress(t *testing.T) {
	p, _ := NewProgress(25, "1/4")
	tests := []struct {
		name  string
		setup func(j *Job) error
	}{
		{"nothing reported", func(j *Job) error { return j.Claim(t0) }},
		{"same progress reported again", func(j *Job) error {
			return firstErr(j.Claim(t0), j.ReportProgress(p, t0), j.Yield(t0), j.Claim(t0), j.ReportProgress(p, t0))
		}},
		{"restored RUNNING, not claimed here", func(j *Job) error {
			if err := j.Claim(t0); err != nil {
				return err
			}
			r, err := Restore(State{
				Version: 1, ID: j.ID(), Type: j.Type(), Status: StatusRunning, Key: j.Key(), Payload: j.Payload(), Attempts: 1,
				MaxAttempts: j.MaxAttempts(), NextRunAt: t0, Percent: 25, Checkpoint: "1/4",
				CreatedAt: t0, StartedAt: t0, UpdatedAt: t0,
			})
			if err != nil {
				return err
			}
			*j = *r
			return nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := newJob(t)
			if err := tt.setup(j); err != nil {
				t.Fatal(err)
			}
			before := *j
			err := j.Yield(t0.Add(time.Second))
			if !errors.Is(err, ErrNoProgress) || !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("Yield = %v, want ErrNoProgress", err)
			}
			if *j != before {
				t.Fatal("a refused yield changed the job")
			}
		})
	}
}
