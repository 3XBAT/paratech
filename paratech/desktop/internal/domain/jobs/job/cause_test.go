package job

import (
	"errors"
	"testing"
	"time"
)

func TestCauses(t *testing.T) {
	for _, c := range Causes() {
		if !c.Valid() {
			t.Errorf("%q is not valid", c)
		}
	}
	if Cause("bogus").Valid() {
		t.Error("an unknown cause is valid")
	}
	if Causes()[0] != CauseNone {
		t.Error("CauseNone must come first")
	}
}

func TestMethodsRecordCause(t *testing.T) {
	tests := []struct {
		name       string
		act        func(j *Job) error
		wantStatus Status
		wantCause  Cause
	}{
		{"wait not configured", func(j *Job) error {
			return j.WaitForCause(StatusWaitingForNetwork, CauseServiceNotConfigured, t0)
		}, StatusWaitingForNetwork, CauseServiceNotConfigured},
		{"wait without cause", func(j *Job) error { return j.WaitFor(StatusWaitingForNetwork, t0) }, StatusWaitingForNetwork, CauseNone},
		{"retry unavailable", func(j *Job) error {
			return j.RetryCause(t0, t0.Add(time.Minute), "hub unavailable", CauseServiceUnavailable)
		}, StatusRetryWait, CauseServiceUnavailable},
		{"retry without cause", func(j *Job) error { return j.Retry(t0, t0.Add(time.Minute), "x") }, StatusRetryWait, CauseNone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			j := inStatus(t, StatusRunning)
			if err := tc.act(j); err != nil {
				t.Fatal(err)
			}
			if j.Status() != tc.wantStatus || j.Cause() != tc.wantCause {
				t.Fatalf("got %s %q, want %s %q", j.Status(), j.Cause(), tc.wantStatus, tc.wantCause)
			}
		})
	}
}

// A cause describes one wait or retry: any later transition clears it, so a stale cause never labels another status.
func TestTransitionClearsCause(t *testing.T) {
	tests := []struct {
		name string
		act  func(j *Job) error
	}{
		{"requeue", func(j *Job) error { return j.Requeue(t0) }},
		{"cancel", func(j *Job) error { return j.Cancel(t0) }},
		{"requeue and claim", func(j *Job) error { return firstErr(j.Requeue(t0), j.Claim(t0)) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			j := inStatus(t, StatusRunning)
			if err := j.WaitForCause(StatusWaitingForNetwork, CauseServiceNotConfigured, t0); err != nil {
				t.Fatal(err)
			}
			if err := tc.act(j); err != nil {
				t.Fatal(err)
			}
			if j.Cause() != CauseNone {
				t.Fatalf("cause %q kept in %s", j.Cause(), j.Status())
			}
		})
	}
}

func TestExhaustedRetryKeepsCause(t *testing.T) {
	j, err := New(Spec{ID: "j", Type: "x", Key: "k", MaxAttempts: 1}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Claim(t0); err != nil {
		t.Fatal(err)
	}
	if err := j.RetryCause(t0, t0, "hub unavailable", CauseServiceUnavailable); err != nil {
		t.Fatal(err)
	}
	if j.Status() != StatusFailed || j.Cause() != CauseServiceUnavailable {
		t.Fatalf("got %s %q", j.Status(), j.Cause())
	}
}

func TestUnknownCauseIsRefused(t *testing.T) {
	j := inStatus(t, StatusRunning)
	if err := j.WaitForCause(StatusWaitingForNetwork, "bogus", t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("wait: %v", err)
	}
	if err := j.RetryCause(t0, t0, "x", "bogus"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("retry: %v", err)
	}
	if j.Status() != StatusRunning {
		t.Fatalf("status changed to %s", j.Status())
	}
}
