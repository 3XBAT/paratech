package agent

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
)

// replyDriver is a Driver whose log goes to a buffer, over reads with three assignments of which a reply may list some.
func replyDriver(t *testing.T, turns Turns) (*Driver, *chanSink, *reads, *bytes.Buffer) {
	t.Helper()
	r := newReads()
	r.assignments.views = []homeworkflow.AssignmentView{
		{ID: "a1", Title: "Лабораторная 2", Subject: "Физика", Due: time.Date(2026, 10, 24, 18, 0, 0, 0, moscow)},
		{ID: "a2", Title: "Эссе о реформах"},
		{ID: "a3", Title: "Не в списке", Subject: "Физика"},
	}
	r.library.resolve = map[string]subjectmatch.Match{"Физика": {ID: "s1", Name: "Физика"}}
	var buf bytes.Buffer
	sink := &chanSink{}
	deps := r.deps(turns, sink)
	deps.Log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	d, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	return d, sink, r, &buf
}

func lastReply(t *testing.T, sink *chanSink) values.ReplyCard {
	t.Helper()
	ups := sink.all()
	if len(ups) == 0 {
		t.Fatal("no update")
	}
	last := ups[len(ups)-1]
	if last.Status != thread.StatusDone || len(last.Cards) != 1 {
		t.Fatalf("last update = %+v", last)
	}
	r, ok := last.Cards[0].(values.ReplyCard)
	if !ok {
		t.Fatalf("card = %T", last.Cards[0])
	}
	return r
}

// TestReplyShowsTitlesFromLocalData: a reply listing assignments of the list_assignments result is shown with their
// titles, library subjects and deadlines from the local data, in the planner's order; an assignment deleted since is
// skipped. The planner got only IDs, and neither the result it got nor the log carries a title.
func TestReplyShowsTitlesFromLocalData(t *testing.T) {
	a1, a2, a9 := mustRef(t, values.KindAssignment, "a1"), mustRef(t, values.KindAssignment, "a2"), mustRef(t, values.KindAssignment, "a9")
	turns := (&scriptedTurns{}).
		then(step(thread.StatusPlanning, []agentturn.Call{readCall(t, "c1", values.ToolListAssignments, `{}`)}), nil).
		then(step(thread.StatusDone, nil, values.ReplyCard{Text: "Три задания.", Assignments: []values.Ref{a2, a9, a1}}), nil)
	d, sink, _, buf := replyDriver(t, turns)

	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "что горит"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()

	r := lastReply(t, sink)
	due, _ := values.NewDate(2026, time.October, 24)
	want := []values.AssignmentLine{
		{Assignment: a2, Title: "Эссе о реформах"},
		{Assignment: a1, Title: "Лабораторная 2", Subject: mustRef(t, values.KindSubject, "s1"), Due: due},
	}
	if len(r.Lines) != len(want) || r.Lines[0] != want[0] || r.Lines[1] != want[1] || r.Validate() != nil {
		t.Fatalf("lines = %+v", r.Lines)
	}

	sub := turns.report().submitted
	if len(sub) != 1 || strings.Contains(sub[0].Outcomes[0].Result.ResultJSON, "Лаборатор") ||
		!strings.Contains(sub[0].Outcomes[0].Result.ResultJSON, `"id":"a1"`) {
		t.Fatalf("submitted = %+v", sub)
	}
	for _, title := range []string{"Лабораторная", "Эссе", "Не в списке", "Три задания"} {
		if strings.Contains(buf.String(), title) {
			t.Fatalf("log has %q:\n%s", title, buf.String())
		}
	}
}

// TestReplyListWhenTheDataCannotBeRead: a failed read of the assignments shows the reply text without the list; a
// failed subject lookup shows the lines without a subject. The log names the failure, never a title.
func TestReplyListWhenTheDataCannotBeRead(t *testing.T) {
	a1 := mustRef(t, values.KindAssignment, "a1")
	reply := values.ReplyCard{Text: "Одно задание.", Assignments: []values.Ref{a1}}

	t.Run("assignments", func(t *testing.T) {
		turns := (&scriptedTurns{}).then(step(thread.StatusDone, nil, reply), nil)
		d, sink, r, buf := replyDriver(t, turns)
		r.assignments.err = errors.New("db is locked")
		if err := d.Ask(t.Context(), agentturn.AskInput{Text: "что горит"}); err != nil {
			t.Fatal(err)
		}
		d.Wait()
		if got := lastReply(t, sink); got.Text != reply.Text || len(got.Lines) != 0 {
			t.Fatalf("reply = %+v", got)
		}
		if !strings.Contains(buf.String(), opReplyLines) || strings.Contains(buf.String(), "Одно задание") {
			t.Fatalf("log:\n%s", buf.String())
		}
	})

	t.Run("subjects", func(t *testing.T) {
		turns := (&scriptedTurns{}).then(step(thread.StatusDone, nil, reply), nil)
		d, sink, r, _ := replyDriver(t, turns)
		r.library.err = errors.New("library is busy")
		if err := d.Ask(t.Context(), agentturn.AskInput{Text: "что горит"}); err != nil {
			t.Fatal(err)
		}
		d.Wait()
		got := lastReply(t, sink)
		if len(got.Lines) != 1 || got.Lines[0].Title != "Лабораторная 2" || !got.Lines[0].Subject.IsZero() || got.Lines[0].Due.IsZero() {
			t.Fatalf("lines = %+v", got.Lines)
		}
	})
}

// TestReplySkipsACorruptTitle: an assignment whose title is not valid UTF-8 or blank is skipped; the other lines and
// the text are still shown and the card stays valid.
func TestReplySkipsACorruptTitle(t *testing.T) {
	a1, a2 := mustRef(t, values.KindAssignment, "a1"), mustRef(t, values.KindAssignment, "a2")
	for name, title := range map[string]string{"invalid utf-8": "Эссе \xff", "blank": "  "} {
		t.Run(name, func(t *testing.T) {
			turns := (&scriptedTurns{}).then(step(thread.StatusDone, nil, values.ReplyCard{Text: "Два задания.", Assignments: []values.Ref{a2, a1}}), nil)
			d, sink, r, _ := replyDriver(t, turns)
			r.assignments.views[1].Title = title
			if err := d.Ask(t.Context(), agentturn.AskInput{Text: "что горит"}); err != nil {
				t.Fatal(err)
			}
			d.Wait()
			got := lastReply(t, sink)
			if got.Text != "Два задания." || len(got.Lines) != 1 || got.Lines[0].Assignment != a1 || got.Validate() != nil {
				t.Fatalf("reply = %+v", got)
			}
		})
	}
}

// TestReplyWithoutAssignmentsReadsNothing:a plain reply makes no read of the assignments.
func TestReplyWithoutAssignmentsReadsNothing(t *testing.T) {
	turns := (&scriptedTurns{}).then(step(thread.StatusDone, nil, values.ReplyCard{Text: "Готово."}), nil)
	d, sink, r, buf := replyDriver(t, turns)
	r.assignments.err = errors.New("must not be read")
	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "привет"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()
	if got := lastReply(t, sink); got.Lines != nil || strings.Contains(buf.String(), opReplyLines) {
		t.Fatalf("reply = %+v, log:\n%s", got, buf.String())
	}
}
