package job

import (
	"errors"
	"testing"
	"time"
)

func TestRerun(t *testing.T) {
	later := t0.Add(5 * time.Minute)
	for _, s := range Statuses() {
		t.Run(string(s), func(t *testing.T) {
			old := inStatus(t, s)
			before := old.Snapshot()

			next, err := old.Rerun("j2", "stt:r1:after:j1", later)
			retried, retryErr := old.RetryFailed("j2", "stt:r1:after:j1", later)
			switch {
			case !s.IsTerminal():
				if !errors.Is(err, ErrStillActive) || !errors.Is(retryErr, ErrStillActive) || next != nil || retried != nil {
					t.Fatalf("Rerun = %v, RetryFailed = %v; want ErrStillActive", err, retryErr)
				}
			case s == StatusCompleted:
				if err != nil || !errors.Is(retryErr, ErrNotFailed) || retried != nil {
					t.Fatalf("Rerun = %v, RetryFailed = %v; want a rerun and ErrNotFailed", err, retryErr)
				}
			default:
				if err != nil || retryErr != nil {
					t.Fatalf("Rerun = %v, RetryFailed = %v", err, retryErr)
				}
			}
			if old.Snapshot() != before || old.Status() != s {
				t.Fatal("the old job changed")
			}
			if next == nil {
				return
			}
			if next.ID() != "j2" || next.Key() != "stt:r1:after:j1" || next.Type() != old.Type() ||
				next.Payload() != old.Payload() || next.MaxAttempts() != old.MaxAttempts() ||
				next.Status() != StatusQueued || next.Attempts() != 0 || next.Version() != 0 ||
				!next.CreatedAt().Equal(later) || !next.NextRunAt().Equal(later) || next.LastError() != "" {
				t.Fatalf("rerun job = %+v", next.Snapshot())
			}
		})
	}

	if _, err := inStatus(t, StatusFailed).Rerun("j2", "", t0); !errors.Is(err, ErrInvalidJob) {
		t.Fatalf("Rerun without a key = %v, want ErrInvalidJob", err)
	}
}
