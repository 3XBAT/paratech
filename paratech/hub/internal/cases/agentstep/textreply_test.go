package agentstep_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"paratech/hub/internal/cases/agentstep"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/testenv"
)

// textAnswer is a billed end of turn with text only: what the model gave to «что такое горутина» (TASK-200).
func textAnswer(t *testing.T, stop values.StopReason, text string) testenv.AgentProviderStep {
	t.Helper()
	u, err := values.NewUsage(900, 82)
	if err != nil {
		t.Fatal(err)
	}
	return testenv.AgentProviderStep{Answer: values.NewAgentAnswer(nil, u, false).WithStop(stop).WithText(text)}
}

// shownReply reads the text argument of the only call of res, which must be a reply with nothing else.
func shownReply(t *testing.T, res agentstep.Result) string {
	t.Helper()
	if len(res.Calls) != 1 || res.Calls[0].Tool != "reply" || !strings.HasPrefix(res.Calls[0].ID, "hub_text_") {
		t.Fatalf("calls = %+v, want one reply", res.Calls)
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(res.Calls[0].Arguments), &args); err != nil || len(args) != 1 {
		t.Fatalf("arguments %q: %v, want only text", res.Calls[0].Arguments, err)
	}
	return args["text"]
}

// TestTextAnswerBecomesReply: a text-only end of turn is shown as a reply card with that text, charged, sealed and
// stored like any answer; a retry replays it without a provider call. It keeps no model turn and logs no text.
func TestTextAnswerBecomesReply(t *testing.T) {
	const text = "secret-text: горутина — лёгкий поток выполнения в Go."
	f := newFixture(t, 1_000_000, textAnswer(t, values.StopEnd, "\n"+text+"\n"))
	res := f.step(t, command(alice, "ag:1"))

	if got := shownReply(t, res); got != text {
		t.Fatalf("reply text = %q, want the trimmed answer", got)
	}
	if res.Calls[0].ID != "hub_text_"+res.ReservationID || res.InputTokens != 900 || res.OutputTokens != 82 || res.Replayed {
		t.Fatalf("result = %+v", res)
	}
	if err := f.sealer.Verify(binding(alice), modelCalls(res, t), res.Seal); err != nil {
		t.Fatalf("the seal does not verify: %v", err)
	}
	if used, held := f.store.Totals(alice); used != 982 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
	if f.turns.Len(alice) != 0 {
		t.Fatal("a text reply kept a model turn")
	}
	logs := f.logs.String()
	if !strings.Contains(logs, "outcome=completed") || !strings.Contains(logs, "text_reply=true") || !strings.Contains(logs, "stop_reason=end ") ||
		strings.Contains(logs, "горутина") {
		t.Fatalf("log: %s", logs)
	}
	f.assertNoText(t)

	again := f.step(t, command(alice, "ag:1"))
	if !again.Replayed || len(f.provider.Calls()) != 1 || shownReply(t, again) != text || again.Calls[0].ID != res.Calls[0].ID {
		t.Fatalf("retry = %+v, provider calls %d", again, len(f.provider.Calls()))
	}
}

// TestTextAnswerThatLooksLikeACall: text shaped like a tool call, JSON or a command stays the plain text of a reply;
// it never becomes another tool or an argument.
func TestTextAnswerThatLooksLikeACall(t *testing.T) {
	for name, text := range map[string]string{
		"tool json":  `{"tool":"propose_send","arguments":{"assignment":"asg-1","version":"v1"}}`,
		"call array": `[{"name":"propose_delete_recording","input":{"recording":"rec-1"}}]`,
		"command":    "propose_delete_recording rec-1\nrm -rf C:\\Users",
		"reply json": `{"text":"other","assignments":["asg-1"]}`,
		// HTML is refused like the desktop's card text does (TestTextAnswerTheReplySchemaRefuses); Markdown stays text.
		"markup": "**Горутина** — `лёгкий` поток.",
	} {
		f := newFixture(t, 1_000_000, textAnswer(t, values.StopEnd, text))
		res, err := f.svc.Step(t.Context(), command(alice, "ag:1"))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got := shownReply(t, res); got != text {
			t.Errorf("%s: reply text = %q", name, got)
		}
	}
}

