package gateway_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/hub/internal/cases/gateway"
	"paratech/hub/internal/domain/llm/services/triage"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/testenv"
)

var (
	// partialNoHelper is the beta answer of 26.10 (TASK-200): a partial work without its helper, otherwise in the schema.
	partialNoHelper = strings.Replace(triageAnswer, `"feasibility":"full","feasibility_reason":""`,
		`"feasibility":"partial","feasibility_reason":"Нужны измерения."`, 1)
	partialHelper = strings.Replace(partialNoHelper, `"helper":null`,
		`"helper":{"steps":["Провести измерения."],"submit":"Отчёт в docx.","pitfalls":[],"ready_parts":[],"placeholders":[]}`, 1)
)

// TestTriageReaskFixesAMissingHelper: a partial work without a helper is asked once more within the same reservation,
// with the server note added to the server prompt and the same data; the fixed answer is returned and stored, both
// calls are charged at their usage, once, and a retry replays it free.
func TestTriageReaskFixesAMissingHelper(t *testing.T) {
	f := triageFixture(t, answer(t, partialNoHelper, 900, 120), answer(t, partialHelper, 950, 140))
	res := f.complete(t, triageCommand("tr-1", triageRound1))
	if res.Output != partialHelper || res.InputTokens != 1850 || res.OutputTokens != 260 || res.Replayed {
		t.Fatalf("result = %+v", res)
	}
	calls := f.provider.Calls()
	if len(calls) != 2 {
		t.Fatalf("provider calls = %d, want one reask", len(calls))
	}
	if calls[1].Instructions() != triagePrompt+"\n\n"+triageReask || calls[1].Data() != calls[0].Data() ||
		calls[1].Model() != calls[0].Model() || calls[1].MaxOutput() != calls[0].MaxOutput() {
		t.Fatalf("reask = %v, first %v", calls[1], calls[0])
	}
	if strings.Contains(calls[1].Instructions(), "Нужны измерения") || strings.Contains(calls[1].Instructions(), "Реферат") {
		t.Fatal("the reask carries model or student text in the instructions")
	}
	if used, held := f.store.Totals(alice); used != 2110 || held != 0 {
		t.Fatalf("used %d held %d, want both calls charged once", used, held)
	}
	if rs := f.store.Reservations(alice); len(rs) != 1 {
		t.Fatalf("reservations = %d, want one", len(rs))
	}
	again := f.complete(t, triageCommand("tr-1", triageRound1))
	if again.Output != partialHelper || !again.Replayed || len(f.provider.Calls()) != 2 {
		t.Fatalf("replay = %+v, provider calls %d", again, len(f.provider.Calls()))
	}
	logs := f.logs.String()
	if !strings.Contains(logs, "reask=accepted") || !strings.Contains(logs, "outcome=completed") {
		t.Fatalf("log: %s", logs)
	}
	f.assertNoText(t, "Реферат", "измерения", triageReask[:20])
}

// TestTriageReaskLogsTheSecondStop: after a reask the log line has the stop reason of the answer it settles.
func TestTriageReaskLogsTheSecondStop(t *testing.T) {
	first := withStop(t, answer(t, partialNoHelper, 900, 120), values.StopSequence)
	for name, tc := range map[string]struct {
		second testenv.ProviderStep
		want   string
	}{
		"accepted": {answer(t, partialHelper, 950, 140), "stop=end"},
		"refused":  {withStop(t, answer(t, partialNoHelper, 950, 140), values.StopMaxTokens), "stop=max_tokens"},
	} {
		f := triageFixture(t, first, tc.second)
		_, _ = f.svc.Complete(t.Context(), triageCommand("tr-1", triageRound1))
		if logs := f.logs.String(); !strings.Contains(logs, tc.want) || strings.Contains(logs, "stop=stop_sequence") {
			t.Errorf("%s: log: %s", name, logs)
		}
	}
}

