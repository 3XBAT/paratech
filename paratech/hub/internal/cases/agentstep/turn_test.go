package agentstep_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"paratech/hub/internal/cases/agentstep"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/testenv"
)

// keptRaw stands for the provider's raw assistant content of the first answer; the marker must never reach the
// result, the log or another tester.
const keptRaw = `[{"type":"thinking","thinking":"kept-thinking-marker","signature":"kept-signature-marker"},` +
	`{"type":"tool_use","id":"toolu_01","name":"find_lessons","input":{"subject":"subj-1"}}]`

// withTurn adds the raw assistant content to a scripted answer, as the anthropic adapter does.
func withTurn(t *testing.T, step testenv.AgentProviderStep, raw string) testenv.AgentProviderStep {
	t.Helper()
	turn, err := values.NewAssistantTurn(raw)
	if err != nil {
		t.Fatal(err)
	}
	step.Answer = step.Answer.WithAssistant(turn)
	return step
}

func findLessons(t *testing.T) testenv.AgentProviderStep {
	t.Helper()
	return withTurn(t, answer(t, 1200, 80, toolCall(t, "toolu_01", "find_lessons", `{"subject":"subj-1"}`)), keptRaw)
}

func reply(t *testing.T, id string) testenv.AgentProviderStep {
	t.Helper()
	return answer(t, 10, 5, toolCall(t, id, "reply", `{"text":"Готово."}`))
}

func assertNoTurn(t *testing.T, f fixture, res agentstep.Result) {
	t.Helper()
	for _, s := range []string{fmt.Sprintf("%+v", res), f.logs.String()} {
		if strings.Contains(s, "kept-thinking-marker") || strings.Contains(s, "kept-signature-marker") {
			t.Fatalf("the kept turn leaked: %s", s)
		}
	}
}

// TestKeptTurnGoesBackWithTheEcho: the model's raw turn is kept for the returned calls and attached, unchanged, to
// the echo of those calls in the next step; the desktop never gets it.
func TestKeptTurnGoesBackWithTheEcho(t *testing.T) {
	f := newFixture(t, 1_000_000, findLessons(t), reply(t, "toolu_02"))
	first := f.step(t, command(alice, "ag:1"))
	if f.turns.Len(alice) != 1 {
		t.Fatalf("kept turns = %d, want 1", f.turns.Len(alice))
	}
	assertNoTurn(t, f, first)

	second := f.step(t, command(alice, "ag:2", echo(first)...))
	turns := f.provider.Calls()[1].Turns()
	if got := turns[1].Assistant().Raw(); got != keptRaw {
		t.Fatalf("assistant turn sent = %q, want the kept one unchanged", got)
	}
	if !f.provider.Calls()[0].Turns()[0].Assistant().IsZero() || !turns[0].Assistant().IsZero() || !turns[2].Assistant().IsZero() {
		t.Fatal("a turn other than the last echo carries an assistant turn")
	}
	assertNoTurn(t, f, second)
	f.assertNoText(t)
}

// TestTurnGoneFailsTheStep: when the kept turn is gone (purged, expired, never kept) the step fails with ErrTurnGone
// before the provider is called and nothing is charged; a retry fails the same way, and a replay of an earlier key
// stays a free replay.
func TestTurnGoneFailsTheStep(t *testing.T) {
	cases := map[string]func(f fixture){
		"dropped": func(f fixture) { f.turns.Drop(alice) },
		"expired": func(f fixture) { f.clock.Advance(retention - account.ResultPurgeInterval) },
	}
	for name, lose := range cases {
		f := newFixture(t, 1_000_000, findLessons(t), reply(t, "toolu_02"))
		first := f.step(t, command(alice, "ag:1"))
		usedBefore, _ := f.store.Totals(alice)
		lose(f)

		for attempt := range 2 {
			_, err := f.svc.Step(t.Context(), command(alice, "ag:2", echo(first)...))
			if !errors.Is(err, agentstep.ErrTurnGone) || errors.Is(err, agentstep.ErrInvalidRequest) {
				t.Fatalf("%s, attempt %d: err = %v, want ErrTurnGone", name, attempt, err)
			}
		}
		if len(f.provider.Calls()) != 1 {
			t.Fatalf("%s: provider calls = %d, want none for the continuation", name, len(f.provider.Calls()))
		}
		if used, held := f.store.Totals(alice); used != usedBefore || held != 0 {
			t.Fatalf("%s: used %d held %d, want nothing charged", name, used, held)
		}
		if !strings.Contains(f.logs.String(), "outcome=turn_gone") {
			t.Fatalf("%s: log: %s", name, f.logs.String())
		}
		if name == "dropped" {
			if again := f.step(t, command(alice, "ag:1")); !again.Replayed || len(f.provider.Calls()) != 1 {
				t.Fatalf("%s: replay = %+v", name, again)
			}
		}
	}
}

