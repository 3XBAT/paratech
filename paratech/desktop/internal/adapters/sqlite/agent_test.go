package sqlite

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

var agentT0 = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

func agentAt(min int) time.Time { return agentT0.Add(time.Duration(min) * time.Minute) }

type fixedNow struct{ t time.Time }

func (f fixedNow) Now() time.Time { return f.t }

func agentRef(t *testing.T, kind values.IDKind, id string) values.Ref {
	t.Helper()
	r, err := values.NewRef(kind, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func agentCall(t *testing.T, id, tool, args string) values.Call {
	t.Helper()
	c, err := values.ParseCall(id, tool, args)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func agentMention(t *testing.T, kind values.IDKind, id, label string) values.Mention {
	t.Helper()
	m, err := values.NewMention(agentRef(t, kind, id), label)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// subjectThread is a chat titled by its first text whose request, in subject s1 with two mentions, has an approved
// plan, an executed create step with the created folder recorded, and an answered call waiting to be taken.
func subjectThread(t *testing.T) *thread.Thread {
	t.Helper()
	s1 := agentRef(t, values.KindSubject, "s1")
	th, err := thread.New("th1", agentT0)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := values.NewScope(s1, values.ScreenSubjects, agentRef(t, values.KindLesson, "l1"), values.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	mentions := []values.Mention{
		agentMention(t, values.KindLesson, "l2", "Лекция 3 «Интегралы»"), agentMention(t, values.KindMaterial, "m1", "Лабы.pdf"),
	}
	if err := th.Ask(thread.Request{Scope: scope, Mentions: mentions, Text: "Разложи лабы по папкам"}, agentAt(1)); err != nil {
		t.Fatal(err)
	}
	if err := th.Observe(agentRef(t, values.KindUpload, "u1")); err != nil {
		t.Fatal(err)
	}
	plan := agentCall(t, "p1", "show_plan", `{"summary":"План","steps":[`+
		`{"tool":"create_folder","arguments":{"subject":"s1","name":"Лабы"}},`+
		`{"tool":"place_upload","arguments":{"upload":"u1","subject":"s1"}}]}`).Args().PlanSteps("steps")
	ctxRefs := values.NewRefSet(scope.Refs()...)
	if err := th.ProposePlan(plan, ctxRefs, agentAt(2)); err != nil {
		t.Fatal(err)
	}
	if err := th.ApprovePlan(agentAt(3)); err != nil {
		t.Fatal(err)
	}
	if _, err := th.BeginStep(agentAt(4)); err != nil {
		t.Fatal(err)
	}
	if err := th.EndStep(1, 500, agentAt(5)); err != nil {
		t.Fatal(err)
	}
	if err := th.Expect([]values.RawCall{{CallID: "w1", Tool: "create_folder", ArgumentsJSON: `{}`}}, agentAt(5)); err != nil {
		t.Fatal(err)
	}
	if _, err := th.Admit(agentCall(t, "w1", "create_folder", `{"subject":"s1","name":"Лабы"}`), thread.Facts{Context: ctxRefs}, agentAt(5)); err != nil {
		t.Fatal(err)
	}
	res := values.ToolResult{CallID: "w1", ResultJSON: `{"folder":"f1"}`}
	if err := th.Answer(res, agentRef(t, values.KindFolder, "f1"), agentAt(6)); err != nil {
		t.Fatal(err)
	}
	return th
}

func sameThread(t *testing.T, got, want *thread.Thread) {
	t.Helper()
	g, w := got.State(), want.State()
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("stored thread differs:\n got %+v\n    %+v\nwant %+v\n    %+v", g, g.Current, w, w.Current)
	}
}

func TestAgentThreadRoundTrip(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewAgentThreadRepository(db)
	th := subjectThread(t)
	if err := repo.Save(ctx, th); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if th.Version() != 1 {
		t.Fatalf("version = %d", th.Version())
	}
	got, err := repo.Get(ctx, "th1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	sameThread(t, got, th)

	// The next step: results taken, a new step open; saved with a compare-and-swap.
	if _, ok := got.TakeResults(agentAt(7)); !ok {
		t.Fatal("results not complete")
	}
	if _, err := got.BeginStep(agentAt(8)); err != nil {
		t.Fatal(err)
	}
	if err := got.Stop(values.StopFailed, agentAt(9)); err != nil {
		t.Fatal(err)
	}
	if err := got.Retry(agentAt(9)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, got); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	again, err := repo.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sameThread(t, again, got)
	if again.Title() != "Разложи лабы по папкам" {
		t.Fatalf("title = %q", again.Title())
	}
	if turn, _ := again.Turn(); len(turn.Mentions()) != 2 || turn.Mentions()[0].Label() != "Лекция 3 «Интегралы»" {
		t.Fatalf("mentions = %+v", turn.Mentions())
	}
	if key, _ := again.StepKey(); key != "ag:th1:1:2:r1" {
		t.Fatalf("restored step key = %q", key)
	}

	// The stale copy loses.
	if err := repo.Save(ctx, th); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("stale Save = %v", err)
	}
	if _, err := repo.Get(ctx, "nope"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get of a missing thread = %v", err)
	}
	if _, err := NewAgentThreadRepository(openTest(t, nil)).Current(ctx); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Current without chats = %v", err)
	}
}

// Chats are not tied to a subject: any number of them is stored; only an id stored first is refused.
func TestAgentChatsAreManyAndIDsUnique(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewAgentThreadRepository(db)
	a, _ := thread.New("a", agentT0)
	b, _ := thread.New("b", agentT0)
	if err := repo.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, b); err != nil {
		t.Fatalf("a second chat = %v", err)
	}
	again, _ := thread.New("a", agentT0)
	if err := repo.Save(ctx, again); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("an id stored first = %v", err)
	}
}

// savedChat stores a chat created at agentT0 whose one request started at minute active and ended; archived > 0
// archives it at that minute.
func savedChat(t *testing.T, repo *AgentThreadRepository, id shared.ThreadID, active, archived int) *thread.Thread {
	t.Helper()
	th, err := thread.New(id, agentT0)
	if err != nil {
		t.Fatal(err)
	}
	if err := th.Ask(thread.Request{Text: "просьба " + string(id)}, agentAt(active)); err != nil {
		t.Fatal(err)
	}
	if err := th.Complete(agentAt(active)); err != nil {
		t.Fatal(err)
	}
	if archived > 0 {
		if err := th.Archive(agentAt(archived)); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.Save(context.Background(), th); err != nil {
		t.Fatal(err)
	}
	return th
}

func chatIDs(chats []*thread.Thread) []shared.ThreadID {
	out := make([]shared.ThreadID, len(chats))
	for i, c := range chats {
		out[i] = c.ID()
	}
	return out
}

// The current chat is the unarchived one active last; List shows the unarchived chats by last activity, then the
// archive; archive, restore and a new request move a chat between them.
func TestAgentChatsCurrentListAndArchive(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewAgentThreadRepository(db)
	savedChat(t, repo, "old", 1, 0)
	savedChat(t, repo, "mid", 5, 0)
	savedChat(t, repo, "new", 9, 20)   // the newest activity, but archived
	savedChat(t, repo, "arch", 3, 21)  // archived, older
	savedChat(t, repo, "tie-a", 5, 0)  // same instant as mid: the greater id wins
	savedChat(t, repo, "tie-b", 2, 22) // archived last, but the archive is ordered by activity too

	cur, err := repo.Current(ctx)
	if err != nil || cur.ID() != "tie-a" {
		t.Fatalf("Current = %v, %v; want tie-a", cur, err)
	}
	list, err := repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []shared.ThreadID{"tie-a", "mid", "old", "new", "arch", "tie-b"}
	if got := chatIDs(list); !reflect.DeepEqual(got, want) {
		t.Fatalf("List = %v, want %v", got, want)
	}
	for _, c := range list {
		stored, err := repo.Get(ctx, c.ID())
		if err != nil {
			t.Fatal(err)
		}
		sameThread(t, c, stored)
	}

	// «Вернуть» the newest archived chat: it is current again.
	back, err := repo.Get(ctx, "new")
	if err != nil {
		t.Fatal(err)
	}
	if err := back.Restore(agentAt(30)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, back); err != nil {
		t.Fatal(err)
	}
	if cur, err := repo.Current(ctx); err != nil || cur.ID() != "new" || cur.Archived() {
		t.Fatalf("Current after restore = %v, %v", cur, err)
	}

	// Archive every unarchived chat: there is no current chat; a new request in an archived one restores it.
	for _, id := range []shared.ThreadID{"new", "tie-a", "mid", "old"} {
		th, err := repo.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if err := th.Archive(agentAt(40)); err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, th); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.Current(ctx); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Current with every chat archived = %v", err)
	}
	old, _ := repo.Get(ctx, "old")
	if err := old.Ask(thread.Request{Text: "ещё"}, agentAt(50)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, old); err != nil {
		t.Fatal(err)
	}
	if cur, err := repo.Current(ctx); err != nil || cur.ID() != "old" || !cur.LastActive().Equal(agentAt(50)) {
		t.Fatalf("Current after a request in the archive = %v, %v", cur, err)
	}
	if list, _ := repo.List(ctx); list[0].ID() != "old" || len(list) != 6 {
		t.Fatalf("List = %v", chatIDs(list))
	}
}

// RequestScope reads the scope stored with any request of a chat, the subject of that request.
func TestAgentRequestScope(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewAgentThreadRepository(db)
	th := subjectThread(t)
	if err := repo.Save(ctx, th); err != nil {
		t.Fatal(err)
	}
	if _, ok := th.TakeResults(agentAt(7)); !ok {
		t.Fatal("results not complete")
	}
	if err := th.Complete(agentAt(8)); err != nil {
		t.Fatal(err)
	}
	if err := th.Ask(thread.Request{Text: "без предмета"}, agentAt(9)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, th); err != nil {
		t.Fatal(err)
	}
	first, err := repo.RequestScope(ctx, "th1", 1)
	if err != nil || first.Subject().ID() != "s1" || first.Lesson().ID() != "l1" {
		t.Fatalf("scope of request 1 = %+v, %v", first, err)
	}
	if second, err := repo.RequestScope(ctx, "th1", 2); err != nil || !second.Subject().IsZero() {
		t.Fatalf("scope of request 2 = %+v, %v", second, err)
	}
	if _, err := repo.RequestScope(ctx, "th1", 3); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("scope of a missing request = %v", err)
	}
}

