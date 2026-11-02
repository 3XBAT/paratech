package agentturn

import (
	"errors"
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
)

const replyA1 = `{"text":"вот задание","assignments":["a1"]}`

// mentionedChat runs request 1 of a chat with a mention of assignment a1 and a reply, on a port that knows a1 and s7.
func mentionedChat(t *testing.T) (*env, *fakeMentioned, Step) {
	t.Helper()
	e := newEnv(t)
	port := &fakeMentioned{exists: values.NewRefSet(mustRef(t, values.KindAssignment, "a1"), mustRef(t, values.KindSubject, "s7"))}
	withMentions(t, e, port)
	e.model.then(raw("r1", "reply", `{"text":"ок"}`))
	first, err := e.svc.Ask(t.Context(), AskInput{
		Text: "что с этим заданием", Mentions: []values.Mention{mention(t, values.KindAssignment, "a1", "Лаба 1")},
	})
	if err != nil || first.Status != thread.StatusDone {
		t.Fatalf("request 1 = %+v, %v", first, err)
	}
	if got := e.model.last(t).Mentions; len(got) != 1 || got[0] != (values.StepMention{Ref: mustRef(t, values.KindAssignment, "a1"), Current: true}) {
		t.Fatalf("request 1 sends %+v", got)
	}
	return e, port, first
}

// An earlier request's mention goes with the next request (not current), after the current ones, and its ID passes
// the provenance check of that request.
func TestStepSendsEarlierMentions(t *testing.T) {
	e, _, first := mentionedChat(t)
	e.model.then(raw("r2", "reply", replyA1))
	st, err := e.svc.Ask(t.Context(), AskInput{
		ThreadID: first.Thread, Text: "а по матану?", Mentions: []values.Mention{mention(t, values.KindSubject, "s7", "Матан")},
	})
	if err != nil || st.Status != thread.StatusDone || len(st.Cards) != 1 {
		t.Fatalf("request 2 = %+v, %v", st, err)
	}
	want := []values.StepMention{
		{Ref: mustRef(t, values.KindSubject, "s7"), Current: true}, {Ref: mustRef(t, values.KindAssignment, "a1")},
	}
	got := e.model.last(t).Mentions
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("request 2 sends %+v, want %+v", got, want)
	}
	if card, ok := st.Cards[0].(values.ReplyCard); !ok || len(card.Assignments) != 1 || card.Assignments[0].ID() != "a1" {
		t.Fatalf("card = %+v, want the reply listing a1", st.Cards[0])
	}
}

// An object of an earlier request deleted since is neither sent nor accepted: the reply that lists it is refused.
func TestStepDropsDeletedEarlierMentions(t *testing.T) {
	e, port, first := mentionedChat(t)
	port.mu.Lock()
	port.exists = values.NewRefSet()
	port.mu.Unlock()

	e.model.then(raw("r2", "reply", replyA1)).then(raw("r3", "reply", `{"text":"задания больше нет"}`))
	st, err := e.svc.Ask(t.Context(), AskInput{ThreadID: first.Thread, Text: "и что теперь?"})
	if err != nil || st.Status != thread.StatusDone {
		t.Fatalf("request 2 = %+v, %v", st, err)
	}
	reqs := e.model.requests()
	if got := reqs[len(reqs)-2].Mentions; len(got) != 0 {
		t.Fatalf("request 2 sends the deleted object: %+v", got)
	}
	if got := lastResults(t, e); !got[0].IsError || got[0].ResultJSON != `{"error":"unknown_id"}` {
		t.Fatalf("result = %+v, want unknown_id", got)
	}
}

// A failing existence check aborts the step like any other read; nothing goes to the Hub.
func TestStepMentionCheckFails(t *testing.T) {
	e, port, first := mentionedChat(t)
	boom := errors.New("db down")
	port.mu.Lock()
	port.err = boom
	port.mu.Unlock()

	before := len(e.model.requests())
	st, err := e.svc.Ask(t.Context(), AskInput{ThreadID: first.Thread, Text: "и что теперь?"})
	if !errors.Is(err, boom) || st.Status != thread.StatusStopped || st.StopReason != values.StopOffline {
		t.Fatalf("request 2 = %+v, %v; want stopped offline", st, err)
	}
	if len(e.model.requests()) != before {
		t.Fatal("the step reached the model")
	}
}

// Without the Mentions port no earlier mention is sent or accepted.
func TestStepWithoutMentionsPort(t *testing.T) {
	e, _, first := mentionedChat(t)
	withMentions(t, e, nil)
	e.model.then(raw("r2", "reply", replyA1)).then(raw("r3", "reply", `{"text":"ок"}`))
	if _, err := e.svc.Ask(t.Context(), AskInput{ThreadID: first.Thread, Text: "и что теперь?"}); err != nil {
		t.Fatal(err)
	}
	reqs := e.model.requests()
	if got := reqs[len(reqs)-2].Mentions; len(got) != 0 {
		t.Fatalf("sent %+v without a port", got)
	}
	if got := lastResults(t, e); !got[0].IsError {
		t.Fatalf("result = %+v, want a refusal", got)
	}
}
