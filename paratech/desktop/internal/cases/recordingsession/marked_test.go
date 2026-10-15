package recordingsession

import (
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// RecoveryMarked stays open until the first Recover has stored its marks and closes before the assembly is done: a
// long assembly must not keep the student from recording (ADR-0009, TASK-022).
func TestRecoveryMarkedAfterTheMarkingPass(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	e.leftBehind("old", recording.StatusRecording, t0.Add(-time.Hour), chunk{payload: "a"})
	marked := e.svc.RecoveryMarked()
	if isClosed(marked) {
		t.Fatal("marked before any Recover")
	}

	e.repo.failNextSaves(shared.ErrStorageBusy) // the mark waits for a retry on the fake clock
	entered, release := e.rec.HoldFinalize()
	defer release()
	done := make(chan error, 1)
	go func() {
		_, err := e.svc.Recover(testCtx(t))
		done <- err
	}()
	if err := e.clock.AwaitSleepers(testCtx(t), 1); err != nil {
		t.Fatal(err)
	}
	if isClosed(marked) {
		t.Fatal("marked while the mark is not stored")
	}

	e.clock.Advance(retryStep)
	<-entered // the assembly runs
	if !isClosed(marked) {
		t.Fatal("not marked once the marks are stored")
	}
	if e.stored("old").Status() != recording.StatusRecoveryRequired {
		t.Fatalf("stored %s, want the mark before the assembly", e.stored("old").Status())
	}
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// A Recover that fails before the marks also ends the wait: a busy database must not block recording for good.
func TestRecoveryMarkedAfterAFailedRecover(t *testing.T) {
	e := newEnv(t, DefaultConfig())
	e.repo.listErr = shared.ErrStorageBusy
	if _, err := e.svc.Recover(testCtx(t)); !errors.Is(err, shared.ErrStorageBusy) {
		t.Fatalf("Recover = %v", err)
	}
	if !isClosed(e.svc.RecoveryMarked()) {
		t.Fatal("not marked after the failed Recover")
	}
}
