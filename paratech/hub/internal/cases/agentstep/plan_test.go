package agentstep_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"paratech/hub/internal/cases/agentstep"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/testenv"
)

// newCatalogsFixture is newFixture serving the given panel catalogs side by side.
func newCatalogsFixture(t *testing.T, catalogs []uint32, steps ...testenv.AgentProviderStep) fixture {
	t.Helper()
	var versions []agentstep.Version
	for _, v := range catalogs {
		system, catalog, err := testenv.Panel(v)
		if err != nil {
			t.Fatal(err)
		}
		versions = append(versions, agentstep.Version{System: system, Catalog: catalog})
	}
	sealer, err := testenv.Sealer(1)
	if err != nil {
		t.Fatal(err)
	}
	last := versions[len(versions)-1]
	f := fixture{
		store: testenv.NewQuotaStore(), turns: testenv.NewAgentTurns(), provider: testenv.NewFakeAgentProvider(steps...),
		limiter: &testenv.FakeLimiter{}, clock: testenv.NewFakeClock(t0), sealer: sealer, logs: &syncBuffer{},
		system: last.System, catalog: last.Catalog,
	}
	f.svc, err = agentstep.New(agentstep.Deps{
		Accounts: f.store, Turns: f.turns, Provider: f.provider, Limiter: f.limiter, Clock: f.clock, Random: testenv.Random{}, Models: models(t),
		Sealer: sealer, Log: slog.New(slog.NewTextHandler(f.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}, agentstep.Config{Unit: "tokens", Limit: 10_000_000, ReservationTTL: ttl, KeyRetention: retention, CallTimeout: callTimeout,
		Profiles: []agentstep.Profile{{Name: values.ProfilePanel, Alias: "planner", Versions: versions}}})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// bigPlan is show_plan arguments of n steps that each mark an assignment done: about 90 bytes a step.
func bigPlan(t *testing.T, n int) string {
	t.Helper()
	steps := make([]map[string]any, n)
	for i := range steps {
		steps[i] = map[string]any{"tool": "mark_assignments_done", "arguments": map[string]any{"assignments": []string{"asg-0123456789"}, "done": true}}
	}
	b, err := json.Marshal(map[string]any{"summary": "Отметить выполненными", "steps": steps})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestLargePlanOfCatalog10 (TASK-325, ADR-0033 §4): a returned show_plan of 200 steps, over 8 KiB, passes on catalog 10,
// is sealed, stored and replayed whole, and its echo starts the next step. The same answer on catalog 9, served next
// to it, is the charged invalid-calls failure.
func TestLargePlanOfCatalog10(t *testing.T) {
	plan := bigPlan(t, 200)
	if len(plan) <= values.MaxArgumentsBytes || len(plan) > values.MaxPlanArgumentsBytes {
		t.Fatalf("plan of %d bytes", len(plan))
	}
	planned := func() testenv.AgentProviderStep {
		return answer(t, 1200, 80, toolCall(t, "toolu_01", "show_plan", plan))
	}

	f := newCatalogsFixture(t, []uint32{9, 10}, planned(), twoCalls(t))
	res := f.step(t, commandV("ag:1", 10))
	if len(res.Calls) != 1 || res.Calls[0].Arguments != plan {
		t.Fatalf("calls = %d", len(res.Calls))
	}
	if err := f.sealer.Verify(bindingV(10), modelCalls(res, t), res.Seal); err != nil {
		t.Fatalf("the seal does not verify: %v", err)
	}
	if again := f.step(t, commandV("ag:1", 10)); !again.Replayed || again.Calls[0].Arguments != plan || len(f.provider.Calls()) != 1 {
		t.Fatalf("replay = %v, provider calls %d", again.Replayed, len(f.provider.Calls()))
	}
	if _, err := f.svc.Step(t.Context(), commandV("ag:2", 10, echo(res)...)); err != nil {
		t.Fatalf("the echo of the large plan: %v", err)
	}
	if strings.Contains(f.logs.String(), "Отметить") || strings.Contains(f.logs.String(), "asg-0123456789") {
		t.Fatal("the log holds the plan")
	}

	old := newCatalogsFixture(t, []uint32{9, 10}, planned())
	_, err := old.svc.Step(t.Context(), commandV("ag:1", 9))
	var failed *agentstep.ProviderFailedError
	if !errors.As(err, &failed) || !strings.Contains(err.Error(), "invalid tool calls") {
		t.Fatalf("v9: err = %v", err)
	}
	if used, _ := old.store.Totals(alice); used != 1280 {
		t.Fatalf("v9: used %d, want the billed answer charged", used)
	}
}

// TestLargePlanEchoOfAnOlderCatalog: a show_plan over 8 KiB echoed on catalog 9 was never returned there, so its seal
// cannot verify: the request is refused before the ledger or the provider is touched.
func TestLargePlanEchoOfAnOlderCatalog(t *testing.T) {
	f := newCatalogsFixture(t, []uint32{9, 10}, answer(t, 1200, 80, toolCall(t, "toolu_01", "show_plan", bigPlan(t, 200))))
	res := f.step(t, commandV("ag:1", 10))

	// The client replays the calls sealed for catalog 10 as if they were of catalog 9.
	_, err := f.svc.Step(t.Context(), commandV("ag:2", 9, echo(res)...))
	if !errors.Is(err, agentstep.ErrBadSeal) {
		t.Fatalf("err = %v, want a bad seal", err)
	}
	if len(f.provider.Calls()) != 1 {
		t.Fatalf("provider calls = %d", len(f.provider.Calls()))
	}
}

// TestConcurrentLargePlanSteps: N callers with one key on catalog 10 make one provider call and one charge; every
// completed or replayed caller gets the same large plan.
func TestConcurrentLargePlanSteps(t *testing.T) {
	plan := bigPlan(t, 200)
	wait, entered := make(chan struct{}), make(chan struct{})
	step := answer(t, 1200, 80, toolCall(t, "toolu_01", "show_plan", plan))
	step.Wait, step.Entered = wait, entered
	f := newCatalogsFixture(t, []uint32{10}, step)
	const n = 8
	errs := make([]error, n)
	results := make([]agentstep.Result, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = f.svc.Step(context.WithoutCancel(t.Context()), commandV("ag:same", 10))
		}()
	}
	<-entered
	close(wait)
	wg.Wait()
	completed := 0
	for i := range n {
		switch {
		case errs[i] == nil && !results[i].Replayed:
			completed++
			fallthrough
		case errs[i] == nil:
			if len(results[i].Calls) != 1 || results[i].Calls[0].Arguments != plan {
				t.Fatalf("caller %d got another plan", i)
			}
		case errors.Is(errs[i], agentstep.ErrAttemptRunning):
		default:
			t.Fatalf("caller %d: %v", i, errs[i])
		}
	}
	if completed != 1 || len(f.provider.Calls()) != 1 {
		t.Fatalf("completed %d, provider calls %d", completed, len(f.provider.Calls()))
	}
	if used, held := f.store.Totals(alice); used != 1280 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
}