// TestReplayOfAContinuationNeedsNoKeptTurn: a continuation that succeeded replays for free even after its kept turn
// is gone.
func TestReplayOfAContinuationNeedsNoKeptTurn(t *testing.T) {
	f := newFixture(t, 1_000_000, findLessons(t), reply(t, "toolu_02"))
	first := f.step(t, command(alice, "ag:1"))
	second := f.step(t, command(alice, "ag:2", echo(first)...))
	used, _ := f.store.Totals(alice)
	f.turns.Drop(alice)

	again := f.step(t, command(alice, "ag:2", echo(first)...))
	if !again.Replayed || fmt.Sprint(again.Calls) != fmt.Sprint(second.Calls) || len(f.provider.Calls()) != 2 {
		t.Fatalf("replay = %+v", again)
	}
	if after, _ := f.store.Totals(alice); after != used {
		t.Fatalf("used %d -> %d", used, after)
	}
}

// TestKeptTurnIsScopedByTester: a tester holding a valid seal over the same calls cannot reach another tester's turn.
func TestKeptTurnIsScopedByTester(t *testing.T) {
	f := newFixture(t, 1_000_000, findLessons(t), reply(t, "toolu_02"))
	first := f.step(t, command(alice, "ag:1"))

	turns := echo(first)
	turns[1].Seal = f.sealer.Seal(binding(bob), modelCalls(first, t))
	_, err := f.svc.Step(t.Context(), command(bob, "ag:1", turns...))
	if !errors.Is(err, agentstep.ErrTurnGone) {
		t.Fatalf("bob's continuation: err = %v, want ErrTurnGone", err)
	}
	if f.turns.Len(bob) != 0 || len(f.provider.Calls()) != 1 {
		t.Fatal("bob reached alice's kept turn")
	}
}

// TestUnusableAnswerKeepsNoTurn: only an answer whose calls are returned keeps a turn.
func TestUnusableAnswerKeepsNoTurn(t *testing.T) {
	bad := withTurn(t, answer(t, 100, 20, toolCall(t, "toolu_01", "send_email", `{}`)), keptRaw)
	f := newFixture(t, 1_000_000, bad)
	if _, err := f.svc.Step(t.Context(), command(alice, "ag:1")); err == nil {
		t.Fatal("an unusable answer succeeded")
	}
	if f.turns.Len(alice) != 0 {
		t.Fatal("an unusable answer kept a turn")
	}
}

// TestTurnStoreFailures: a turn that cannot be kept does not cost the answer (it is charged and returned); a turn
// that cannot be read is a Hub failure, released, not ErrTurnGone.
func TestTurnStoreFailures(t *testing.T) {
	down := errors.New("turn store down")
	f := newFixture(t, 1_000_000, findLessons(t), reply(t, "toolu_02"))
	f.turns.Fail(down)
	first := f.step(t, command(alice, "ag:1"))
	if used, _ := f.store.Totals(alice); used != 1280 || len(first.Calls) != 1 {
		t.Fatalf("used %d, result %+v", used, first)
	}
	if !strings.Contains(f.logs.String(), "agent step turn not kept") {
		t.Fatalf("log: %s", f.logs.String())
	}

	_, err := f.svc.Step(t.Context(), command(alice, "ag:2", echo(first)...))
	if !errors.Is(err, down) || errors.Is(err, agentstep.ErrTurnGone) {
		t.Fatalf("read failure: err = %v", err)
	}
	f.turns.Fail(nil)
	if _, err := f.svc.Step(t.Context(), command(alice, "ag:3", echo(first)...)); !errors.Is(err, agentstep.ErrTurnGone) {
		t.Fatalf("never kept: err = %v", err)
	}
	if used, held := f.store.Totals(alice); used != 1280 || held != 0 || len(f.provider.Calls()) != 1 {
		t.Fatalf("used %d held %d calls %d", used, held, len(f.provider.Calls()))
	}
}

// TestPurgeResultsDeletesOldTurns: kept turns go when the result window ends (ADR-0005(d)).
func TestPurgeResultsDeletesOldTurns(t *testing.T) {
	f := newFixture(t, 1_000_000, findLessons(t))
	f.step(t, command(alice, "ag:1"))
	if n, err := f.svc.PurgeResults(t.Context()); err != nil || n != 0 {
		t.Fatalf("fresh purge = %d, %v", n, err)
	}
	f.clock.Advance(retention - account.ResultPurgeInterval + time.Second)
	if n, err := f.svc.PurgeResults(t.Context()); err != nil || n != 1 || f.turns.Len(alice) != 0 {
		t.Fatalf("purge = %d, %v", n, err)
	}
}
