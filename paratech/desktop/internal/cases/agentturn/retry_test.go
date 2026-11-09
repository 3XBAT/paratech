package agentturn

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
)

// fakeSleeper records the waits. With block set a wait signals entered and returns only when its ctx ends (a «Стоп»
// or the caller), so a test sees that nothing but the wake ends it; else it returns at once.
type fakeSleeper struct {
	mu      sync.Mutex
	waits   []time.Duration
	block   bool
	entered chan struct{}
	onSleep func() // when set, runs at the start of every wait
}

func (f *fakeSleeper) Sleep(ctx context.Context, d time.Duration) error {
	f.mu.Lock()
	f.waits = append(f.waits, d)
	block, hook := f.block, f.onSleep
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	if !block {
		return nil
	}
	f.entered <- struct{}{}
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeSleeper) got() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.waits)
}

func blocking() *fakeSleeper { return &fakeSleeper{block: true, entered: make(chan struct{}, 1)} }

func newRetryEnv(t *testing.T, sl *fakeSleeper, p *recProgress) *env {
	t.Helper()
	e := newEnvDeps(t, func(d *Deps) {
		d.Sleeper = sl
		if p != nil {
			d.Progress = p
		}
	})
	if p != nil {
		p.threads = e.threads
	}
	return e
}

// Errors as hubclient returns them: a request that never reached the Hub, and a provider failure the Hub answered
// (a cut provider stream is a retryable PROVIDER_ERROR).
var (
	errTransport = backoff.Transient(shared.ErrNetworkUnavailable)
	errProvider  = fmt.Errorf("%w: %w: %w", backoff.ErrTransient, shared.ErrServiceUnavailable, values.ErrHubFailure)
)

// A transient failure is retried by itself under the same key after 5 s, then 10 s; the panel learns each attempt.
func TestAutoRetrySameKey(t *testing.T) {
	sl, p := &fakeSleeper{}, &recProgress{}
	e := newRetryEnv(t, sl, p)
	e.model.fail(errTransport).fail(errProvider).then(raw("r1", "reply", `{"text":"готово"}`))

	st := ask(t, e, "мои предметы")
	if st.Status != thread.StatusDone {
		t.Fatalf("after two failures and an answer = %+v", st)
	}
	reqs := e.model.requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d", len(reqs))
	}
	for _, r := range reqs {
		if r.Key != "ag:th1:1:1" {
			t.Fatalf("keys = %q %q %q, want one key", reqs[0].Key, reqs[1].Key, reqs[2].Key)
		}
	}
	if got := sl.got(); !slices.Equal(got, []time.Duration{5 * time.Second, 10 * time.Second}) {
		t.Fatalf("waits = %v", got)
	}
	// Each attempt: once as the wait begins, once as the retried call goes; Started only for the first call.
	if got := p.attempts(); !slices.Equal(got, []int{1, 1, 2, 2}) {
		t.Fatalf("retry updates = %v", got)
	}
	if steps, _ := p.got(); len(steps) != 1 {
		t.Fatalf("started updates = %d", len(steps))
	}
}

// The next step of the request has retries of its own.
func TestAutoRetryCountsPerStep(t *testing.T) {
	sl := &fakeSleeper{}
	e := newRetryEnv(t, sl, nil)
	e.model.fail(errTransport).then(raw("c1", "list_subjects", `{}`)).
		fail(errTransport).fail(errTransport).fail(errTransport).then(raw("r1", "reply", `{"text":"готово"}`))
	st := ask(t, e, "мои предметы")
	st, err := e.svc.Submit(t.Context(), SubmitInput{Turn: ref(st), Outcomes: []Outcome{okResult("c1", `{}`)}})
	if err != nil || st.Status != thread.StatusDone {
		t.Fatalf("Submit = %+v, %v", st, err)
	}
	want := []time.Duration{5 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second}
	if got := sl.got(); !slices.Equal(got, want) {
		t.Fatalf("waits = %v, want %v", got, want)
	}
}

