package testenv

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/notice"
)

func TestFakeNotifier(t *testing.T) {
	n, err := notice.New("job-1", "stt", job.StatusCompleted, time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	f := &FakeNotifier{}
	ctx := context.Background()

	if !f.Ready() {
		t.Fatal("a new fake is not ready")
	}
	f.SetReady(false)
	if f.Ready() {
		t.Fatal("SetReady(false) left the fake ready")
	}
	f.SetReady(true)
	if !f.Ready() {
		t.Fatal("SetReady(true) left the fake not ready")
	}

	if err := f.Notify(ctx, n); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("toast failed")
	f.Fail(boom)
	if err := f.Notify(ctx, n); !errors.Is(err, boom) {
		t.Fatalf("failing fake: err = %v", err)
	}
	if err := f.Notify(ctx, nil); !errors.Is(err, ErrZeroCommand) {
		t.Fatalf("nil notice: err = %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := f.Notify(cancelled, n); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context: err = %v", err)
	}

	if calls := f.Calls(); len(calls) != 2 || calls[0] != n || calls[1] != n {
		t.Fatalf("calls = %v, want the two calls with a notice and a live context", calls)
	}
}
