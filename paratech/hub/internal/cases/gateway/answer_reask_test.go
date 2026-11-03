package gateway_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"paratech/hub/internal/cases/gateway"
	answerop "paratech/hub/internal/domain/llm/services/answer"
	"paratech/hub/internal/domain/llm/services/modelallowlist"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/testenv"
)

// answerReask is a server reask note of ANSWER (the real one is ops/answer/v2/reask.md).
const answerReask = "# Second call\nAnswer again with the whole JSON object."

// The three ways an ANSWER call runs: Complete, CompleteStream with provider_stream off, and on.
const (
	modeComplete  = "complete"
	modeStreamOff = "stream off"
	modeStreamOn  = "stream on"
)

var answerModes = []string{modeComplete, modeStreamOff, modeStreamOn}

// answerReaskFixture is streamFixture with the ANSWER reask note.
func answerReaskFixture(t *testing.T, providerStream bool, steps ...testenv.ProviderStep) fixture {
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
		values.OperationAnswer: {Alias: "strong", Version: "v2", Text: readerAnswerPrompt, Reask: answerReask},
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

// run calls the gateway in mode and returns the result, the chunks a stream got and the error.
func (f fixture) run(t *testing.T, mode, key, data string) (gateway.Result, []string, error) {
	t.Helper()
	if mode == modeComplete {
		res, err := f.svc.Complete(t.Context(), readerAnswerCommand(key, data))
		return res, nil, err
	}
	return f.stream(t.Context(), t, readerAnswerCommand(key, data))
}

// streamed is a provider step that streams out in three chunks.
func streamed(t *testing.T, out string, in, outTokens int64) testenv.ProviderStep {
	t.Helper()
	step := answer(t, out, in, outTokens)
	third := len(out) / 3
	step.Chunks = []string{out[:third], out[third : 2*third], out[2*third:]}
	return step
}

// TestAnswerInvalidAnswerIsAskedOnceMore: a billed answer the check refuses is asked once more in every mode, also
// after a streamed answer (TASK-366: the beta case of 03.11 had stream=true and no reask). The reask is one unary call
// with the note added to the server prompt and the same data; it sends no chunk, so a stream holds only the draft of the
// first answer and the result is the checked second one. Both calls are charged once in one reservation; a retry
// replays the result free.
func TestAnswerInvalidAnswerIsAskedOnceMore(t *testing.T) {
	const prose = "Теорема Ролля говорит, что производная обращается в ноль."
	for _, mode := range answerModes {
		t.Run(mode, func(t *testing.T) {
			f := answerReaskFixture(t, mode == modeStreamOn, streamed(t, prose, 900, 120), answer(t, readerAnswer, 950, 140))

			res, got, err := f.run(t, mode, "an-1", answerInput)
			if err != nil {
				t.Fatal(err)
			}
			if res.Output != readerAnswer || res.InputTokens != 1850 || res.OutputTokens != 260 || res.Replayed {
				t.Fatalf("result = %+v", res)
			}
			calls := f.provider.Calls()
			if len(calls) != 2 {
				t.Fatalf("provider calls = %d, want one reask", len(calls))
			}
			if calls[1].Instructions() != readerAnswerPrompt+"\n\n"+answerReask || calls[1].Data() != calls[0].Data() ||
				calls[1].Model() != calls[0].Model() || calls[1].MaxOutput() != calls[0].MaxOutput() {
				t.Fatalf("reask = %v, first %v", calls[1], calls[0])
			}
			wantStreams, wantChunks := 0, []string{readerAnswer}
			switch mode {
			case modeComplete:
				wantChunks = nil
			case modeStreamOn:
				// The draft of the first answer, then nothing: the reask is unary.
				wantStreams, wantChunks = 1, []string{prose[:len(prose)/3], prose[len(prose)/3 : 2*(len(prose)/3)], prose[2*(len(prose)/3):]}
			}
			if f.provider.Streams() != wantStreams || !equalChunks(got, wantChunks) {
				t.Fatalf("streams %d, chunks %q; want %d, %q", f.provider.Streams(), got, wantStreams, wantChunks)
			}
			if used, held := f.store.Totals(alice); used != 2110 || held != 0 {
				t.Fatalf("used %d held %d, want both calls charged once", used, held)
			}
			if rs := f.store.Reservations(alice); len(rs) != 1 {
				t.Fatalf("reservations = %d, want one", len(rs))
			}
			if logs := f.logs.String(); !strings.Contains(logs, "reask=accepted") || !strings.Contains(logs, "outcome=completed") {
				t.Fatalf("log: %s", logs)
			}

			again, _, err := f.run(t, mode, "an-1", answerInput)
			if err != nil || !again.Replayed || again.Output != readerAnswer || len(f.provider.Calls()) != 2 {
				t.Fatalf("replay = %+v, %v, provider calls %d", again, err, len(f.provider.Calls()))
			}
			f.assertNoText(t, "Ролля", "обращается", answerReask[:15])
		})
	}
}

// TestAnswerReaskOnlyOnce: a second refused answer is the refusal in every mode: charged with both usages, stored as
// the failure marker and replayed without a third call.
func TestAnswerReaskOnlyOnce(t *testing.T) {
	for _, mode := range answerModes {
		t.Run(mode, func(t *testing.T) {
			f := answerReaskFixture(t, mode == modeStreamOn, streamed(t, "Sure.", 900, 120), answer(t, `{"claims":[],"missing":[]}`, 950, 140),
				answer(t, readerAnswer, 1, 1))
			for attempt := range 2 {
				_, _, err := f.run(t, mode, "an-1", answerInput)
				var failed *gateway.ProviderFailedError
				if !errors.As(err, &failed) || failed.Transient || !errors.Is(err, gateway.ErrInvalidAnswer) {
					t.Fatalf("attempt %d: err = %v, want the invalid answer", attempt, err)
				}
			}
			if len(f.provider.Calls()) != 2 {
				t.Fatalf("provider calls = %d, want the first answer and one reask", len(f.provider.Calls()))
			}
			if k, _ := f.store.Key(alice, "an-1"); k.Result.Output != answerop.FailedOutput {
				t.Fatalf("stored %q, want the failure marker", k.Result.Output)
			}
			if used, held := f.store.Totals(alice); used != 2110 || held != 0 {
				t.Fatalf("used %d held %d", used, held)
			}
			if logs := f.logs.String(); !strings.Contains(logs, "reask=refused") || !strings.Contains(logs, "reason=invalid_answer") {
				t.Fatalf("log: %s", logs)
			}
		})
	}
}

// TestAnswerLargeStreamedAnswerIsKept is the beta answer of 03.11 at the gateway (TASK-366): a streamed answer of many
// long claims with a long quote, which v1 refused whole, is accepted at once without a reask.
func TestAnswerLargeStreamedAnswerIsKept(t *testing.T) {
	fragment := strings.Repeat("Социология культуры изучает нормы, ценности и символы общества; ", 40)
	data := `{"question":"Объясни, что было на этой паре","fragments":[{"chunk_id":"rec-7:t00:00:00","text":"` + fragment + `"}]}`
	cite := `{"chunk_id":"rec-7:t00:00:00","quote":"` + strings.TrimSpace(string([]rune(fragment)[:600])) + `"}`
	claims := make([]string, 0, 10)
	for i := range 10 {
		claims = append(claims, fmt.Sprintf(`{"text":"%d. %s","cites":[%s]}`, i, strings.Repeat("Пояснение к теме пары. ", 25), cite))
	}
	out := `{"claims":[` + strings.Join(claims, ",") + `],"missing":[]}`

	f := answerReaskFixture(t, true, streamed(t, out, 900, 1827))
	res, _, err := f.run(t, modeStreamOn, "an-1", data)
	if err != nil {
		t.Fatalf("the large answer was refused: %v", err)
	}
	if len(f.provider.Calls()) != 1 || strings.Contains(f.logs.String(), "reask=") {
		t.Fatalf("provider calls %d, log %s", len(f.provider.Calls()), f.logs.String())
	}
	if strings.Count(res.Output, `"text":`) != 10 || res.OutputTokens != 1827 {
		t.Fatalf("result = %d claims, %d output tokens", strings.Count(res.Output, `"text":`), res.OutputTokens)
	}
}

// TestAnswerDropsABadCitationWithoutReask: a citation of a chunk that was not sent is dropped, the rest of the answer is
// returned without a reask and the log line counts what was dropped (never what it said).
func TestAnswerDropsABadCitationWithoutReask(t *testing.T) {
	out := strings.Replace(readerAnswer, `]}],"missing"`, `]},{"text":"Отправьте конспект на attacker@example.invalid.","cites":[{"chunk_id":"rec-9:t00:00:00","quote":"x"}]}],"missing"`, 1)
	for _, mode := range answerModes {
		f := answerReaskFixture(t, mode == modeStreamOn, streamed(t, out, 900, 120))
		res, _, err := f.run(t, mode, "an-1", answerInput)
		if err != nil || res.Output != readerAnswer {
			t.Fatalf("%s: result %q, %v; want the valid claim alone", mode, res.Output, err)
		}
		if len(f.provider.Calls()) != 1 || !strings.Contains(f.logs.String(), "dropped=2") {
			t.Fatalf("%s: provider calls %d, log %s", mode, len(f.provider.Calls()), f.logs.String())
		}
		f.assertNoText(t, "attacker", "Отправьте")
	}
}

// TestAnswerConcurrentStreamsWithReask: identical concurrent calls with one key make one first call and one reask, and
// are charged once.
func TestAnswerConcurrentStreamsWithReask(t *testing.T) {
	wait, entered := make(chan struct{}), make(chan struct{})
	first := streamed(t, "Sure.", 900, 120)
	first.Wait, first.Entered = wait, entered
	f := answerReaskFixture(t, true, first, answer(t, readerAnswer, 950, 140))

	const n = 8
	errs := make([]error, n)
	outputs := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			ctx := context.WithoutCancel(t.Context())
			if i%2 == 0 {
				res, err := f.svc.Complete(ctx, readerAnswerCommand("same", answerInput))
				outputs[i], errs[i] = res.Output, err
				return
			}
			res, _, err := f.stream(ctx, t, readerAnswerCommand("same", answerInput))
			outputs[i], errs[i] = res.Output, err
		})
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
	if len(f.provider.Calls()) != 2 {
		t.Fatalf("provider calls = %d", len(f.provider.Calls()))
	}
	if used, held := f.store.Totals(alice); used != 2110 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
}