// After three retries the request stops: failed when the last failure came from the Hub, offline otherwise; either
// is «Повторить» under a new key.
func TestAutoRetryGivesUp(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want values.StopReason
	}{
		{"never reached the hub", errTransport, values.StopOffline},
		{"timeout", backoff.Transient(shared.ErrServiceUnavailable), values.StopOffline},
		{"provider cut", errProvider, values.StopFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sl := &fakeSleeper{}
			e := newRetryEnv(t, sl, nil)
			e.model.fail(tt.err).fail(tt.err).fail(tt.err).fail(tt.err).then(raw("r1", "reply", `{"text":"готово"}`))

			st := ask(t, e, "мои предметы")
			if st.Status != thread.StatusStopped || st.StopReason != tt.want {
				t.Fatalf("after four failures = %s %s, want stopped %s", st.Status, st.StopReason, tt.want)
			}
			if got := sl.got(); !slices.Equal(got, []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second}) {
				t.Fatalf("waits = %v", got)
			}
			reqs := e.model.requests()
			if len(reqs) != 4 || reqs[3].Key != "ag:th1:1:1" {
				t.Fatalf("requests = %d, last key %q", len(reqs), reqs[len(reqs)-1].Key)
			}

			st, err := e.svc.Retry(t.Context(), ref(st))
			if err != nil || st.Status != thread.StatusDone {
				t.Fatalf("Retry = %+v, %v", st, err)
			}
			if last := e.model.last(t); last.Key != "ag:th1:1:1:r1" {
				t.Fatalf("key of Retry = %q", last.Key)
			}
		})
	}
}

// Failures retrying cannot fix within seconds are never retried by themselves.
func TestNoAutoRetry(t *testing.T) {
	tests := []struct {
		name string
		step func(m *fakeModel) *fakeModel
		want values.StopReason
	}{
		{"quota", func(m *fakeModel) *fakeModel {
			return m.fail(fmt.Errorf("%w: %w", shared.ErrQuotaExceeded, values.ErrHubFailure))
		}, values.StopQuota},
		{"permanent", func(m *fakeModel) *fakeModel { return m.fail(backoff.Permanent(errors.New("refused"))) }, values.StopFailed},
		{"unusable answer", func(m *fakeModel) *fakeModel { return m.then() }, values.StopFailed},
		{"not configured", func(m *fakeModel) *fakeModel {
			return m.fail(backoff.Transient(fmt.Errorf("%w: %w", shared.ErrServiceNotConfigured, shared.ErrNetworkUnavailable)))
		}, values.StopOffline},
		{"expired twice", func(m *fakeModel) *fakeModel {
			gone := backoff.Permanent(values.ErrTurnGone)
			return m.fail(gone).fail(gone)
		}, values.StopExpired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sl := &fakeSleeper{}
			e := newRetryEnv(t, sl, nil)
			tt.step(e.model)
			st := ask(t, e, "мои предметы")
			if st.Status != thread.StatusStopped || st.StopReason != tt.want {
				t.Fatalf("stop = %s %s, want %s", st.Status, st.StopReason, tt.want)
			}
			if got := sl.got(); len(got) != 0 {
				t.Fatalf("waits = %v, want none", got)
			}
		})
	}
}

// «Стоп» during the wait ends it at once (the fake waits only for the wake) and the stored request is STOPPED/user.
func TestStopDuringRetryWait(t *testing.T) {
	sl := blocking()
	e := newRetryEnv(t, sl, nil)
	e.model.fail(errTransport)

	type result struct {
		st  Step
		err error
	}
	done := make(chan result, 1)
	go func() {
		st, err := e.svc.Ask(t.Context(), AskInput{Scope: subjectScope(t), Text: "мои предметы"})
		done <- result{st, err}
	}()
	<-sl.entered

	stopped, err := e.svc.Stop(t.Context(), TurnRef{Thread: "th1", Turn: 1})
	if err != nil || stopped.StopReason != values.StopUser {
		t.Fatalf("Stop = %+v, %v", stopped, err)
	}
	var r result
	select {
	case r = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the wait did not end on «Стоп»")
	}
	if r.err != nil || r.st.Status != thread.StatusStopped || r.st.StopReason != values.StopUser {
		t.Fatalf("Ask after «Стоп» = %+v, %v", r.st, r.err)
	}
	th, _ := e.threads.Get(t.Context(), "th1")
	if turn, _ := th.Turn(); turn.Status() != thread.StatusStopped || turn.StopReason() != values.StopUser || !turn.StepOpen() {
		t.Fatalf("stored = %s %s open %v", turn.Status(), turn.StopReason(), turn.StepOpen())
	}
	if n := len(e.model.requests()); n != 1 {
		t.Fatalf("requests = %d, want no retry after «Стоп»", n)
	}
	// «Продолжить» after the student's stop replays the same key.
	e.model.then(raw("r1", "reply", `{"text":"готово"}`))
	if st, err := e.svc.Continue(t.Context(), ref(r.st)); err != nil || st.Status != thread.StatusDone {
		t.Fatalf("Continue = %+v, %v", st, err)
	}
	if last := e.model.last(t); last.Key != "ag:th1:1:1" {
		t.Fatalf("key of Continue = %q", last.Key)
	}
}