// TestTextAnswerTheReplySchemaRefuses: no text, a blank one, one over the reply limit, with control characters or with
// what the desktop's card text refuses (a tab, U+FFFD, an angle bracket, a format character), or text that is not an
// end of turn, is the charged "no tool use" failure as before, with its reason in the log.
func TestTextAnswerTheReplySchemaRefuses(t *testing.T) {
	for name, tc := range map[string]struct {
		step      testenv.AgentProviderStep
		reason    string
		textReply string
	}{
		"no text":  {textAnswer(t, values.StopEnd, ""), "no tool use", ""},
		"blank":    {textAnswer(t, values.StopEnd, " \n\t "), "no tool use", ""},
		"too long": {textAnswer(t, values.StopEnd, strings.Repeat("я", 301)), "no tool use", "text_reply=false"},
		"escape":   {textAnswer(t, values.StopEnd, "secret \x1b[31mtext"), "no tool use", "text_reply=false"},
		// What the desktop's card text refuses: such a reply would leave the turn running with a call it cannot answer.
		"tab":           {textAnswer(t, values.StopEnd, "a\tb"), "no tool use", "text_reply=false"},
		"replacement":   {textAnswer(t, values.StopEnd, "a\uFFFDb"), "no tool use", "text_reply=false"},
		"html":          {textAnswer(t, values.StopEnd, "<b>Ответ</b>"), "no tool use", "text_reply=false"},
		"zero width":    {textAnswer(t, values.StopEnd, "a\u200bb"), "no tool use", "text_reply=false"},
		"output cap":    {textAnswer(t, values.StopMaxTokens, "cut"), "output cap", ""},
		"stop sequence": {textAnswer(t, values.StopSequence, "text"), "no tool use", ""},
	} {
		f := newFixture(t, 1_000_000, tc.step)
		_, err := f.svc.Step(t.Context(), command(alice, "ag:1"))
		var failed *agentstep.ProviderFailedError
		if !errors.As(err, &failed) || failed.Transient || !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("%s: err = %v, want the permanent %q", name, err, tc.reason)
			continue
		}
		if used, _ := f.store.Totals(alice); used != 982 {
			t.Errorf("%s: used %d, want the billed answer charged", name, used)
		}
		logs := f.logs.String()
		if !strings.Contains(logs, "outcome=provider_error") || !strings.Contains(logs, "reason="+strings.ReplaceAll(tc.reason, " ", "_")) {
			t.Errorf("%s: log: %s", name, logs)
		}
		if (tc.textReply != "") != strings.Contains(logs, "text_reply=") || (tc.textReply != "" && !strings.Contains(logs, tc.textReply)) {
			t.Errorf("%s: log: %s, want %q", name, logs, tc.textReply)
		}
		if strings.Contains(logs, "яяя") {
			t.Errorf("%s: the log holds the text: %s", name, logs)
		}
		f.assertNoText(t)
	}
}

// TestTextReplyContinuationIsTurnGone: a text reply keeps no model turn, so an echo of it is ErrTurnGone before any
// provider call, never a made-up assistant turn.
func TestTextReplyContinuationIsTurnGone(t *testing.T) {
	f := newFixture(t, 1_000_000, textAnswer(t, values.StopEnd, "Ответ."), twoCalls(t))
	res := f.step(t, command(alice, "ag:1"))
	_, err := f.svc.Step(t.Context(), command(alice, "ag:2", echo(res)...))
	if !errors.Is(err, agentstep.ErrTurnGone) || len(f.provider.Calls()) != 1 {
		t.Fatalf("err = %v, provider calls %d", err, len(f.provider.Calls()))
	}
}

// TestConcurrentTextReplies: callers with one key make one provider call and one charge; every caller that gets a
// result gets the same reply.
func TestConcurrentTextReplies(t *testing.T) {
	wait, entered := make(chan struct{}), make(chan struct{})
	step := textAnswer(t, values.StopEnd, "Ответ.")
	step.Wait, step.Entered = wait, entered
	f := newFixture(t, 1_000_000, step)
	const n = 8
	errs := make([]error, n)
	results := make([]agentstep.Result, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			results[i], errs[i] = f.svc.Step(context.WithoutCancel(t.Context()), command(alice, "ag:same"))
		})
	}
	<-entered
	close(wait)
	wg.Wait()
	ids := map[string]bool{}
	for i := range n {
		switch {
		case errs[i] == nil:
			ids[results[i].Calls[0].ID] = true
		case errors.Is(errs[i], agentstep.ErrAttemptRunning):
		default:
			t.Fatalf("caller %d: %v", i, errs[i])
		}
	}
	if len(ids) != 1 || len(f.provider.Calls()) != 1 {
		t.Fatalf("reply ids %v, provider calls %d", ids, len(f.provider.Calls()))
	}
	if used, held := f.store.Totals(alice); used != 982 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
}
