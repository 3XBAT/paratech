package agentturn

import (
	"errors"
	"reflect"
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
)

// «Повторить» of a done request: a new request in the same scope with the same text and new keys; the old button is
// stale afterwards.
func TestRerunDoneRequest(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("r1", "reply", `{"text":"две пары"}`)).then(raw("r2", "reply", `{"text":"снова две пары"}`))
	st := ask(t, e, "какие пары")
	if st.Status != thread.StatusDone {
		t.Fatalf("first request = %+v", st)
	}

	got, err := e.svc.Rerun(t.Context(), ref(st))
	if err != nil || got.Turn != 2 || got.Status != thread.StatusDone || got.Scope != subjectScope(t) {
		t.Fatalf("Rerun = %+v, %v", got, err)
	}
	req := e.model.last(t)
	if req.Key != "ag:th1:2:1" || req.Scope != subjectScope(t) {
		t.Fatalf("repeat request = %q %+v", req.Key, req.Scope)
	}
	if h := req.History; h[len(h)-1].StudentText != "какие пары" {
		t.Fatalf("repeat history = %+v", h)
	}
	if e.effects.asked[0] != ref(st) {
		t.Fatalf("effects asked for %+v", e.effects.asked)
	}
	stored := e.history.all(st.Thread)
	if last := stored[len(stored)-3]; last.Turn != 2 || last.Entry.StudentText != "какие пары" {
		t.Fatalf("stored text of the repeat = %+v", stored)
	}

	// A double click: the second press names the old request.
	if _, err := e.svc.Rerun(t.Context(), ref(st)); !errors.Is(err, ErrStaleTurn) {
		t.Fatalf("second Rerun of the old request = %v", err)
	}
	if n := len(e.model.requests()); n != 2 {
		t.Fatalf("Hub steps = %d, want 2", n)
	}
}

// The planner history of a repeat ends with the repeat's own text, after the earlier requests as pairs «text, reply»
// (the repeated request's text is one of them). Pinned so a change to the window (owner bug of 2026-10-24, TASK-183)
// is deliberate.
func TestRerunHistoryEndsWithItsText(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("r1", "reply", `{"text":"горутина — это"}`)).
		then(raw("r2", "reply", `{"text":"рыбаков — это"}`)).
		then(raw("r3", "reply", `{"text":"рыбаков — это"}`))
	ask(t, e, "что такое горутина")
	st := ask(t, e, "кто такой рыбаков")
	if _, err := e.svc.Rerun(t.Context(), ref(st)); err != nil {
		t.Fatal(err)
	}
	h := e.model.last(t).History
	want := []values.HistoryTurn{
		{StudentText: "что такое горутина"}, {AgentReply: "reply: горутина — это"},
		{StudentText: "кто такой рыбаков"}, {AgentReply: "reply: рыбаков — это"},
		{StudentText: "кто такой рыбаков"},
	}
	if len(h) != len(want) {
		t.Fatalf("history = %+v", h)
	}
	for i, w := range want {
		if h[i].StudentText != w.StudentText || h[i].AgentReply != w.AgentReply || h[i].Calls != nil || h[i].Results != nil {
			t.Fatalf("history[%d] = %+v, want %+v", i, h[i], w)
		}
	}
}

// Which requests can be repeated, through the case: finished and stopped ones; a running one is busy, one waiting for
// the plan is not allowed.
func TestRerunByStatus(t *testing.T) {
	plan := `{"summary":"План","steps":[{"tool":"create_subject","arguments":{"name":"Химия"}}]}`
	tests := []struct {
		name    string
		setup   func(t *testing.T, e *env) Step
		wantErr error
	}{
		{"done", func(t *testing.T, e *env) Step {
			e.model.then(raw("r1", "reply", `{"text":"ок"}`))
			return ask(t, e, "привет")
		}, nil},
		{"failed step stopped", func(t *testing.T, e *env) Step {
			e.model.fail(backoff.Permanent(errors.New("hub refused")))
			return ask(t, e, "привет")
		}, nil},
		{"stopped offline", func(t *testing.T, e *env) Step {
			e.model.fail(backoff.Transient(shared.ErrNetworkUnavailable))
			return ask(t, e, "привет")
		}, nil},
		{"cancelled", func(t *testing.T, e *env) Step {
			e.model.then(raw("p1", "show_plan", plan))
			st, err := e.svc.Cancel(t.Context(), ref(ask(t, e, "добавь")))
			if err != nil {
				t.Fatal(err)
			}
			return st
		}, nil},
		{"awaiting the plan", func(t *testing.T, e *env) Step {
			e.model.then(raw("p1", "show_plan", plan))
			return ask(t, e, "добавь")
		}, thread.ErrInvalidTransition},
		{"running calls", func(t *testing.T, e *env) Step {
			e.model.then(raw("c1", "list_subjects", `{}`))
			return ask(t, e, "мои предметы")
		}, thread.ErrTurnInProgress},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			st := tt.setup(t, e)
			before, _ := e.threads.Get(t.Context(), st.Thread)
			steps := len(e.model.requests())
			e.model.then(raw("r9", "reply", `{"text":"повтор"}`))

			got, err := e.svc.Rerun(t.Context(), ref(st))
			if tt.wantErr != nil {
				after, _ := e.threads.Get(t.Context(), st.Thread)
				if !errors.Is(err, tt.wantErr) || !reflect.DeepEqual(after.State(), before.State()) ||
					len(e.model.requests()) != steps {
					t.Fatalf("refused Rerun = %v, changed %v", err, !reflect.DeepEqual(after.State(), before.State()))
				}
				return
			}
			if err != nil || got.Turn != st.Turn+1 || got.Status != thread.StatusDone {
				t.Fatalf("Rerun = %+v, %v", got, err)
			}
			if req := e.model.last(t); req.Key != "ag:th1:2:1" {
				t.Fatalf("key = %q", req.Key)
			}
		})
	}
}

