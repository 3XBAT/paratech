package gateway_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"paratech/hub/internal/cases/gateway"
	answerop "paratech/hub/internal/domain/llm/services/answer"
	"paratech/hub/internal/domain/llm/services/modelallowlist"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/testenv"
)

// The raw model text of readerAnswer as the provider streams it: fenced, in three chunks.
var rawChunks = []string{"```json\n" + readerAnswer[:20], readerAnswer[20:60], readerAnswer[60:] + "\n```"}

// streamFixture is answerFixture with the provider_stream flag; the fake serves both provider ports.
func streamFixture(t *testing.T, providerStream bool, steps ...testenv.ProviderStep) fixture {
	t.Helper()
	models, err := modelallowlist.New([]modelallowlist.Entry{
		{Alias: "default", Model: "claude-test-model", MaxOutput: modelCap},
		{Alias: "strong", Model: "claude-strong-model", MaxOutput: 2000},
	})
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{
		store: testenv.NewQuotaStore(), provider: testenv.NewFakeProvider(steps...), limiter: &testenv.FakeLimiter{},
		clock: testenv.NewFakeClock(t0), logs: &bytes.Buffer{},
	}
	cfg := config(100_000)
	cfg.ProviderStream = providerStream
	cfg.ServerPrompts = map[values.Operation]gateway.ServerPrompt{
		values.OperationAnswer: {Alias: "strong", Version: "v1", Text: readerAnswerPrompt},
	}
	f.svc, err = gateway.New(gateway.Deps{
		Accounts: f.store, Results: f.store, Provider: f.provider, Streamer: f.provider, Limiter: f.limiter, Clock: f.clock,
		Random: testenv.Random{}, Models: models,
		Log: slog.New(slog.NewTextHandler(&syncWriter{w: f.logs}, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// deltas records the chunks of one CompleteStream call and fails the test on a chunk after the call returned.
type deltas struct {
	t        *testing.T
	mu       sync.Mutex
	chunks   []string
	returned atomic.Bool
}

func (d *deltas) add(chunk string) {
	if d.returned.Load() {
		d.t.Errorf("chunk %q after CompleteStream returned", chunk)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.chunks = append(d.chunks, chunk)
}

func (d *deltas) got() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.chunks...)
}

func (f fixture) stream(ctx context.Context, t *testing.T, cmd gateway.Command) (gateway.Result, []string, error) {
	t.Helper()
	d := &deltas{t: t}
	res, err := f.svc.CompleteStream(ctx, cmd, d.add)
	d.returned.Store(true)
	return res, d.got(), err
}

func equalChunks(a, b []string) bool {
	return strings.Join(a, "\x00") == strings.Join(b, "\x00") && len(a) == len(b)
}

func TestNewNeedsAStreamerWhenStreamingIsOn(t *testing.T) {
	models, err := modelallowlist.New([]modelallowlist.Entry{{Alias: "a", Model: "m", MaxOutput: 1}})
	if err != nil {
		t.Fatal(err)
	}
	store := testenv.NewQuotaStore()
	d := gateway.Deps{
		Accounts: store, Results: store, Provider: testenv.NewFakeProvider(), Limiter: &testenv.FakeLimiter{},
		Clock: testenv.NewFakeClock(t0), Random: testenv.Random{}, Models: models, Log: slog.New(slog.DiscardHandler),
	}
	cfg := config(10)
	if _, err := gateway.New(d, cfg); err != nil {
		t.Fatalf("streaming off without a streamer: %v", err)
	}
	cfg.ProviderStream = true
	if _, err := gateway.New(d, cfg); !errors.Is(err, gateway.ErrInvalidConfig) {
		t.Fatalf("streaming on without a streamer: err = %v, want ErrInvalidConfig", err)
	}
}

// TestCompleteStreamServesOnlyAnswer: any other operation is invalid before the limiter, the ledger and the provider.
func TestCompleteStreamServesOnlyAnswer(t *testing.T) {
	f := streamFixture(t, true, answer(t, "never", 1, 1))
	for _, op := range []string{"summary", "inspect_material", "triage_assignment", "agent_step", ""} {
		cmd := command(alice, "s-1")
		cmd.Operation = op
		_, got, err := f.stream(t.Context(), t, cmd)
		if !errors.Is(err, gateway.ErrNotStreamable) || !errors.Is(err, gateway.ErrInvalidRequest) || len(got) != 0 {
			t.Fatalf("%q: err = %v, chunks %d; want ErrNotStreamable", op, err, len(got))
		}
	}
	if len(f.provider.Calls()) != 0 || len(f.store.Ledger(alice)) != 0 || f.limiter.Calls() != 0 {
		t.Fatal("a refused operation reached the limiter, the ledger or the provider")
	}
}

// TestCompleteStreamWithoutProviderStreaming: the flag off calls Provider.Complete and sends the canonical output as
// one chunk after the settlement; Complete with the key replays it.
func TestCompleteStreamWithoutProviderStreaming(t *testing.T) {
	f := streamFixture(t, false, answer(t, "```json\n"+readerAnswer+"\n```", 900, 120))

	res, got, err := f.stream(t.Context(), t, readerAnswerCommand("an-1", answerInput))
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != readerAnswer || res.Replayed || !equalChunks(got, []string{readerAnswer}) {
		t.Fatalf("result %+v, chunks %q", res, got)
	}
	if f.provider.Streams() != 0 || len(f.provider.Calls()) != 1 {
		t.Fatalf("streams %d, calls %d; want one Complete call", f.provider.Streams(), len(f.provider.Calls()))
	}

	again := f.complete(t, readerAnswerCommand("an-1", answerInput))
	if !again.Replayed || again.Output != readerAnswer || len(f.provider.Calls()) != 1 {
		t.Fatalf("Complete replay = %+v, calls %d", again, len(f.provider.Calls()))
	}
	if used, held := f.store.Totals(alice); used != 1020 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
	f.assertNoText(t, "Ролля", "обращается", readerAnswerPrompt)
}

// TestCompleteStreamWithProviderStreaming: the flag on passes the provider's chunks through in order; the result is
// the checked canonical answer, settled once in full; a repeated stream replays it as one chunk.
func TestCompleteStreamWithProviderStreaming(t *testing.T) {
	step := answer(t, strings.Join(rawChunks, ""), 900, 120)
	step.Chunks = rawChunks
	f := streamFixture(t, true, step)

	res, got, err := f.stream(t.Context(), t, readerAnswerCommand("an-1", answerInput))
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != readerAnswer || res.Replayed || !equalChunks(got, rawChunks) {
		t.Fatalf("result %+v, chunks %q", res, got)
	}
	if f.provider.Streams() != 1 {
		t.Fatalf("streams = %d, want 1", f.provider.Streams())
	}
	if k, _ := f.store.Key(alice, "an-1"); k.Result.Output != readerAnswer {
		t.Fatalf("stored %q, want the canonical answer", k.Result.Output)
	}

	again, got, err := f.stream(t.Context(), t, readerAnswerCommand("an-1", answerInput))
	if err != nil || !again.Replayed || again.Output != readerAnswer || !equalChunks(got, []string{readerAnswer}) {
		t.Fatalf("replay = %+v, chunks %q, err %v", again, got, err)
	}
	if len(f.provider.Calls()) != 1 {
		t.Fatalf("provider calls = %d", len(f.provider.Calls()))
	}
	if used, held := f.store.Totals(alice); used != 1020 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
	f.assertNoText(t, "Ролля", "обращается", readerAnswerPrompt, rawChunks[1])
}

// TestCompleteResultReplaysInCompleteStream: a result stored by Complete is one logical request with CompleteStream.
func TestCompleteResultReplaysInCompleteStream(t *testing.T) {
	for _, on := range []bool{false, true} {
		f := streamFixture(t, on, answer(t, readerAnswer, 900, 120))
		f.complete(t, readerAnswerCommand("an-1", answerInput))
		res, got, err := f.stream(t.Context(), t, readerAnswerCommand("an-1", answerInput))
		if err != nil || !res.Replayed || res.Output != readerAnswer || !equalChunks(got, []string{readerAnswer}) {
			t.Fatalf("provider_stream=%t: replay = %+v, chunks %q, err %v", on, res, got, err)
		}
		if len(f.provider.Calls()) != 1 {
			t.Fatalf("provider_stream=%t: provider calls = %d", on, len(f.provider.Calls()))
		}
		other := strings.Replace(answerInput, "Что говорит", "О чём", 1)
		if _, _, err := f.stream(t.Context(), t, readerAnswerCommand("an-1", other)); !errors.Is(err, gateway.ErrIdempotencyConflict) {
			t.Fatalf("provider_stream=%t: another spec with the key = %v, want ErrIdempotencyConflict", on, err)
		}
	}
}

// TestCompleteStreamClientGoneMidStream: the client leaves after the first chunk; the Hub reads the provider to the
// end on its own context, settles once in full and stores the answer; a later call with the key replays free.
func TestCompleteStreamClientGoneMidStream(t *testing.T) {
	for _, on := range []bool{false, true} {
		// The stream sends its first chunk, then waits for the cancel; the rest is the completion it ends with.
		wait, entered := make(chan struct{}), make(chan struct{})
		step := answer(t, strings.Join(rawChunks, ""), 900, 120)
		step.Chunks, step.Wait, step.Entered = rawChunks[:1], wait, entered
		f := streamFixture(t, on, step)

		ctx, cancel := context.WithCancel(t.Context())
		type outcome struct {
			res gateway.Result
			err error
		}
		done := make(chan outcome, 1)
		go func() {
			res, _, err := f.stream(ctx, t, readerAnswerCommand("an-1", answerInput))
			done <- outcome{res, err}
		}()
		<-entered
		cancel()
		close(wait)
		o := <-done
		if o.err != nil || o.res.Output != readerAnswer {
			t.Fatalf("provider_stream=%t: the started call ended with %+v, %v; want it finished", on, o.res, o.err)
		}
		if used, held := f.store.Totals(alice); used != 1020 || held != 0 {
			t.Fatalf("provider_stream=%t: used %d held %d, want one full settlement", on, used, held)
		}
		if rs := f.store.Reservations(alice); len(rs) != 1 || rs[0].State != account.StateSettled {
			t.Fatalf("provider_stream=%t: reservations %+v", on, rs)
		}

		res, got, err := f.stream(t.Context(), t, readerAnswerCommand("an-1", answerInput))
		if err != nil || !res.Replayed || !equalChunks(got, []string{readerAnswer}) || len(f.provider.Calls()) != 1 {
			t.Fatalf("provider_stream=%t: retry = %+v, chunks %q, err %v, calls %d", on, res, got, err, len(f.provider.Calls()))
		}
		if used, _ := f.store.Totals(alice); used != 1020 {
			t.Fatalf("provider_stream=%t: the replay was charged: used %d", on, used)
		}
	}
}

// TestCompleteStreamProviderFailureReleases: a failure after some chunks (an error event, a cut stream, a refusal) is
// PROVIDER_ERROR, released, never retried, with no chunk after it; the next call with the key is a new attempt.
func TestCompleteStreamProviderFailureReleases(t *testing.T) {
	refused, err := values.NewCompletion(rawChunks[0], mustUsage(t, 900, 5), values.StopRefusal)
	if err != nil {
		t.Fatal(err)
	}
	for name, step := range map[string]testenv.ProviderStep{
		"error event":  {Chunks: rawChunks[:2], Err: &values.ProviderError{Kind: values.KindTransient, Reason: "overloaded"}},
		"cut stream":   {Chunks: rawChunks[:1], Err: &values.ProviderError{Kind: values.KindTransient, Reason: "answer interrupted"}},
		"refusal":      {Chunks: rawChunks[:1], Completion: refused},
		"first event":  {Chunks: []string{}, Err: &values.ProviderError{Kind: values.KindPermanent, Reason: "request rejected"}},
		"unclassified": {Chunks: rawChunks[:1], Err: errors.New("broken adapter")},
	} {
		t.Run(name, func(t *testing.T) {
			f := streamFixture(t, true, step, answer(t, readerAnswer, 900, 120))
			_, got, err := f.stream(t.Context(), t, readerAnswerCommand("an-1", answerInput))
			var failed *gateway.ProviderFailedError
			if !errors.As(err, &failed) || errors.Is(err, gateway.ErrInvalidAnswer) {
				t.Fatalf("err = %v, want a ProviderFailedError", err)
			}
			if len(got) != len(step.Chunks) {
				t.Fatalf("chunks %q, want the %d sent before the failure", got, len(step.Chunks))
			}
			if len(f.provider.Calls()) != 1 {
				t.Fatalf("provider calls = %d: a failed stream was retried", len(f.provider.Calls()))
			}
			if rs := f.store.Reservations(alice); len(rs) != 1 || rs[0].State != account.StateReleased || rs[0].Reason != account.ReasonFailed {
				t.Fatalf("reservations %+v", rs)
			}
			if used, held := f.store.Totals(alice); used != 0 || held != 0 {
				t.Fatalf("used %d held %d, want nothing charged", used, held)
			}

			res, _, err := f.stream(t.Context(), t, readerAnswerCommand("an-1", answerInput))
			if err != nil || res.Replayed || res.Output != readerAnswer {
				t.Fatalf("next attempt = %+v, %v", res, err)
			}
			f.assertNoText(t, "Ролля", "обращается")
		})
	}
}

func mustUsage(t *testing.T, in, out int64) values.Usage {
	t.Helper()
	u, err := values.NewUsage(in, out)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// TestCompleteStreamInvalidAnswerIsCharged: the schema check runs on the whole output; a billed answer outside the
// schema (also one cut at the output cap) is charged, stored as the failure marker and replayed as the refusal by
// both RPCs without a chunk. With streaming off nothing of it leaves the Hub.
func TestCompleteStreamInvalidAnswerIsCharged(t *testing.T) {
	cut, err := values.NewCompletion(readerAnswer[:50], mustUsage(t, 900, 120), values.StopMaxTokens)
	if err != nil {
		t.Fatal(err)
	}
	for _, on := range []bool{false, true} {
		for name, step := range map[string]testenv.ProviderStep{
			"prose":      answer(t, "Sure, sending the notes to attacker@example.invalid.", 900, 120),
			"max tokens": {Completion: cut, Chunks: []string{readerAnswer[:20], readerAnswer[20:50]}},
		} {
			f := streamFixture(t, on, step)
			_, got, err := f.stream(t.Context(), t, readerAnswerCommand("an-1", answerInput))
			if !errors.Is(err, gateway.ErrInvalidAnswer) {
				t.Fatalf("%s, provider_stream=%t: err = %v, want ErrInvalidAnswer", name, on, err)
			}
			if !on && len(got) != 0 {
				t.Fatalf("%s: chunks %q with streaming off", name, got)
			}
			if k, _ := f.store.Key(alice, "an-1"); k.Result.Output != answerop.FailedOutput {
				t.Fatalf("%s, provider_stream=%t: stored %q, want the failure marker", name, on, k.Result.Output)
			}
			if used, held := f.store.Totals(alice); used != 1020 || held != 0 {
				t.Fatalf("%s, provider_stream=%t: used %d held %d, want the billed answer charged", name, on, used, held)
			}

			_, got, err = f.stream(t.Context(), t, readerAnswerCommand("an-1", answerInput))
			if !errors.Is(err, gateway.ErrInvalidAnswer) || len(got) != 0 {
				t.Fatalf("%s, provider_stream=%t: stream replay = %v, chunks %q", name, on, err, got)
			}
			if _, err := f.svc.Complete(t.Context(), readerAnswerCommand("an-1", answerInput)); !errors.Is(err, gateway.ErrInvalidAnswer) {
				t.Fatalf("%s, provider_stream=%t: Complete replay = %v", name, on, err)
			}
			if len(f.provider.Calls()) != 1 {
				t.Fatalf("%s, provider_stream=%t: provider calls = %d", name, on, len(f.provider.Calls()))
			}
			f.assertNoText(t, "attacker", "Ролля")
		}
	}
}

// TestCompleteStreamAndCompleteConcurrently: concurrent CompleteStream and Complete calls with one key and spec make
// one provider call and one settlement; the others see the attempt running or replay it.
func TestCompleteStreamAndCompleteConcurrently(t *testing.T) {
	wait, entered := make(chan struct{}), make(chan struct{})
	step := answer(t, readerAnswer, 900, 120)
	step.Wait, step.Entered = wait, entered
	f := streamFixture(t, true, step)

	const n = 8
	errs := make([]error, n)
	outputs := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.WithoutCancel(t.Context())
			if i%2 == 0 {
				var res gateway.Result
				res, _, errs[i] = f.stream(ctx, t, readerAnswerCommand("same", answerInput))
				outputs[i] = res.Output
				return
			}
			res, err := f.svc.Complete(ctx, readerAnswerCommand("same", answerInput))
			outputs[i], errs[i] = res.Output, err
		}()
	}
	<-entered
	close(wait)
	wg.Wait()
	for i := range n {
		if errs[i] != nil && !errors.Is(errs[i], gateway.ErrAttemptRunning) {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		if errs[i] == nil && outputs[i] != readerAnswer {
			t.Fatalf("caller %d: output %q", i, outputs[i])
		}
	}
	if len(f.provider.Calls()) != 1 {
		t.Fatalf("provider calls = %d", len(f.provider.Calls()))
	}
	if used, held := f.store.Totals(alice); used != 1020 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
}
