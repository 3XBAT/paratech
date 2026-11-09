package answering

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/answer"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
)

const retryQuestion = "Что такое производная функции?"

// seqReader fails its calls with errs in order, then answers validAnswer (or out); each call gets partials first.
type seqReader struct {
	errs     []error
	out      string
	partials []string
	keys     []string
}

func (r *seqReader) AnswerQuestion(_ context.Context, req answer.Request, partial func(string)) (string, error) {
	n := len(r.keys)
	r.keys = append(r.keys, req.Key())
	if partial != nil {
		for _, p := range r.partials {
			partial(p)
		}
	}
	if n < len(r.errs) {
		return "", r.errs[n]
	}
	if r.out != "" {
		return r.out, nil
	}
	return validAnswer, nil
}

// fakeSleeper records the waits; during one it runs during, then returns the error of ctx like the real clock.
type fakeSleeper struct {
	waits  []time.Duration
	during func(ctx context.Context)
}

func (s *fakeSleeper) Sleep(ctx context.Context, d time.Duration) error {
	s.waits = append(s.waits, d)
	if s.during != nil {
		s.during(ctx)
	}
	return ctx.Err()
}

type retryEnv struct {
	svc     *Service
	reader  *seqReader
	sleeper *fakeSleeper
	answers *memAnswers
}

func newRetryEnv(t *testing.T, r *seqReader, withSleeper bool) *retryEnv {
	t.Helper()
	e := &retryEnv{reader: r, sleeper: &fakeSleeper{}, answers: &memAnswers{byID: map[shared.AnswerID]*answer.Answer{}}}
	d := Deps{Knowledge: &fakeKnowledge{texts: lessons()}, Reader: r, Answers: e.answers, Clock: fixedClock{}, IDs: &seqIDs{}}
	if withSleeper {
		d.Sleeper = e.sleeper
	}
	var err error
	if e.svc, err = New(d); err != nil {
		t.Fatal(err)
	}
	return e
}

func secs(n ...int) []time.Duration {
	out := make([]time.Duration, 0, len(n))
	for _, s := range n {
		out = append(out, time.Duration(s)*time.Second)
	}
	return out
}

// AskNow retries a transient failure under the same key after 5, 10, 20 s, never anything else.
func TestAskNowRetries(t *testing.T) {
	net := shared.ErrNetworkUnavailable
	tests := []struct {
		name   string
		errs   []error
		out    string
		want   error // nil: an answer
		reads  int
		waits  []time.Duration
		stored int
	}{
		{"failure, failure, success", []error{net, net}, "", nil, 3, secs(5, 10), 1},
		{"four failures", []error{net, net, net, net}, "", ErrNoConnection, 4, secs(5, 10, 20), 0},
		{"timeout then success", []error{fmt.Errorf("hub: %w", context.DeadlineExceeded)}, "", nil, 2, secs(5), 1},
		{"quota", []error{shared.ErrQuotaExceeded}, "", ErrQuota, 1, nil, 0},
		{"refused answer", []error{fmt.Errorf("provider: %w", answer.ErrInvalidAnswer)}, "", ErrReaderRefused, 1, nil, 0},
		{"unusable answer", nil, `{"claims":[],"send_to":"x"}`, ErrReaderRefused, 1, nil, 0},
		{"Hub not set up", []error{fmt.Errorf("hub: %w", shared.ErrServiceNotConfigured)}, "", ErrNoConnection, 1, nil, 0},
		{"permanent failure", []error{backoff.Permanent(errors.New("unauthenticated"))}, "", ErrNoConnection, 1, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRetryEnv(t, &seqReader{errs: tt.errs, out: tt.out}, true)
			got, err := e.svc.AskNow(context.Background(), retryQuestion, "", scope, nil)
			switch {
			case tt.want == nil && (err != nil || got.Answer == nil || got.Reused):
				t.Fatalf("AskNow = %+v, %v", got, err)
			case tt.want != nil && !errors.Is(err, tt.want):
				t.Fatalf("AskNow = %v, want %v", err, tt.want)
			}
			if len(e.reader.keys) != tt.reads || !slices.Equal(e.sleeper.waits, tt.waits) || len(e.answers.byID) != tt.stored {
				t.Fatalf("%d reads, waits %v, %d stored; want %d, %v, %d", len(e.reader.keys), e.sleeper.waits,
					len(e.answers.byID), tt.reads, tt.waits, tt.stored)
			}
			for _, k := range e.reader.keys {
				if k != e.reader.keys[0] {
					t.Fatalf("keys %v: a retry asked under another key", e.reader.keys)
				}
			}
		})
	}
}

