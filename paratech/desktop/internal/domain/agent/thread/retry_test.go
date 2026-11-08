package thread

import (
	"errors"
	"testing"

	"paratech/desktop/internal/domain/agent/values"
)

// ADR-0026 addendum 2026-11-08: «Продолжить» with the same key only after the student's «Стоп» and the quota; an open
// step stopped failed or offline is retried under a new key.
func TestContinueAndRetryByReason(t *testing.T) {
	tests := []struct {
		reason values.StopReason
		cont   error // nil: Continue works and keeps the key
		retry  error // nil: Retry works and changes the key
	}{
		{reason: values.StopUser, retry: ErrInvalidTransition},
		{reason: values.StopQuota, retry: ErrInvalidTransition},
		{reason: values.StopOffline, cont: ErrRetryRequired},
		{reason: values.StopFailed, cont: ErrRetryRequired},
		{reason: values.StopExpired, cont: ErrRestartRequired, retry: ErrInvalidTransition},
	}
	for _, tt := range tests {
		t.Run(string(tt.reason), func(t *testing.T) {
			for _, op := range []string{"continue", "retry"} {
				th := newThread(t)
				key, err := th.BeginStep(at(2))
				if err != nil {
					t.Fatal(err)
				}
				if err := th.Stop(tt.reason, at(3)); err != nil {
					t.Fatal(err)
				}
				want, move, wantKey := tt.cont, th.Continue, key
				if op == "retry" {
					want, move, wantKey = tt.retry, th.Retry, key+":r1"
				}
				err = move(at(4))
				if !errors.Is(err, want) || (want == nil && err != nil) {
					t.Fatalf("%s = %v, want %v", op, err, want)
				}
				if want != nil {
					continue
				}
				if got, _ := th.StepKey(); got != wantKey {
					t.Fatalf("key after %s = %q, want %q", op, got, wantKey)
				}
				if turn, _ := th.Turn(); turn.Status() != StatusPlanning || turn.StopReason() != "" {
					t.Fatalf("after %s: %s %q", op, turn.Status(), turn.StopReason())
				}
			}
		})
	}
}

// Retry from offline uses the budget of the step like a retry after a failure; the next retry is the budget stop.
func TestRetryFromOfflineBudget(t *testing.T) {
	th := newThread(t)
	key, err := th.BeginStep(at(2))
	if err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= MaxRetries; n++ {
		if err := th.Stop(values.StopOffline, at(3)); err != nil {
			t.Fatal(err)
		}
		if err := th.Retry(at(3)); err != nil {
			t.Fatalf("retry %d: %v", n, err)
		}
		if turn, _ := th.Turn(); turn.Retry() != n {
			t.Fatalf("retries = %d, want %d", turn.Retry(), n)
		}
	}
	if got, _ := th.StepKey(); got != key+":r3" {
		t.Fatalf("key = %q", got)
	}
	if err := th.Stop(values.StopOffline, at(4)); err != nil {
		t.Fatal(err)
	}
	if err := th.Retry(at(4)); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("retry over the limit = %v", err)
	}
	if turn, _ := th.Turn(); turn.Status() != StatusStopped || turn.StopReason() != values.StopBudget {
		t.Fatalf("request = %s %s, want stopped by budget", turn.Status(), turn.StopReason())
	}
}

// A request stopped offline with its calls waiting for the Driver has no open step: Continue goes on, Retry has no
// step to send again.
func TestOfflineWithoutOpenStepContinues(t *testing.T) {
	th := newThread(t)
	stepWith(t, th, "c1")
	if err := th.Stop(values.StopOffline, at(4)); err != nil {
		t.Fatal(err)
	}
	if err := th.Retry(at(5)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("Retry without an open step = %v", err)
	}
	if err := th.Continue(at(5)); err != nil {
		t.Fatalf("Continue = %v", err)
	}
	if turn, _ := th.Turn(); turn.Status() == StatusStopped || turn.StepOpen() {
		t.Fatalf("status = %s, step open %v; want running without an open step", turn.Status(), turn.StepOpen())
	}
}