// «Отмена» during the wait wakes it as «Стоп» does; the request ends cancelled, without an error and a retry.
func TestCancelDuringRetryWait(t *testing.T) {
	sl := blocking()
	e := newRetryEnv(t, sl, nil)
	e.model.fail(errTransport)

	type result struct {
		st  Step
		err error
	}
	done := make(chan result, 1)
	go func() {
		st, err := e.svc.Ask(t.Context(), AskInput{Scope: subjectScope(t), Text: "мои предметы"})
		done <- result{st, err}
	}()
	<-sl.entered

	if st, err := e.svc.Cancel(t.Context(), TurnRef{Thread: "th1", Turn: 1}); err != nil || st.Status != thread.StatusCancelled {
		t.Fatalf("Cancel = %+v, %v", st, err)
	}
	var r result
	select {
	case r = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the wait did not end on «Отмена»")
	}
	if r.err != nil || r.st.Status != thread.StatusCancelled {
		t.Fatalf("Ask after «Отмена» = %+v, %v", r.st, r.err)
	}
	if n := len(e.model.requests()); n != 1 {
		t.Fatalf("requests = %d, want no retry after «Отмена»", n)
	}
}

// A failed read of the stored request during the wait stops the request offline: it never stays PLANNING without a
// loop (TASK-171).
func TestRetryWaitReadFails(t *testing.T) {
	sl := &fakeSleeper{}
	e := newRetryEnv(t, sl, nil)
	sl.onSleep = func() {
		e.threads.mu.Lock()
		e.threads.failGet = errors.New("disk read failed")
		e.threads.mu.Unlock()
	}
	e.model.fail(errTransport).then(raw("r1", "reply", `{"text":"готово"}`))

	st, err := e.svc.Ask(t.Context(), AskInput{Scope: subjectScope(t), Text: "мои предметы"})
	if err == nil || !errors.Is(err, shared.ErrNetworkUnavailable) {
		t.Fatalf("Ask = %+v, %v, want the failure", st, err)
	}
	if st.Status != thread.StatusStopped || st.StopReason != values.StopOffline {
		t.Fatalf("step = %s %s", st.Status, st.StopReason)
	}
	th, _ := e.threads.Get(t.Context(), "th1")
	if turn, _ := th.Turn(); turn.Status() != thread.StatusStopped || turn.StopReason() != values.StopOffline || !turn.StepOpen() {
		t.Fatalf("stored = %s %s open %v", turn.Status(), turn.StopReason(), turn.StepOpen())
	}
	if n := len(e.model.requests()); n != 1 {
		t.Fatalf("requests = %d, want no retry after the failed read", n)
	}
}

// The wait holds no lock: a request of another chat runs to its end meanwhile.
func TestAskOfAnotherChatDuringRetryWait(t *testing.T) {
	sl := blocking()
	e := newRetryEnv(t, sl, nil)
	for _, id := range []shared.ThreadID{"a", "b"} {
		th, err := thread.New(id, e.clock.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err := e.threads.Save(t.Context(), th); err != nil {
			t.Fatal(err)
		}
	}
	e.model.fail(errTransport).then(raw("r1", "reply", `{"text":"другой чат"}`))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := e.svc.Ask(ctx, AskInput{ThreadID: "a", Scope: subjectScope(t), Text: "мои предметы"})
		done <- err
	}()
	<-sl.entered

	other := make(chan Step, 1)
	go func() {
		st, err := e.svc.Ask(t.Context(), AskInput{ThreadID: "b", Scope: subjectScope(t), Text: "привет"})
		if err != nil {
			t.Error(err)
		}
		other <- st
	}()
	select {
	case st := <-other:
		if st.Thread != "b" || st.Status != thread.StatusDone {
			t.Fatalf("other chat = %+v", st)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Ask of another chat waited for the retry wait")
	}

	// The caller going away ends the wait too: the request is stored stopped offline, not left running.
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Ask after its caller went away = %v", err)
	}
	th, _ := e.threads.Get(t.Context(), "a")
	if turn, _ := th.Turn(); turn.Status() != thread.StatusStopped || turn.StopReason() != values.StopOffline {
		t.Fatalf("stored = %s %s", turn.Status(), turn.StopReason())
	}
}
