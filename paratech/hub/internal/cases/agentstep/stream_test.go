package agentstep_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"paratech/hub/internal/cases/agentstep"
	"paratech/hub/internal/domain/llm/services/toolcatalog"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/testenv"
)

// newStreamFixture is newFixture with the provider also serving StepStream and provider streaming set to on.
func newStreamFixture(t *testing.T, on bool, steps ...testenv.AgentProviderStep) fixture {
	t.Helper()
	sealer, err := testenv.Sealer(1)
	if err != nil {
		t.Fatal(err)
	}
	_, system, catalog := profiles(t)
	f := fixture{
		store: testenv.NewQuotaStore(), turns: testenv.NewAgentTurns(), provider: testenv.NewFakeAgentProvider(steps...),
		limiter: &testenv.FakeLimiter{}, clock: testenv.NewFakeClock(t0), sealer: sealer, logs: &syncBuffer{}, system: system,
		catalog: catalog,
	}
	cfg := config(t, 1_000_000)
	cfg.ProviderStream = on
	svc, err := agentstep.New(agentstep.Deps{
		Accounts: f.store, Turns: f.turns, Provider: f.provider, Streamer: f.provider, Limiter: f.limiter, Clock: f.clock,
		Random: testenv.Random{}, Models: models(t), Sealer: sealer,
		Log: slog.New(slog.NewTextHandler(f.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	return f
}

// deltas records the chunks of a StepStream; the use case calls it from one goroutine (-race checks it).
type deltas struct{ got []string }

func (d *deltas) add(c string)   { d.got = append(d.got, c) }
func (d *deltas) joined() string { return strings.Join(d.got, "") }

func (f fixture) stream(t *testing.T, ctx context.Context, cmd agentstep.Command) (agentstep.Result, *deltas, error) {
	t.Helper()
	d := &deltas{}
	res, err := f.svc.StepStream(ctx, cmd, d.add)
	return res, d, err
}

// TestStepStreamSendsDeltasThenTheResult: the reply chunks go out as the provider writes them, the result is the one
// Step builds (sealed, settled, stored, the model turn kept), and the log line names the stream without texts.
func TestStepStreamSendsDeltasThenTheResult(t *testing.T) {
	step := twoCalls(t)
	step.Chunks = []string{"Поиск", " занятий."}
	f := newStreamFixture(t, true, step)
	res, d, err := f.stream(t, t.Context(), command(alice, "ag:1"))
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(d.got) != "[Поиск  занятий.]" {
		t.Fatalf("deltas %q", d.got)
	}
	if len(res.Calls) != 2 || res.Replayed || res.InputTokens != 1200 || f.provider.Streams() != 1 {
		t.Fatalf("result %+v, streams %d", res, f.provider.Streams())
	}
	if err := f.sealer.Verify(binding(alice), modelCalls(res, t), res.Seal); err != nil {
		t.Fatalf("seal: %v", err)
	}
	if used, held := f.store.Totals(alice); used != 1280 || held != 0 || f.turns.Len(alice) != 1 {
		t.Fatalf("used %d held %d turns %d", used, held, f.turns.Len(alice))
	}
	logs := f.logs.String()
	if !strings.Contains(logs, "stream=true") || !strings.Contains(logs, "provider_stream=true") || !strings.Contains(logs, "chunks=2") ||
		!strings.Contains(logs, "outcome=completed") {
		t.Fatalf("log: %s", logs)
	}
	f.assertNoText(t)
}

// TestStepStreamAndStepAreOneRequest: either RPC replays the result the other stored, free and without a delta.
func TestStepStreamAndStepAreOneRequest(t *testing.T) {
	step := twoCalls(t)
	step.Chunks = []string{"Поиск занятий."}
	f := newStreamFixture(t, true, step, twoCalls(t))

	first, d, err := f.stream(t, t.Context(), command(alice, "ag:1"))
	if err != nil || len(d.got) != 1 {
		t.Fatalf("first %v, deltas %q", err, d.got)
	}
	again := f.step(t, command(alice, "ag:1"))
	if !again.Replayed || again.ReservationID != first.ReservationID || !bytes.Equal(again.Seal, first.Seal) {
		t.Fatalf("Step after StepStream: %+v", again)
	}

	f.step(t, command(alice, "ag:2"))
	replay, d, err := f.stream(t, t.Context(), command(alice, "ag:2"))
	if err != nil || !replay.Replayed || len(d.got) != 0 {
		t.Fatalf("StepStream after Step: %+v, %v, deltas %q", replay, err, d.got)
	}
	if len(f.provider.Calls()) != 2 {
		t.Fatalf("provider calls %d", len(f.provider.Calls()))
	}
	if used, _ := f.store.Totals(alice); used != 2*1280 {
		t.Fatalf("used %d", used)
	}
}

// TestStepStreamFailureAfterDeltas: a failure may follow the chunks; it is the error Step gives, with no result.
func TestStepStreamFailureAfterDeltas(t *testing.T) {
	u, _ := values.NewUsage(100, 20)
	cases := map[string]struct {
		step    testenv.AgentProviderStep
		charged int64
	}{
		"invalid calls": {testenv.AgentProviderStep{
			Answer: values.NewAgentAnswer([]values.ToolCall{toolCall(t, "toolu_1", "reply", `{"text":5}`)}, u, true),
		}, 120},
		"cut stream": {testenv.AgentProviderStep{Err: &values.ProviderError{Kind: values.KindTransient, Reason: "answer interrupted"}}, 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tc.step.Chunks = []string{"черновик"}
			f := newStreamFixture(t, true, tc.step)
			res, d, err := f.stream(t, t.Context(), command(alice, "ag:1"))
			var failed *agentstep.ProviderFailedError
			if !errors.As(err, &failed) || res.ReservationID != "" || len(res.Calls) != 0 {
				t.Fatalf("res %+v, err %v", res, err)
			}
			if d.joined() != "черновик" {
				t.Fatalf("deltas %q", d.got)
			}
			if used, held := f.store.Totals(alice); used != tc.charged || held != 0 {
				t.Fatalf("used %d held %d", used, held)
			}
		})
	}
}

// TestStepStreamClientGone: a client that leaves mid-stream does not stop the call; it is settled once in full and
// stored, so the same key replays it free.
func TestStepStreamClientGone(t *testing.T) {
	wait, entered := make(chan struct{}), make(chan struct{})
	step := twoCalls(t)
	step.Chunks, step.Wait, step.Entered = []string{"Поиск"}, wait, entered
	f := newStreamFixture(t, true, step)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, _, err := f.stream(t, ctx, command(alice, "ag:1"))
		done <- err
	}()
	<-entered
	cancel()
	close(wait)
	if err := <-done; err != nil {
		t.Fatalf("the started step failed after the cancel: %v", err)
	}
	res, d, err := f.stream(t, t.Context(), command(alice, "ag:1"))
	if err != nil || !res.Replayed || len(d.got) != 0 || len(f.provider.Calls()) != 1 {
		t.Fatalf("retry %+v, %v, deltas %q", res, err, d.got)
	}
	if used, held := f.store.Totals(alice); used != 1280 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
	if rs := f.store.Reservations(alice); len(rs) != 1 || rs[0].State != account.StateSettled {
		t.Fatalf("reservations %+v", rs)
	}
}

// TestStepStreamWithProviderStreamingOff: the unary provider call and no delta, the result as with it on.
func TestStepStreamWithProviderStreamingOff(t *testing.T) {
	step := twoCalls(t)
	step.Chunks = []string{"never sent"}
	f := newStreamFixture(t, false, step)
	res, d, err := f.stream(t, t.Context(), command(alice, "ag:1"))
	if err != nil || len(res.Calls) != 2 || len(d.got) != 0 || f.provider.Streams() != 0 {
		t.Fatalf("res %+v, %v, deltas %q, streams %d", res, err, d.got, f.provider.Streams())
	}
	if strings.Contains(f.logs.String(), "provider_stream=true") || !strings.Contains(f.logs.String(), "stream=true") {
		t.Fatalf("log: %s", f.logs.String())
	}
}

// TestStepStreamIsBounded: at most MaxReplyTextRunes characters go out, cut between characters.
func TestStepStreamIsBounded(t *testing.T) {
	step := twoCalls(t)
	for range toolcatalog.MaxReplyTextRunes/100 + 5 {
		step.Chunks = append(step.Chunks, strings.Repeat("я", 99)+"😀")
	}
	f := newStreamFixture(t, true, step)
	_, d, err := f.stream(t, t.Context(), command(alice, "ag:1"))
	if err != nil {
		t.Fatal(err)
	}
	if got := d.joined(); utf8.RuneCountInString(got) != toolcatalog.MaxReplyTextRunes || !utf8.ValidString(got) {
		t.Fatalf("%d characters streamed", utf8.RuneCountInString(got))
	}
}

// TestStepStreamTextAnswer: the text of an answer without calls streams and becomes the reply card of the result.
func TestStepStreamTextAnswer(t *testing.T) {
	step := textAnswer(t, values.StopEnd, "Горутина — лёгкий поток.")
	step.Chunks = []string{"Горутина", " — лёгкий поток."}
	f := newStreamFixture(t, true, step)
	res, d, err := f.stream(t, t.Context(), command(alice, "ag:1"))
	if err != nil || shownReply(t, res) != d.joined() {
		t.Fatalf("res %+v, %v, deltas %q", res, err, d.got)
	}
}

// TestConcurrentStepStreams: callers of both RPCs with one key make one provider call and one charge.
func TestConcurrentStepStreams(t *testing.T) {
	wait, entered := make(chan struct{}), make(chan struct{})
	step := twoCalls(t)
	step.Wait, step.Entered = wait, entered
	f := newStreamFixture(t, true, step)
	const n = 8
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.WithoutCancel(t.Context())
			if i%2 == 0 {
				_, errs[i] = f.svc.StepStream(ctx, command(alice, "ag:same"), func(string) {})
			} else {
				_, errs[i] = f.svc.Step(ctx, command(alice, "ag:same"))
			}
		}()
	}
	<-entered
	close(wait)
	wg.Wait()
	for i, err := range errs {
		if err != nil && !errors.Is(err, agentstep.ErrAttemptRunning) {
			t.Fatalf("caller %d: %v", i, err)
		}
	}
	if used, held := f.store.Totals(alice); len(f.provider.Calls()) != 1 || used != 1280 || held != 0 {
		t.Fatalf("calls %d used %d held %d", len(f.provider.Calls()), used, held)
	}
}

// TestNewNeedsAStreamerForProviderStreaming: provider streaming without a streaming provider is a configuration error.
func TestNewNeedsAStreamerForProviderStreaming(t *testing.T) {
	sealer, _ := testenv.Sealer(1)
	cfg := config(t, 10)
	cfg.ProviderStream = true
	_, err := agentstep.New(agentstep.Deps{
		Accounts: testenv.NewQuotaStore(), Turns: testenv.NewAgentTurns(), Provider: testenv.NewFakeAgentProvider(),
		Limiter: &testenv.FakeLimiter{}, Clock: testenv.NewFakeClock(t0), Random: testenv.Random{}, Models: models(t), Sealer: sealer,
		Log: slog.New(slog.DiscardHandler),
	}, cfg)
	if !errors.Is(err, agentstep.ErrInvalidConfig) {
		t.Fatalf("err %v", err)
	}
}