// Ask (the tickets' job runs) and AskNow without a Sleeper read once.
func TestAskDoesNotRetry(t *testing.T) {
	e := newRetryEnv(t, &seqReader{errs: []error{shared.ErrNetworkUnavailable}}, true)
	if _, err := e.svc.Ask(context.Background(), retryQuestion, "", scope, nil); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("Ask = %v", err)
	}
	if len(e.reader.keys) != 1 || len(e.sleeper.waits) != 0 {
		t.Fatalf("Ask: %d reads, waits %v", len(e.reader.keys), e.sleeper.waits)
	}

	e = newRetryEnv(t, &seqReader{errs: []error{shared.ErrNetworkUnavailable}}, false)
	if _, err := e.svc.AskNow(context.Background(), retryQuestion, "", scope, nil); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("AskNow without a Sleeper = %v", err)
	}
	if len(e.reader.keys) != 1 {
		t.Fatalf("AskNow without a Sleeper: %d reads", len(e.reader.keys))
	}
}

// ctx ending during the wait returns its error at once: no further read.
func TestAskNowCancelledDuringWait(t *testing.T) {
	e := newRetryEnv(t, &seqReader{errs: []error{shared.ErrNetworkUnavailable}}, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.sleeper.during = func(context.Context) { cancel() }
	if _, err := e.svc.AskNow(ctx, retryQuestion, "", scope, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("AskNow = %v", err)
	}
	if len(e.reader.keys) != 1 || len(e.sleeper.waits) != 1 || len(e.answers.byID) != 0 {
		t.Fatalf("%d reads, waits %v, %d stored", len(e.reader.keys), e.sleeper.waits, len(e.answers.byID))
	}
}

// An answer stored under the key during the wait (a replayed step) is found before the next read.
func TestAskNowFindsAnswerStoredDuringWait(t *testing.T) {
	first := newRetryEnv(t, &seqReader{}, true)
	made, err := first.svc.Ask(context.Background(), retryQuestion, "", scope, nil)
	if err != nil {
		t.Fatal(err)
	}

	e := newRetryEnv(t, &seqReader{errs: []error{shared.ErrNetworkUnavailable}}, true)
	e.sleeper.during = func(context.Context) { e.answers.byID[made.Answer.ID()] = made.Answer }
	got, err := e.svc.AskNow(context.Background(), retryQuestion, "", scope, nil)
	if err != nil || !got.Reused || got.Answer.ID() != made.Answer.ID() {
		t.Fatalf("AskNow = %+v, %v", got, err)
	}
	if len(e.reader.keys) != 1 || e.answers.inserts != 0 {
		t.Fatalf("%d reads, %d inserts", len(e.reader.keys), e.answers.inserts)
	}
}

// Each retry drafts afresh: the same first draft of the retried read is passed again, not taken as unchanged.
func TestAskNowDraftRestarts(t *testing.T) {
	partials := []string{`{"claims":[{"text":"Произ`}
	e := newRetryEnv(t, &seqReader{errs: []error{shared.ErrNetworkUnavailable}, partials: partials}, true)
	sink := &draftLog{}
	if got, err := e.svc.AskNow(context.Background(), retryQuestion, "", scope, sink); err != nil || got.Answer == nil {
		t.Fatalf("AskNow = %+v, %v", got, err)
	}
	want := [][]string{{"Произ"}, {"Произ"}}
	if len(sink.got) != len(want) || !slices.Equal(sink.got[0], want[0]) || !slices.Equal(sink.got[1], want[1]) {
		t.Fatalf("drafts %q, want %q", sink.got, want)
	}
}