func TestAgentThreadRunningAndEarlierTurns(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewAgentThreadRepository(db)

	waiting, _ := thread.New("w", agentT0)
	if err := waiting.StartRequest(values.Scope{}, agentAt(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := waiting.AskQuestions(agentAt(2)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, waiting); err != nil {
		t.Fatal(err)
	}
	if running, err := repo.ListRunning(ctx); err != nil || len(running) != 0 {
		t.Fatalf("ListRunning with a waiting request = %d, %v", len(running), err)
	}

	// A new request cancels the waiting one, in storage too.
	if err := waiting.StartRequest(values.Scope{}, agentAt(3)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, waiting); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.sql.QueryRowContext(ctx, "SELECT status FROM agent_turns WHERE thread_id = 'w' AND number = 1").Scan(&status); err != nil || status != "CANCELLED" {
		t.Fatalf("earlier request = %q, %v", status, err)
	}
	running, err := repo.ListRunning(ctx)
	if err != nil || len(running) != 1 || running[0].ID() != "w" {
		t.Fatalf("ListRunning = %v, %v", running, err)
	}
}

func TestAgentHistory(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	if err := NewAgentThreadRepository(db).Save(ctx, subjectThread(t)); err != nil {
		t.Fatal(err)
	}
	h := NewAgentHistory(db, fixedNow{agentT0})
	entries := []values.HistoryEntry{
		{Turn: 1, Entry: values.HistoryTurn{StudentText: "создай папку"}},
		{Turn: 1, Entry: values.HistoryTurn{Calls: &values.SealedCalls{
			Calls: []values.RawCall{{CallID: "w1", Tool: "create_folder", ArgumentsJSON: `{"name":"Лабы"}`}}, Seal: []byte{0, 1, 255},
		}}},
		{Turn: 1, Entry: values.HistoryTurn{Results: []values.ToolResult{{CallID: "w1", ResultJSON: `{"error":"x"}`, IsError: true}}}},
		{Turn: 2, Entry: values.HistoryTurn{StudentText: "ещё"}},
	}
	for _, e := range entries {
		if err := h.Append(ctx, "th1", e.Turn, e.Entry); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	got, err := h.Since(ctx, "th1", 0, 100)
	if err != nil || !reflect.DeepEqual(got, entries) {
		t.Fatalf("Since = %+v, %v", got, err)
	}
	if got, _ := h.Since(ctx, "th1", 2, 100); len(got) != 1 || got[0].Entry.StudentText != "ещё" {
		t.Fatalf("Since turn 2 = %+v", got)
	}
	if got, _ := h.Since(ctx, "th1", 0, 2); !reflect.DeepEqual(got, entries[2:]) {
		t.Fatalf("last two = %+v", got)
	}
	if err := h.Append(ctx, "th1", 2, values.HistoryTurn{}); !errors.Is(err, values.ErrInvalidResult) {
		t.Fatalf("Append of an empty entry = %v", err)
	}
	if err := h.Append(ctx, "nope", 1, entries[0].Entry); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("Append to a missing thread = %v", err)
	}
}

// The entries of dropped requests (a restart from an earlier message, ADR-0028) are left out by the read itself, so
// they take no place under the limit; their rows stay.
func TestAgentHistoryLeavesOutDroppedRequests(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	repo := NewAgentThreadRepository(db)
	h := NewAgentHistory(db, fixedNow{agentT0})
	th, err := thread.New("th1", agentT0)
	if err != nil {
		t.Fatal(err)
	}
	var want []values.HistoryEntry
	for turn := 1; turn <= 4; turn++ {
		if err := th.Ask(thread.Request{Text: fmt.Sprintf("просьба %d", turn)}, agentAt(turn)); err != nil {
			t.Fatal(err)
		}
		if err := th.Complete(agentAt(turn)); err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, th); err != nil {
			t.Fatal(err)
		}
		entries := []values.HistoryEntry{
			{Turn: turn, Entry: values.HistoryTurn{StudentText: fmt.Sprintf("просьба %d", turn)}},
			{Turn: turn, Entry: values.HistoryTurn{Calls: &values.SealedCalls{
				Calls: []values.RawCall{{CallID: "r1", Tool: "reply", ArgumentsJSON: `{"text":"ок"}`}}, Seal: []byte{1},
			}}},
			{Turn: turn, Entry: values.HistoryTurn{Results: []values.ToolResult{{CallID: "r1", ResultJSON: `{"ok":true}`}}}},
		}
		for _, e := range entries {
			if err := h.Append(ctx, "th1", e.Turn, e.Entry); err != nil {
				t.Fatal(err)
			}
		}
		if turn == 1 || turn == 4 {
			want = append(want, entries...)
		}
	}
	mustExec(t, db, `UPDATE agent_turns SET dropped_by = 4 WHERE thread_id = 'th1' AND number IN (2, 3)`)

	tests := []struct {
		name        string
		from, limit int
		want        []values.HistoryEntry
	}{
		{"all", 0, 100, want},
		{"the limit counts live entries only", 0, 6, want},
		{"cut inside the live ones", 0, 4, want[2:]},
		{"from a dropped request on", 2, 100, want[3:]},
	}
	for _, tt := range tests {
		got, err := h.Since(ctx, "th1", tt.from, tt.limit)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("%s: Since = %+v, %v\nwant %+v", tt.name, got, err, tt.want)
		}
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM agent_steps WHERE thread_id = 'th1'"); n != 12 {
		t.Fatalf("stored entries = %d, want all 12 kept", n)
	}
	mustExec(t, db, `UPDATE agent_turns SET dropped_by = 0 WHERE thread_id = 'th1'`)
	if got, _ := h.Since(ctx, "th1", 0, 6); len(got) != 6 || got[0].Turn != 3 {
		t.Fatalf("without dropped requests the newest 6 are of requests 3 and 4: %+v", got)
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE agent_turns SET dropped_by = 1 WHERE thread_id = 'th1' AND number = 2`); !errors.Is(MapError(ctx, err), ErrConstraint) {
		t.Fatalf("dropped by an earlier request: %v, want ErrConstraint", err)
	}
}

func TestAgentActions(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	if err := NewAgentThreadRepository(db).Save(ctx, subjectThread(t)); err != nil {
		t.Fatal(err)
	}
	repo := NewAgentActionRepository(db)
	due := time.Date(2026, 11, 1, 18, 30, 0, 0, time.UTC)
	comps := []struct {
		tool values.Tool
		comp action.Compensation
	}{
		{values.ToolCreateSubject, action.RemoveSubject{Subject: agentRef(t, values.KindSubject, "s2")}},
		{values.ToolCreateFolder, action.RemoveFolder{Subject: agentRef(t, values.KindSubject, "s1"), Folder: agentRef(t, values.KindFolder, "f1")}},
		{values.ToolPlaceUpload, action.ReturnToInbox{Material: agentRef(t, values.KindMaterial, "m1")}},
		{values.ToolPlaceUpload, action.ReturnToInbox{Material: agentRef(t, values.KindMaterial, "m1"), Lesson: agentRef(t, values.KindLesson, "l1")}},
		{values.ToolLinkMaterial, action.UnlinkMaterial{Material: agentRef(t, values.KindMaterial, "m1"), Lesson: agentRef(t, values.KindLesson, "l1")}},
		{values.ToolSetLessonInfo, action.RestoreLessonInfo{Recording: agentRef(t, values.KindRecording, "r1"), OldSubject: "Физика (лаб.)", OldKind: "lab"}},
		{values.ToolSetLessonInfo, action.RestoreLessonInfo{Recording: agentRef(t, values.KindRecording, "r2")}},
		{values.ToolEditDue, action.RestoreDue{Assignment: agentRef(t, values.KindAssignment, "a1"), OldDue: due}},
		{values.ToolEditDue, action.RestoreDue{Assignment: agentRef(t, values.KindAssignment, "a2")}},
		{values.ToolMakeSummary, action.CancelJob{Job: agentRef(t, values.KindJob, "j1")}},
		{values.ToolTriageAssignment, action.RemoveWork{Work: agentRef(t, values.KindWork, "w1")}},
		{values.ToolWriteWork, action.StopWork{Work: agentRef(t, values.KindWork, "w1"), Job: agentRef(t, values.KindJob, "j2")}},
		{values.ToolCreateAssignment, action.RemoveAssignment{Assignment: agentRef(t, values.KindAssignment, "ag-1")}},
	}
	for i, tc := range comps {
		id := shared.ActionID(fmt.Sprintf("a%d", i))
		spec := action.Spec{ID: id, Thread: "th1", Turn: 1, CallID: fmt.Sprintf("c%d", i), Tool: tc.tool}
		if rm, ok := tc.comp.(action.RemoveAssignment); ok {
			spec.Intent = action.Intent{Target: rm.Assignment} // a create names the ID it makes (ADR-0029)
		}
		a, err := action.New(spec, agentAt(i))
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, a); err != nil {
			t.Fatalf("insert %s: %v", tc.comp.Kind(), err)
		}
		if err := a.Complete(tc.comp, 3, agentAt(i).Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, a); err != nil {
			t.Fatalf("update %s: %v", tc.comp.Kind(), err)
		}
		got, err := repo.Get(ctx, id)
		if err != nil || !reflect.DeepEqual(got.State(), a.State()) {
			t.Fatalf("%s: got %+v, %v\nwant %+v", tc.comp.Kind(), got.State(), err, a.State())
		}
	}

	proposal := agentRef(t, values.KindProposal, "p1")
	p, err := action.NewProposed(action.Spec{ID: "ap", Thread: "th1", Turn: 1, CallID: "cp", Tool: values.ToolProposeSend}, proposal, agentAt(20))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, p); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.Get(ctx, "ap"); err != nil || got.Proposal() != proposal {
		t.Fatalf("proposed = %+v, %v", got, err)
	}

	pending, _ := action.New(action.Spec{ID: "pending", Thread: "th1", Turn: 2, CallID: "c1", Tool: values.ToolCreateSubject}, agentAt(30))
	if err := repo.Save(ctx, pending); err != nil {
		t.Fatal(err)
	}
	dup, _ := action.New(action.Spec{ID: "dup", Thread: "th1", Turn: 2, CallID: "c1", Tool: values.ToolCreateSubject}, agentAt(31))
	if err := repo.Save(ctx, dup); !errors.Is(err, shared.ErrDuplicate) {
		t.Fatalf("second entry of a call = %v", err)
	}
	stale, _ := repo.Get(ctx, "pending")
	if err := pending.Fail(agentAt(32)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, pending); err != nil {
		t.Fatal(err)
	}
	if err := stale.Fail(agentAt(33)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, stale); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("stale Save = %v", err)
	}

	list, err := repo.ListByThread(ctx, "th1", 3)
	if err != nil || len(list) != 3 || list[0].ID() != "pending" || list[1].ID() != "ap" {
		t.Fatalf("ListByThread = %v, %v", list, err)
	}
	open, _ := action.New(action.Spec{ID: "open", Thread: "th1", Turn: 3, CallID: "c1", Tool: values.ToolCreateSubject}, agentAt(40))
	if err := repo.Save(ctx, open); err != nil {
		t.Fatal(err)
	}
	unfinished, err := repo.ListUnfinished(ctx)
	if err != nil || len(unfinished) != 1 || unfinished[0].ID() != "open" {
		t.Fatalf("ListUnfinished = %v, %v", unfinished, err)
	}
	if _, err := repo.Get(ctx, "nope"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get of a missing entry = %v", err)
	}
}

// The intent recorded before an action, the refusal of its Undo, the lookup by call and the journal of every thread.
func TestAgentActionIntentRefusalAndLookups(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	if err := NewAgentThreadRepository(db).Save(ctx, subjectThread(t)); err != nil {
		t.Fatal(err)
	}
	repo := NewAgentActionRepository(db)
	a1 := agentRef(t, values.KindAssignment, "a1")
	m1 := agentRef(t, values.KindMaterial, "m1")
	intents := []struct {
		tool values.Tool
		in   action.Intent
	}{
		{values.ToolEditDue, action.Intent{Target: a1, Before: 4, Prior: action.RestoreDue{Assignment: a1, OldDue: time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC)},
			Next: action.RestoreDue{Assignment: a1, OldDue: time.Date(2026, 11, 3, 18, 0, 0, 0, time.UTC)}}},
		{values.ToolPlaceUpload, action.Intent{Target: m1, Before: 2, Prior: action.ReturnToInbox{Material: m1, Lesson: agentRef(t, values.KindLesson, "l1")}}},
		{values.ToolCreateSubject, action.Intent{Name: "Химия (лаб.)"}},
		{values.ToolMakeSummary, action.Intent{}},
	}
	for i, tc := range intents {
		id := shared.ActionID(fmt.Sprintf("i%d", i))
		a, err := action.New(action.Spec{ID: id, Thread: "th1", Turn: 5, CallID: fmt.Sprintf("c%d", i), Tool: tc.tool, Intent: tc.in}, agentAt(i))
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, a); err != nil {
			t.Fatal(err)
		}
		got, err := repo.GetByCall(ctx, "th1", 5, fmt.Sprintf("c%d", i))
		if err != nil || !reflect.DeepEqual(got.State(), a.State()) {
			t.Fatalf("%s: got %+v, %v\nwant %+v", tc.tool, got.State(), err, a.State())
		}
	}
	if _, err := repo.GetByCall(ctx, "th1", 6, "c0"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("GetByCall of another request = %v", err)
	}

	refused, _ := repo.Get(ctx, "i0")
	steps := []func() error{
		func() error { return refused.Complete(action.RestoreDue{Assignment: a1}, 5, agentAt(10)) },
		func() error { return refused.BeginUndo(agentAt(11)) },
		func() error { return refused.RefuseUndo(action.UndoResultReady, agentAt(12)) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, refused); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := repo.Get(ctx, "i0"); err != nil || got.Refusal() != action.UndoResultReady || got.Intent() != intents[0].in {
		t.Fatalf("refused = %+v, %v", got, err)
	}

	recent, err := repo.ListRecent(ctx, 2)
	if err != nil || len(recent) != 2 || recent[0].ID() != "i3" || recent[1].ID() != "i2" {
		t.Fatalf("ListRecent = %v, %v", recent, err)
	}
	mustExec(t, db, `UPDATE agent_actions SET undo_refusal = '' WHERE id = 'i0'`)
	if _, err := repo.Get(ctx, "i0"); !errors.Is(err, action.ErrInvalidAction) {
		t.Fatalf("UNDO_REFUSED without a reason = %v", err)
	}
	mustExec(t, db, `UPDATE agent_actions SET intent = '{"target":"assignment:a1"}' WHERE id = 'i1'`)
	if _, err := repo.Get(ctx, "i1"); err == nil {
		t.Fatal("an intent with a target but no version was restored")
	}
}

// The intent of create_assignment names the ID it will make with no version before (ADR-0029): it round-trips, and
// Recover reads it back to settle a crash.
func TestAgentActionIntentOfAnAddedAssignment(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	if err := NewAgentThreadRepository(db).Save(ctx, subjectThread(t)); err != nil {
		t.Fatal(err)
	}
	repo := NewAgentActionRepository(db)
	in := action.Intent{Target: agentRef(t, values.KindAssignment, "ag-1")}
	a, err := action.New(action.Spec{ID: "c1", Thread: "th1", Turn: 1, CallID: "c1", Tool: values.ToolCreateAssignment, Intent: in}, agentAt(1))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, "c1")
	if err != nil || got.Intent() != in || got.Status() != action.StatusPending {
		t.Fatalf("got %+v, %v", got, err)
	}
}

// Entries of the same instant are listed in insert order, not by their random ids: «Отменить всё» goes exactly in
// reverse.
func TestAgentActionsSameInstantKeepInsertOrder(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	if err := NewAgentThreadRepository(db).Save(ctx, subjectThread(t)); err != nil {
		t.Fatal(err)
	}
	repo := NewAgentActionRepository(db)
	ids := []shared.ActionID{"zz", "mm", "aa"} // inserted in this order, ids sorted the other way
	for i, id := range ids {
		a, err := action.New(action.Spec{ID: id, Thread: "th1", Turn: 1, CallID: fmt.Sprintf("c%d", i), Tool: values.ToolCreateSubject}, agentAt(1))
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	want := []shared.ActionID{"aa", "mm", "zz"}
	lists := map[string]func() ([]*action.Action, error){
		"ListByThread": func() ([]*action.Action, error) { return repo.ListByThread(ctx, "th1", 10) },
		"ListRecent":   func() ([]*action.Action, error) { return repo.ListRecent(ctx, 10) },
	}
	for name, list := range lists {
		got, err := list()
		if err != nil || len(got) != len(want) {
			t.Fatalf("%s = %v, %v", name, got, err)
		}
		for i, a := range got {
			if a.ID() != want[i] {
				t.Fatalf("%s[%d] = %s, want %s (newest inserted first)", name, i, a.ID(), want[i])
			}
		}
	}
	unfinished, err := repo.ListUnfinished(ctx)
	if err != nil || len(unfinished) != 3 || unfinished[0].ID() != "zz" || unfinished[2].ID() != "aa" {
		t.Fatalf("ListUnfinished = %v, %v", unfinished, err)
	}
}

// A row a newer build or a broken tool wrote is refused on read, not half restored.
func TestAgentRowsAreCheckedOnRead(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	if err := NewAgentThreadRepository(db).Save(ctx, subjectThread(t)); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `UPDATE agent_turns SET open_calls = '[{"id":"w1","plan_step":7}]' WHERE thread_id = 'th1'`)
	if _, err := NewAgentThreadRepository(db).Get(ctx, "th1"); err == nil {
		t.Fatal("a request with a call of an unknown plan step was restored")
	}
	mustExec(t, db, `UPDATE agent_turns SET open_calls = '[{"id":"w1","plan_step":0,"extra":1}]' WHERE thread_id = 'th1'`)
	if _, err := NewAgentThreadRepository(db).Get(ctx, "th1"); err == nil {
		t.Fatal("an unknown field was accepted")
	}

	mustExec(t, db, `INSERT INTO agent_actions (id, thread_id, turn, call_id, tool, status, compensation, target_version, version, created_at, updated_at)
		VALUES ('x', 'th1', 1, 'c', 'create_subject', 'DONE', '{"kind":"cancel_job","job":"j"}', 1, 1, 0, 0)`)
	if _, err := NewAgentActionRepository(db).Get(ctx, "x"); !errors.Is(err, action.ErrInvalidCompensation) {
		t.Fatalf("a compensation of another tool = %v", err)
	}
}