// TestTriageWithoutReaskNote: a server prompt without a reask note never asks again.
func TestTriageWithoutReaskNote(t *testing.T) {
	f := triageFixture(t, answer(t, partialNoHelper, 900, 120), answer(t, partialHelper, 950, 140))
	cfg := config(100_000)
	cfg.ServerPrompts = map[values.Operation]gateway.ServerPrompt{
		values.OperationTriageAssignment: {Alias: "strong", Version: "v1", Text: triagePrompt},
	}
	svc, err := gateway.New(gateway.Deps{
		Accounts: f.store, Results: f.store, Provider: f.provider, Limiter: f.limiter, Clock: f.clock, Random: testenv.Random{},
		Models: triageModels(t), Log: slog.New(slog.NewTextHandler(&syncWriter{w: f.logs}, nil)),
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Complete(t.Context(), triageCommand("tr-1", triageRound1)); !errors.Is(err, gateway.ErrInvalidAnswer) {
		t.Fatalf("err = %v", err)
	}
	if len(f.provider.Calls()) != 1 || strings.Contains(f.logs.String(), "reask=") {
		t.Fatalf("provider calls %d, log %s", len(f.provider.Calls()), f.logs.String())
	}
	if used, _ := f.store.Totals(alice); used != 1020 {
		t.Fatalf("used %d", used)
	}
}

func withStop(t *testing.T, step testenv.ProviderStep, stop values.StopReason) testenv.ProviderStep {
	t.Helper()
	c, err := values.NewCompletion(step.Completion.Output(), step.Completion.Usage(), stop)
	if err != nil {
		t.Fatal(err)
	}
	step.Completion = c
	return step
}

// TestTriageReaskOnlyOnce: a second answer without the helper is the refusal, charged with both usages and replayed
// without a third call; its log line says why.
func TestTriageReaskOnlyOnce(t *testing.T) {
	f := triageFixture(t, answer(t, partialNoHelper, 900, 120), answer(t, partialNoHelper, 950, 140), answer(t, partialHelper, 1, 1))
	for attempt := range 2 {
		_, err := f.svc.Complete(t.Context(), triageCommand("tr-1", triageRound1))
		var failed *gateway.ProviderFailedError
		if !errors.As(err, &failed) || failed.Transient || !errors.Is(err, gateway.ErrInvalidAnswer) {
			t.Fatalf("attempt %d: err = %v, want the invalid answer", attempt, err)
		}
	}
	if len(f.provider.Calls()) != 2 {
		t.Fatalf("provider calls = %d, want the first answer and one reask", len(f.provider.Calls()))
	}
	if k, _ := f.store.Key(alice, "tr-1"); k.Result.Output != triage.FailedOutput {
		t.Fatalf("stored %q, want the failure marker", k.Result.Output)
	}
	if used, held := f.store.Totals(alice); used != 2110 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
	logs := f.logs.String()
	if !strings.Contains(logs, "reask=refused") || !strings.Contains(logs, "reason=invalid_answer") || !strings.Contains(logs, "needs a helper") {
		t.Fatalf("log: %s", logs)
	}
	f.assertNoText(t, "Реферат", "измерения")
}

// TestTriageReaskFailure: a reask without a billed answer (a provider error, a refusal) leaves the first refusal with
// the first usage; other refusals of the schema are never asked again.
func TestTriageReaskFailure(t *testing.T) {
	u, _ := values.NewUsage(5, 1)
	refusal, err := values.NewCompletion("", u, values.StopRefusal)
	if err != nil {
		t.Fatal(err)
	}
	for name, second := range map[string]testenv.ProviderStep{
		"provider error": {Err: &values.ProviderError{Kind: values.KindTransient, Reason: "overloaded", Status: 529}},
		"refusal":        {Completion: refusal},
	} {
		f := triageFixture(t, answer(t, partialNoHelper, 900, 120), second)
		_, err := f.svc.Complete(t.Context(), triageCommand("tr-1", triageRound1))
		if !errors.Is(err, gateway.ErrInvalidAnswer) || len(f.provider.Calls()) != 2 {
			t.Errorf("%s: err = %v, provider calls %d", name, err, len(f.provider.Calls()))
		}
		if used, held := f.store.Totals(alice); used != 1020 || held != 0 {
			t.Errorf("%s: used %d held %d, want the first answer charged", name, used, held)
		}
		if !strings.Contains(f.logs.String(), "reask=failed") {
			t.Errorf("%s: log: %s", name, f.logs.String())
		}
	}

	helperWhenFull := strings.Replace(triageAnswer, `"helper":null`, `"helper":{"steps":["x"],"submit":"x","pitfalls":[],"ready_parts":[],"placeholders":[]}`, 1)
	noReason := strings.Replace(partialHelper, `"Нужны измерения."`, `""`, 1)
	for name, out := range map[string]string{"helper when full": helperWhenFull, "no reason": noReason} {
		f := triageFixture(t, answer(t, out, 900, 120), answer(t, triageAnswer, 1, 1))
		if _, err := f.svc.Complete(t.Context(), triageCommand("tr-1", triageRound1)); !errors.Is(err, gateway.ErrInvalidAnswer) ||
			len(f.provider.Calls()) != 1 || strings.Contains(f.logs.String(), "reask=") {
			t.Errorf("%s: err = %v, provider calls %d, log %s", name, err, len(f.provider.Calls()), f.logs.String())
		}
	}
}

// TestTriageReaskNeedsTimeBeforeTheHoldExpires: when the first call used the time of the hold, there is no reask and
// the refusal is settled before the hold expires.
func TestTriageReaskNeedsTimeBeforeTheHoldExpires(t *testing.T) {
	wait, entered := make(chan struct{}), make(chan struct{})
	first := answer(t, partialNoHelper, 900, 120)
	first.Wait, first.Entered = wait, entered
	f := triageFixture(t, first, answer(t, partialHelper, 1, 1))
	done := make(chan error, 1)
	go func() {
		_, err := f.svc.Complete(t.Context(), triageCommand("tr-1", triageRound1))
		done <- err
	}()
	<-entered
	f.clock.Advance(ttl - 5*time.Second) // what is left is the settlement's own bound
	close(wait)
	if err := <-done; !errors.Is(err, gateway.ErrInvalidAnswer) {
		t.Fatalf("err = %v", err)
	}
	if len(f.provider.Calls()) != 1 || !strings.Contains(f.logs.String(), "reask=no_time") {
		t.Fatalf("provider calls %d, log %s", len(f.provider.Calls()), f.logs.String())
	}
	if used, held := f.store.Totals(alice); used != 1020 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
}

// TestTriageConcurrentCallsWithReask: identical concurrent calls with one key make one first call and one reask, and
// are charged once.
func TestTriageConcurrentCallsWithReask(t *testing.T) {
	wait, entered := make(chan struct{}), make(chan struct{})
	first := answer(t, partialNoHelper, 900, 120)
	first.Wait, first.Entered = wait, entered
	f := triageFixture(t, first, answer(t, partialHelper, 950, 140))

	const n = 8
	errs := make([]error, n)
	results := make([]gateway.Result, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			results[i], errs[i] = f.svc.Complete(context.WithoutCancel(t.Context()), triageCommand("same", triageRound1))
		})
	}
	<-entered
	close(wait)
	wg.Wait()
	for i := range n {
		if errs[i] != nil && !errors.Is(errs[i], gateway.ErrAttemptRunning) {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		if errs[i] == nil && results[i].Output != partialHelper {
			t.Fatalf("caller %d: output %q", i, results[i].Output)
		}
	}
	if len(f.provider.Calls()) != 2 {
		t.Fatalf("provider calls = %d", len(f.provider.Calls()))
	}
	if used, held := f.store.Totals(alice); used != 2110 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
}
