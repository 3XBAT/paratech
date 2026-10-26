package agentturn

import (
	"bytes"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
)

// listAssignments asks a request whose first step is list_assignments and reports the IDs of its result as seen.
func listAssignments(t *testing.T, e *env, ids ...string) Step {
	t.Helper()
	st := ask(t, e, "что горит на неделе")
	if len(st.Calls) != 1 || st.Calls[0].Call.Tool() != values.ToolListAssignments {
		t.Fatalf("first step = %+v", st)
	}
	o := Outcome{Result: values.ToolResult{CallID: st.Calls[0].Call.ID(), ResultJSON: `{"assignments":[]}`}}
	for _, id := range ids {
		o.Seen = append(o.Seen, mustRef(t, values.KindAssignment, id))
	}
	st, err := e.svc.Submit(t.Context(), SubmitInput{Turn: ref(st), Outcomes: []Outcome{o}})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return st
}

func replyOf(t *testing.T, st Step) values.ReplyCard {
	t.Helper()
	if st.Status != thread.StatusDone || len(st.Cards) != 1 {
		t.Fatalf("step = %+v, want one card of a finished request", st)
	}
	r, ok := st.Cards[0].(values.ReplyCard)
	if !ok {
		t.Fatalf("card = %T", st.Cards[0])
	}
	return r
}

// TestReplyListsAssignmentsOfTheResults: a reply lists the assignments list_assignments returned, in the planner's
// order and once each; the card carries only their IDs (the Driver adds titles), and the history keeps the sealed call
// and the bare "shown" result, nothing of the assignments.
func TestReplyListsAssignmentsOfTheResults(t *testing.T) {
	e := newEnv(t)
	e.model.then(raw("c1", "list_assignments", `{"status":"upcoming"}`)).
		then(raw("c2", "reply", `{"text":"Два задания, ближайший срок — 2026-10-24.","assignments":["a2","a1","a2"]}`))

	r := replyOf(t, listAssignments(t, e, "a1", "a2"))
	want := []values.Ref{mustRef(t, values.KindAssignment, "a2"), mustRef(t, values.KindAssignment, "a1")}
	if !slices.Equal(r.Assignments, want) || len(r.Lines) != 0 || r.Validate() != nil {
		t.Fatalf("reply = %+v", r)
	}

	all := e.history.all("th1")
	last := all[len(all)-1].Entry.Results
	if len(last) != 1 || last[0].CallID != "c2" || last[0].ResultJSON != `{"shown":true}` {
		t.Fatalf("reply result = %+v", last)
	}
}

// TestReplyWithAnIDOfUnknownProvenance: an invented ID, an ID of another kind and an assignment of an earlier thread
// are refused with unknown_id and no card is shown; the planner then replies without them. The assignment of the
// scope (the page the student asked on) may be listed.
func TestReplyWithAnIDOfUnknownProvenance(t *testing.T) {
	for name, ids := range map[string]string{
		"invented":            `["a9"]`,
		"one of two invented": `["a1","a9"]`,
		"lesson id":           `["l7"]`,
		"subject of the base": `["s1"]`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.model.then(raw("c1", "list_assignments", `{}`)).
				then(raw("c2", "reply", `{"text":"Заданий: 1.","assignments":`+ids+`}`)).
				then(raw("c3", "reply", `{"text":"Заданий: 1."}`))

			r := replyOf(t, listAssignments(t, e, "a1"))
			if len(r.Assignments) != 0 {
				t.Fatalf("reply = %+v, want the second reply without a list", r)
			}
			reqs := e.model.requests()
			got := reqs[len(reqs)-1].History
			res := got[len(got)-1].Results
			if len(res) != 1 || res[0].CallID != "c2" || !res[0].IsError || res[0].ResultJSON != `{"error":"unknown_id"}` {
				t.Fatalf("refusal = %+v", res)
			}
		})
	}

	t.Run("assignment of the scope", func(t *testing.T) {
		e := newEnv(t)
		e.model.then(raw("c1", "reply", `{"text":"Срок — 2026-10-30.","assignments":["a5"]}`))
		sc, err := values.NewScope(mustRef(t, values.KindSubject, "s1"), values.ScreenAssignments, values.Ref{},
			mustRef(t, values.KindAssignment, "a5"))
		if err != nil {
			t.Fatal(err)
		}
		st, err := e.svc.Ask(t.Context(), AskInput{Scope: sc, Text: "когда срок"})
		if err != nil {
			t.Fatal(err)
		}
		if r := replyOf(t, st); len(r.Assignments) != 1 || r.Assignments[0].ID() != "a5" {
			t.Fatalf("reply = %+v", r)
		}
	})
}

// TestTopicWithoutAnAssignmentEndsHonestly: a work on a topic needs an assignment or an upload for triage. A triage
// call without one, or with an ID the thread never saw, is refused and never reaches the Driver; the request ends
// with the planner's plain reply, never a work card or a half-done action.
func TestTopicWithoutAnAssignmentEndsHonestly(t *testing.T) {
	e := newEnv(t)
	honest := "По теме без задания работа пока не создаётся: она начинается с задания в «Задания»."
	e.model.then(raw("t1", "triage_assignment", `{}`)).
		then(raw("t2", "triage_assignment", `{"assignment":"a-topic"}`)).
		then(raw("r1", "reply", `{"text":"`+honest+`"}`))

	st := ask(t, e, "сделай презентацию на тему «Пётр I»")
	if r := replyOf(t, st); string(r.Text) != honest || len(st.Calls) != 0 {
		t.Fatalf("step = %+v", st)
	}
	reqs := e.model.requests()
	if len(reqs) != 3 {
		t.Fatalf("%d planner steps, want 3", len(reqs))
	}
	for i, want := range []string{`{"error":"invalid_arguments"}`, `{"error":"unknown_id"}`} {
		h := reqs[i+1].History
		if res := h[len(h)-1].Results; len(res) != 1 || !res[0].IsError || res[0].ResultJSON != want {
			t.Fatalf("step %d refusal = %+v, want %s", i+1, res, want)
		}
	}
	th, err := e.threads.Get(t.Context(), "th1")
	if err != nil {
		t.Fatal(err)
	}
	if turn, _ := th.Turn(); turn.Budget().Writes != 0 {
		t.Fatalf("writes = %d, want none admitted", turn.Budget().Writes)
	}
}

// TestReplyTextsStayOutOfTheLog: the texts of a reply, shown or refused, are never logged.
func TestReplyTextsStayOutOfTheLog(t *testing.T) {
	e := newEnv(t)
	var buf bytes.Buffer
	e.svc.log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e.model.then(raw("c1", "reply", `{"text":"текст-отказа","assignments":["a9"]}`)).
		then(raw("c2", "reply", `{"text":"текст-ответа"}`))

	replyOf(t, ask(t, e, "что горит"))
	if out := buf.String(); strings.Contains(out, "текст-") {
		t.Fatalf("log has a reply text:\n%s", out)
	}
}