// Live effects block the repeat and nothing is stored or sent; once they are undone (Live false) the repeat runs.
func TestRerunOverLiveEffects(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("w1", "create_subject", `{"name":"Химия"}`)).then(raw("r1", "reply", `{"text":"создан"}`))
	st := ask(t, e, "создай химию")
	st, err := e.svc.Submit(t.Context(), SubmitInput{Turn: ref(st), Outcomes: []Outcome{{
		Result: values.ToolResult{CallID: "w1", ResultJSON: `{"subject":"s2"}`}, Created: mustRef(t, values.KindSubject, "s2"),
	}}})
	if err != nil || st.Status != thread.StatusDone {
		t.Fatalf("request = %+v, %v", st, err)
	}
	e.effects.set(ref(st), true)
	before, _ := e.threads.Get(t.Context(), st.Thread)
	entries := len(e.history.all(st.Thread))

	if _, err := e.svc.Rerun(t.Context(), ref(st)); !errors.Is(err, thread.ErrRerunBlocked) {
		t.Fatalf("Rerun over a done action = %v", err)
	}
	after, _ := e.threads.Get(t.Context(), st.Thread)
	if !reflect.DeepEqual(after.State(), before.State()) || len(e.history.all(st.Thread)) != entries || len(e.model.requests()) != 2 {
		t.Fatal("a blocked Rerun changed something")
	}

	e.effects.set(ref(st), false) // «Отменить всё»: the action is UNDONE
	e.model.then(raw("w2", "create_subject", `{"name":"Химия"}`))
	got, err := e.svc.Rerun(t.Context(), ref(st))
	if err != nil || got.Turn != 2 || len(got.Calls) != 1 || got.Calls[0].Call.ID() != "w2" {
		t.Fatalf("Rerun after the undo = %+v, %v", got, err)
	}

	e.effects.err = errors.New("journal unreadable")
	if _, err := e.svc.Rerun(t.Context(), ref(got)); err == nil || errors.Is(err, thread.ErrRerunBlocked) {
		t.Fatalf("Rerun with an unreadable journal = %v", err)
	}
}

// A request stopped (no connection) with calls waiting: they are closed not done under the old request, the repeat
// starts with the old text and scope (an upload in the text included).
func TestRerunFromStoppedWithOpenCalls(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("c1", "inspect_upload", `{"upload":"u1"}`), raw("c2", "list_subjects", `{}`))
	scope := values.Scope{}
	st, err := e.svc.Ask(t.Context(), AskInput{Scope: scope, Text: "что это", Uploads: []values.Ref{mustRef(t, values.KindUpload, "u1")}})
	if err != nil || len(st.Calls) != 2 {
		t.Fatalf("Ask = %+v, %v", st, err)
	}
	th, _ := e.threads.Get(t.Context(), st.Thread)
	if err := th.Stop(values.StopOffline, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := e.threads.Save(t.Context(), th); err != nil {
		t.Fatal(err)
	}

	e.model.then(raw("r1", "reply", `{"text":"это файл"}`))
	got, err := e.svc.Rerun(t.Context(), ref(st))
	if err != nil || got.Turn != 2 || got.Status != thread.StatusDone || got.Scope != scope {
		t.Fatalf("Rerun = %+v, %v", got, err)
	}
	stored := e.history.all(st.Thread)
	// text, echo, results of turn 1; text of turn 2, its echo and results.
	if len(stored) != 6 || stored[2].Turn != 1 || len(stored[2].Entry.Results) != 2 ||
		stored[2].Entry.Results[0].ResultJSON != `{"error":"not_done"}` || stored[3].Turn != 2 {
		t.Fatalf("stored history = %+v", stored)
	}
	if h := e.model.last(t).History; len(h) != 3 || h[2].StudentText != "что это\n[upload:u1]" ||
		h[1].AgentReply != thread.NoReply+"\nactions: inspect_upload error, list_subjects error" {
		t.Fatalf("repeat history = %+v", h)
	}
	if _, err := e.svc.Submit(t.Context(), SubmitInput{Turn: ref(st), Outcomes: []Outcome{okResult("c1", `{}`)}}); !errors.Is(err, ErrStaleTurn) {
		t.Fatalf("late results of the old request = %v", err)
	}
}

func TestRerunWithoutEffects(t *testing.T) {
	e := newEnv(t)
	e.svc.effects = nil
	e.model.then(raw("r1", "reply", `{"text":"ок"}`))
	st := ask(t, e, "привет")
	if _, err := e.svc.Rerun(t.Context(), ref(st)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Rerun without Effects = %v", err)
	}
}
