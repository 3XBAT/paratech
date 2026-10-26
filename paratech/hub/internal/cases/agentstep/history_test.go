package agentstep_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"paratech/hub/internal/cases/agentstep"
	"paratech/hub/internal/domain/llm/values"
)

// replyText stands for an agent reply the desktop builds; like the student's text it must never reach the log.
const replyText = "reply: secret-reply-marker"

func studentTurn(s string) agentstep.Turn {
	return agentstep.Turn{Kind: values.TurnStudentText, StudentText: s}
}

func replyTurn(s string) agentstep.Turn {
	return agentstep.Turn{Kind: values.TurnAgentReply, AgentReply: s}
}

// pairs is a history of a desktop with TASK-183: an earlier request with its reply, then the current request.
func pairs() []agentstep.Turn {
	return []agentstep.Turn{studentTurn("secret earlier request"), replyTurn(replyText), studentTurn(studentText)}
}

// TestHistoryPairsReachTheProvider: the agent reply reaches the provider in place; the same key replays it for free,
// and the same key without the reply or with another reply is a conflict.
func TestHistoryPairsReachTheProvider(t *testing.T) {
	f := newFixture(t, 1_000_000, twoCalls(t))
	first := f.step(t, command(alice, "ag:1", pairs()...))
	turns := f.provider.Calls()[0].Turns()
	if len(turns) != 3 || turns[1].Kind() != values.TurnAgentReply || turns[1].Text() != replyText || turns[2].Text() != studentText {
		t.Fatalf("history = %v", turns)
	}
	ledger := len(f.store.Ledger(alice))

	again := f.step(t, command(alice, "ag:1", pairs()...))
	if !again.Replayed || fmt.Sprint(again.Calls) != fmt.Sprint(first.Calls) || len(f.provider.Calls()) != 1 ||
		len(f.store.Ledger(alice)) != ledger {
		t.Fatalf("replay = %+v", again)
	}

	other := pairs()
	other[1].AgentReply += "!"
	for name, turns := range map[string][]agentstep.Turn{
		"without the reply": {pairs()[0], pairs()[2]},
		"another reply":     other,
	} {
		if _, err := f.svc.Step(t.Context(), command(alice, "ag:1", turns...)); !errors.Is(err, agentstep.ErrIdempotencyConflict) {
			t.Errorf("%s: err = %v, want ErrIdempotencyConflict", name, err)
		}
	}
	f.assertNoText(t)
	if strings.Contains(f.logs.String(), "secret-reply-marker") {
		t.Fatalf("the log contains the reply:\n%s", f.logs.String())
	}
}

// TestHistoryPairsWithTheEcho: the current request's tool loop continues after the pairs, and the kept turn still
// goes back on its echo.
func TestHistoryPairsWithTheEcho(t *testing.T) {
	f := newFixture(t, 1_000_000, findLessons(t), reply(t, "toolu_02"))
	first := f.step(t, command(alice, "ag:1", pairs()...))
	loop := echo(first)
	turns := append(pairs(), loop[1:]...)
	f.step(t, command(alice, "ag:2", turns...))
	sent := f.provider.Calls()[1].Turns()
	if len(sent) != 5 || sent[1].Kind() != values.TurnAgentReply || sent[3].Assistant().Raw() != keptRaw {
		t.Fatalf("history = %v", sent)
	}
}

// TestAgentReplyRules: misplaced, empty or oversized replies are invalid before anything is reserved or called.
func TestAgentReplyRules(t *testing.T) {
	f := newFixture(t, 1_000_000, twoCalls(t))
	first := f.step(t, command(alice, "ag:1"))
	loop := echo(first)
	text, rep := studentTurn("a"), replyTurn("reply: b")
	for name, turns := range map[string][]agentstep.Turn{
		"reply first":         {rep, text},
		"reply last":          {text, rep},
		"reply before echo":   {text, rep, loop[1], loop[2]},
		"reply after results": {loop[0], loop[1], loop[2], rep, text},
		"two replies":         {text, rep, rep, text},
		"empty reply":         {text, replyTurn(""), text},
		"reply with NUL":      {text, replyTurn("a\x00"), text},
		"reply over 2 KiB":    {text, replyTurn(strings.Repeat("a", values.MaxAgentReplyBytes+1)), text},
	} {
		_, err := f.svc.Step(t.Context(), command(alice, "ag:2", turns...))
		if !errors.Is(err, agentstep.ErrInvalidRequest) {
			t.Errorf("%s: err = %v, want ErrInvalidRequest", name, err)
		}
	}
	if len(f.provider.Calls()) != 1 || len(f.store.Ledger(alice)) != 2 {
		t.Fatalf("an invalid request reached the provider or the ledger (%d ledger rows)", len(f.store.Ledger(alice)))
	}
}

// TestConcurrentStepsWithPairs: concurrent identical requests with pairs make one provider call; the rest replay or
// see the running attempt.
func TestConcurrentStepsWithPairs(t *testing.T) {
	f := newFixture(t, 1_000_000, twoCalls(t))
	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Go(func() {
			_, errs[i] = f.svc.Step(t.Context(), command(alice, "ag:1", pairs()...))
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil && !errors.Is(err, agentstep.ErrAttemptRunning) {
			t.Errorf("step %d: %v", i, err)
		}
	}
	if len(f.provider.Calls()) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(f.provider.Calls()))
	}
}
