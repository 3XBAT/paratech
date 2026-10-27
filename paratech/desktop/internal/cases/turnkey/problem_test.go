package turnkey

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/shared"
)

// TASK-226: a reader call that did not go through keeps its problem in the stored work, so a later read (another
// session) shows it; a recorded answer clears it.
func TestTriageKeepsTheProblemOfTheCall(t *testing.T) {
	tests := []struct {
		name string
		src  work.Source
		fail func(r *fakeReader)
		err  error
		want work.Problem
	}{
		{"no connection", work.Source{Assignment: "a1"}, func(r *fakeReader) { r.fail(errors.New("no network")) },
			ErrReaderUnavailable, work.ProblemOffline},
		{"quota", work.Source{Assignment: "a1"}, func(r *fakeReader) { r.fail(shared.ErrQuotaExceeded) }, ErrQuota,
			work.ProblemQuota},
		{"refused answer", work.Source{Assignment: "a1"}, func(r *fakeReader) { r.then(`{"work_type":"poem"}`) },
			ErrReaderRefused, work.ProblemRefused},
		{"no text", work.Source{Material: "m3"}, func(*fakeReader) {}, ErrNoText, work.ProblemNoText},
		{"file gone", work.Source{Material: "m5"}, func(*fakeReader) {}, ErrSourceNotFound, work.ProblemSourceGone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.works.sources["m5"] = true // the work exists, its file no longer does
			tt.fail(e.reader)
			ctx := t.Context()

			st, err := e.svc.Triage(ctx, tt.src)
			if !errors.Is(err, tt.err) || st.View.LastProblem != tt.want || st.View.Status != work.StatusTriage {
				t.Fatalf("Triage = %v, problem %q, status %s", err, st.View.LastProblem, st.View.Status)
			}
			if v, err := e.svc.Work(ctx, st.View.ID); err != nil || v.LastProblem != tt.want || v.Version != st.View.Version {
				t.Fatalf("Work later = %q v%d, %v; want %q v%d", v.LastProblem, v.Version, err, tt.want, st.View.Version)
			}
		})
	}
}

func TestSuccessfulTriageClearsTheProblem(t *testing.T) {
	e := newEnv(t)
	e.reader.fail(errors.New("no network")).fail(shared.ErrQuotaExceeded).then(out(work.FeasibleFull, 0))
	ctx := t.Context()
	st, _ := e.svc.Triage(ctx, work.Source{Assignment: "a1"})
	v, err := e.svc.Retry(ctx, st.View.ID)
	if !errors.Is(err, ErrQuota) || v.LastProblem != work.ProblemQuota {
		t.Fatalf("second failure = %q, %v", v.LastProblem, err)
	}
	if v, err = e.svc.Retry(ctx, st.View.ID); err != nil || v.Status != work.StatusReview || v.LastProblem != "" {
		t.Fatalf("Retry = %s %q, %v", v.Status, v.LastProblem, err)
	}
	if got, _ := e.works.Get(ctx, st.View.ID); got.LastProblem() != "" {
		t.Fatalf("stored problem %q after an answer", got.LastProblem())
	}
}

// A problem that cannot be saved does not hide the error of the call.
func TestProblemNotSavedStillFails(t *testing.T) {
	e := newEnv(t)
	e.reader.fail(errors.New("no network"))
	ctx := t.Context()
	w, _ := work.New("w9", work.Source{Assignment: "a1"}, t0)
	if err := e.works.Save(ctx, w); err != nil {
		t.Fatal(err)
	}
	e.works.saveErr = errors.New("disk full")
	v, err := e.svc.Retry(ctx, "w9")
	if !errors.Is(err, ErrReaderUnavailable) || v.ID != "w9" || v.Version != 1 {
		t.Fatalf("Retry = %+v, %v", v, err)
	}
	e.works.saveErr = nil
	if got, _ := e.works.Get(ctx, "w9"); got.LastProblem() != "" {
		t.Fatalf("stored problem %q", got.LastProblem())
	}
}

// fakeSends answers LastSent with a fixed time or error and counts the calls.
type fakeSends struct {
	mu    sync.Mutex
	at    time.Time
	err   error
	calls int
}

func (f *fakeSends) LastSent(context.Context, shared.WorkID) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.at, f.err
}

// TASK-226: a read takes the newest SENT send of the work into it once; the same send read again saves nothing.
func TestWorkTakesTheLastSend(t *testing.T) {
	e := newEnv(t)
	sends := &fakeSends{}
	e.svc.sends = sends
	ctx := t.Context()
	w, _ := work.New("w9", work.Source{Assignment: "a1"}, t0)
	if err := e.works.Save(ctx, w); err != nil {
		t.Fatal(err)
	}
	stored := func() *work.Work {
		t.Helper()
		got, err := e.works.Get(ctx, "w9")
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	if v, err := e.svc.Work(ctx, "w9"); err != nil || !v.SentAt.IsZero() || stored().Version() != 1 {
		t.Fatalf("never sent: %v, %v, version %d", v.SentAt, err, stored().Version())
	}

	first := t0.Add(time.Hour)
	sends.at = first
	v, err := e.svc.Work(ctx, "w9")
	if err != nil || !v.SentAt.Equal(first) || v.Version != 2 || !v.UpdatedAt.Equal(t0) {
		t.Fatalf("sent: %v v%d updated %v, %v", v.SentAt, v.Version, v.UpdatedAt, err)
	}
	if got := stored(); !got.SentAt().Equal(first) || got.Version() != 2 {
		t.Fatalf("stored %v v%d", got.SentAt(), got.Version())
	}
	if v, err := e.svc.WorkOf(ctx, work.Source{Assignment: "a1"}); err != nil || !v.SentAt.Equal(first) || v.Version != 2 {
		t.Fatalf("WorkOf again = %v v%d, %v", v.SentAt, v.Version, err)
	}

	// The deliveries cannot be read, or the work cannot be saved: the read still answers.
	sends.err = errors.New("locked")
	if v, err := e.svc.Work(ctx, "w9"); err != nil || !v.SentAt.Equal(first) {
		t.Fatalf("unreadable sends: %v, %v", v.SentAt, err)
	}
	second := t0.Add(2 * time.Hour)
	sends.at, sends.err = second, nil
	e.works.saveErr = errors.New("disk full")
	if v, err := e.svc.Work(ctx, "w9"); err != nil || !v.SentAt.Equal(second) || v.Version != 2 {
		t.Fatalf("unsaved send: %v v%d, %v", v.SentAt, v.Version, err)
	}
	e.works.saveErr = nil
	if v, err := e.svc.Work(ctx, "w9"); err != nil || !v.SentAt.Equal(second) || stored().Version() != 3 {
		t.Fatalf("next read: %v, %v, version %d", v.SentAt, err, stored().Version())
	}

	// A work that cannot be sent never asks.
	calls := sends.calls
	m, _ := work.New("w10", work.Source{Material: "m2"}, t0)
	if err := e.works.Save(ctx, m); err != nil {
		t.Fatal(err)
	}
	if v, err := e.svc.Work(ctx, "w10"); err != nil || !v.SentAt.IsZero() || sends.calls != calls {
		t.Fatalf("a work of a file: %v, %v, %d calls", v.SentAt, err, sends.calls-calls)
	}
}
