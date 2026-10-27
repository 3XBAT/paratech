package agent

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/turnkey"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// act runs one action call through the tools and returns what it gave and its journal entry (zero if none).
func act(t *testing.T, w *world, c agentturn.Call) (ran, agentjournal.Entry) {
	t.Helper()
	r := w.tools(t).write(t.Context(), req, c)
	e, err := w.journal.Find(t.Context(), req.Thread, req.Turn, c.Call.ID())
	if err != nil && !errors.Is(err, shared.ErrNotFound) {
		t.Fatal(err)
	}
	return r, e
}

func lectures() string { return string(subject.SystemFolderID("s1", subject.SystemLectures)) }

// Every action of §2.3 runs through the journal (PENDING -> DONE) and its Undo restores what was (ADR-0021, D7).
func TestEveryActionAndItsUndo(t *testing.T) {
	msk := now.Location()
	tests := []struct {
		name   string
		tool   values.Tool
		args   string
		result string // the result for the planner
		done   func(t *testing.T, w *world)
		undone func(t *testing.T, w *world)
	}{
		{"create subject", values.ToolCreateSubject, `{"name":"Химия"}`, `{"changed":true,"done":true,"id":"s-new1"}`,
			func(t *testing.T, w *world) {
				if _, ok := w.lib.subjects["s-new1"]; !ok {
					t.Fatal("no subject")
				}
			},
			func(t *testing.T, w *world) {
				if _, ok := w.lib.subjects["s-new1"]; ok {
					t.Fatal("the created subject is still there")
				}
			}},
		{"create folder", values.ToolCreateFolder, `{"subject":"s1","name":"Лабы"}`, `{"changed":true,"done":true,"id":"f-new1"}`,
			func(t *testing.T, w *world) {
				if _, ok := w.lib.subjects["s1"].Folder("f-new1"); !ok {
					t.Fatal("no folder")
				}
			},
			func(t *testing.T, w *world) {
				if _, ok := w.lib.subjects["s1"].Folder("f-new1"); ok {
					t.Fatal("the created folder is still there")
				}
			}},
		{"place upload with a lesson", values.ToolPlaceUpload, `{"upload":"m1","subject":"s1","lesson":"l1"}`, `{"changed":true,"done":true,"id":"m1"}`,
			func(t *testing.T, w *world) {
				m := w.lib.materials["m1"]
				if m.Subject() != "s1" || m.Folder() != subject.SystemFolderID("s1", subject.SystemMaterials) || !slices.Contains(m.Lessons(), "l1") {
					t.Fatalf("material = %s %s %v", m.Subject(), m.Folder(), m.Lessons())
				}
			},
			func(t *testing.T, w *world) {
				if m := w.lib.materials["m1"]; !m.InInbox() || len(m.Lessons()) != 0 {
					t.Fatalf("material = %s %v, want back in «Входящие» and unlinked", m.Subject(), m.Lessons())
				}
			}},
		{"place upload in a folder", values.ToolPlaceUpload, `{"upload":"m2","subject":"s1","folder":"` + lectures() + `"}`, `{"changed":true,"done":true,"id":"m2"}`,
			func(t *testing.T, w *world) {
				if string(w.lib.materials["m2"].Folder()) != lectures() {
					t.Fatal("not placed")
				}
			},
			func(t *testing.T, w *world) {
				if !w.lib.materials["m2"].InInbox() {
					t.Fatal("not back in «Входящие»")
				}
			}},
		{"link material", values.ToolLinkMaterial, `{"material":"m2","lesson":"l2"}`, `{"changed":true,"done":true}`,
			func(t *testing.T, w *world) {
				if !slices.Contains(w.lib.materials["m2"].Lessons(), "l2") {
					t.Fatal("not linked")
				}
			},
			func(t *testing.T, w *world) {
				if len(w.lib.materials["m2"].Lessons()) != 0 {
					t.Fatal("still linked")
				}
			}},
		{"set lesson info", values.ToolSetLessonInfo, `{"recording":"r1","subject":"s1","kind":"seminar"}`, `{"changed":true,"done":true}`,
			func(t *testing.T, w *world) {
				if s, k := correctionOf(w.lessons.lessons["r1"]); s != "Физика" || k != "seminar" {
					t.Fatalf("correction = %q %q", s, k)
				}
			},
			func(t *testing.T, w *world) {
				if s, k := correctionOf(w.lessons.lessons["r1"]); s != "" || k != "" {
					t.Fatalf("correction = %q %q, want none", s, k)
				}
			}},
		{"set lesson info over the student's correction", values.ToolSetLessonInfo, `{"recording":"r2","subject":"s1","kind":"lecture"}`, `{"changed":true,"done":true}`,
			func(*testing.T, *world) {},
			func(t *testing.T, w *world) {
				if s, k := correctionOf(w.lessons.lessons["r2"]); s != "физ-ра (лаб)" || k != "lab" {
					t.Fatalf("correction = %q %q, want the previous one", s, k)
				}
			}},
		{"edit due", values.ToolEditDue, `{"assignment":"a1","due":"2026-10-30"}`, `{"changed":true,"done":true}`,
			func(t *testing.T, w *world) {
				if got := w.assignments.views["a1"].Due; !got.Equal(time.Date(2026, 10, 30, 18, 30, 0, 0, msk)) {
					t.Fatalf("due = %v, want the 30th at the old time of day", got)
				}
			},
			func(t *testing.T, w *world) {
				if got := w.assignments.views["a1"].Due; !got.Equal(time.Date(2026, 10, 24, 15, 30, 0, 0, time.UTC)) {
					t.Fatalf("due = %v, want the old one exactly", got)
				}
			}},
		{"set a first due", values.ToolEditDue, `{"assignment":"a2","due":"2026-11-02"}`, `{"changed":true,"done":true}`,
			func(t *testing.T, w *world) {
				if got := w.assignments.views["a2"].Due; !got.Equal(time.Date(2026, 11, 2, 23, 59, 0, 0, msk)) {
					t.Fatalf("due = %v", got)
				}
			},
			func(t *testing.T, w *world) {
				if !w.assignments.views["a2"].Due.IsZero() {
					t.Fatal("a due date is left")
				}
			}},
		{"make summary", values.ToolMakeSummary, `{"recording":"r1"}`, `{"changed":true,"done":true,"job":"job1"}`,
			func(t *testing.T, w *world) {
				if w.jobs.started[0] != "summary:r1" {
					t.Fatalf("started %v", w.jobs.started)
				}
			}, jobCancelled},
		{"make a ready summary again", values.ToolMakeSummary, `{"recording":"r2"}`, `{"changed":true,"done":true,"job":"job1"}`,
			func(t *testing.T, w *world) {
				if w.jobs.started[0] != "redo:r2" {
					t.Fatalf("started %v", w.jobs.started)
				}
			}, jobCancelled},
		{"make lecture text", values.ToolMakeLectureText, `{"recording":"r1"}`, `{"changed":true,"done":true,"job":"job1"}`,
			func(t *testing.T, w *world) {
				if w.jobs.started[0] != "lectureText:r1" {
					t.Fatalf("started %v", w.jobs.started)
				}
			}, jobCancelled},
		{"find homework", values.ToolFindHomework, `{"recording":"r1"}`, `{"changed":true,"done":true,"job":"job1"}`,
			func(t *testing.T, w *world) {
				if w.jobs.started[0] != "homework:r1" {
					t.Fatalf("started %v", w.jobs.started)
				}
			}, jobCancelled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			r, e := act(t, w, writeCall(t, "c1", tt.tool, tt.args))
			if r.out.Result.IsError || r.out.Result.ResultJSON != tt.result || !r.write {
				t.Fatalf("result = %+v", r.out.Result)
			}
			if e.Status != action.StatusDone || !e.CanUndo || len(r.cards) == 0 || r.cards[0] != (values.ActionLine{Action: e.ID}) {
				t.Fatalf("entry = %+v, cards %+v", e, r.cards)
			}
			tt.done(t, w)

			u, err := w.journal.Undo(t.Context(), e.ID)
			if err != nil || u.Status != action.StatusUndone {
				t.Fatalf("Undo = %+v, %v", u, err)
			}
			tt.undone(t, w)
		})
	}
}

func jobCancelled(t *testing.T, w *world) {
	t.Helper()
	if st := w.activity.jobs[0].Status; st != job.StatusCancelled {
		t.Fatalf("job = %s, want cancelled", st)
	}
}

// The race «правка после действия»: the student changed the target after the action, so Undo is refused («Изменено
// после — отменить нельзя») and the student's change stays.
func TestUndoRefusedAfterTheStudentsChange(t *testing.T) {
	tests := []struct {
		name   string
		tool   values.Tool
		args   string
		change func(w *world)
		kept   func(t *testing.T, w *world)
	}{
		{"folder: subject changed", values.ToolCreateFolder, `{"subject":"s1","name":"Лабы"}`,
			func(w *world) { bump(w.lib.subjects["s1"]) },
			func(t *testing.T, w *world) {
				if _, ok := w.lib.subjects["s1"].Folder("f-new1"); !ok {
					t.Fatal("the folder was removed")
				}
			}},
		{"subject: a file was filed under it", values.ToolCreateSubject, `{"name":"Химия"}`,
			func(w *world) { _, _ = w.lib.Place(context.Background(), "m1", "s-new1", "") },
			func(t *testing.T, w *world) {
				if _, ok := w.lib.subjects["s-new1"]; !ok {
					t.Fatal("a subject with a file was removed")
				}
			}},
		{"upload: moved by the student", values.ToolPlaceUpload, `{"upload":"m1","subject":"s1","lesson":"l1"}`,
			func(w *world) {
				_, _ = w.lib.Place(context.Background(), "m1", "s1", subject.SystemFolderID("s1", subject.SystemLectures))
			},
			func(t *testing.T, w *world) {
				if string(w.lib.materials["m1"].Folder()) != lectures() {
					t.Fatal("the student's move was undone")
				}
			}},
		{"due: edited by the student", values.ToolEditDue, `{"assignment":"a1","due":"2026-10-30"}`,
			func(w *world) {
				v := w.assignments.views["a1"]
				v.Due, v.Version = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC), v.Version+1
				w.assignments.views["a1"] = v
			},
			func(t *testing.T, w *world) {
				if w.assignments.views["a1"].Due.Day() != 5 {
					t.Fatal("the student's due date was overwritten")
				}
			}},
		{"due: assignment deleted by the student", values.ToolEditDue, `{"assignment":"a1","due":"2026-10-30"}`,
			func(w *world) { w.assignments.deleted = map[shared.AssignmentID]bool{"a1": true} },
			func(t *testing.T, w *world) {
				if w.assignments.views["a1"].Due.Day() != 30 {
					t.Fatal("Undo changed a deleted assignment")
				}
			}},
		{"lesson info: corrected by the student", values.ToolSetLessonInfo, `{"recording":"r1","subject":"s1","kind":"seminar"}`,
			func(w *world) { _, _ = w.lessons.SetLessonInfo(context.Background(), "r1", "Физика", "lab") },
			func(t *testing.T, w *world) {
				if _, k := correctionOf(w.lessons.lessons["r1"]); k != "lab" {
					t.Fatal("the student's correction was overwritten")
				}
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			_, e := act(t, w, writeCall(t, "c1", tt.tool, tt.args))
			tt.change(w)
			u, err := w.journal.Undo(t.Context(), e.ID)
			if err != nil || u.Status != action.StatusUndoRefused || u.NoUndo != action.NoUndoChanged {
				t.Fatalf("Undo = %+v, %v", u, err)
			}
			tt.kept(t, w)
		})
	}
}

// Apply compares and swaps in the transaction: a version that moved on after the check is refused, nothing written.
func TestApplyComparesTheVersion(t *testing.T) {
	w := newWorld(t)
	_, e := act(t, w, writeCall(t, "c1", values.ToolEditDue, `{"assignment":"a1","due":"2026-10-30"}`))
	if _, err := w.actions.Apply(t.Context(), e.Compensation, 3); !errors.Is(err, action.ErrTargetChanged) {
		t.Fatalf("Apply on a stale version = %v", err)
	}
	if w.assignments.views["a1"].Due.Day() != 30 {
		t.Fatal("a refused Apply changed the due date")
	}
	if w.tx.n == 0 {
		t.Fatal("Apply ran outside a transaction")
	}
}

// hookTx runs before once ahead of the next transaction: a change by the student between prepare and run.
type hookTx struct {
	Tx
	before func()
}

func (tx *hookTx) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if f := tx.before; f != nil {
		tx.before = nil
		f()
	}
	return tx.Tx.Do(ctx, fn)
}

// create_folder compares and swaps on the subject version it saw: a subject changed between prepare and run gets no
// folder, so «Отменить всё» cannot rebase create_subject over the student's change.
func TestCreateFolderComparesTheSubjectVersion(t *testing.T) {
	w := newWorld(t)
	act(t, w, writeCall(t, "c1", values.ToolCreateSubject, `{"name":"Химия"}`))
	folders := len(w.lib.subjects["s-new1"].Folders())
	w.actions.tx = &hookTx{Tx: w.tx, before: func() { bump(w.lib.subjects["s-new1"]) }}

	r, e := act(t, w, writeCall(t, "c2", values.ToolCreateFolder, `{"subject":"s-new1","name":"Лабы"}`))
	if !r.out.Result.IsError || e.Status != action.StatusFailed || e.CanUndo {
		t.Fatalf("result = %+v, entry %+v", r.out.Result, e)
	}
	if n := len(w.lib.subjects["s-new1"].Folders()); n != folders {
		t.Fatalf("folders = %d: a folder was created on a changed subject", n)
	}

	undone, err := w.journal.UndoRequest(t.Context(), req.Thread, req.Turn)
	if err != nil || len(undone) != 1 || undone[0].Status != action.StatusUndoRefused || undone[0].NoUndo != action.NoUndoChanged {
		t.Fatalf("UndoRequest = %+v, %v", undone, err)
	}
	if _, ok := w.lib.subjects["s-new1"]; !ok {
		t.Fatal("the subject the student changed was removed")
	}
}

// Apply on a target already compensated applies nothing and returns version 0, so no earlier action rebases on it.
func TestApplyOnACompensatedTarget(t *testing.T) {
	tests := []struct {
		name   string
		tool   values.Tool
		args   string
		revert func(t *testing.T, w *world)
	}{
		{"folder removed by the student", values.ToolCreateFolder, `{"subject":"s1","name":"Лабы"}`,
			func(t *testing.T, w *world) {
				if err := w.lib.RemoveFolder(t.Context(), "s1", "f-new1"); err != nil {
					t.Fatal(err)
				}
			}},
		{"due set back by the student", values.ToolEditDue, `{"assignment":"a1","due":"2026-10-30"}`,
			func(_ *testing.T, w *world) {
				v := w.assignments.views["a1"]
				v.Due, v.Version = time.Date(2026, 10, 24, 15, 30, 0, 0, time.UTC), v.Version+1
				w.assignments.views["a1"] = v
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			_, e := act(t, w, writeCall(t, "c1", tt.tool, tt.args))
			tt.revert(t, w)
			after, err := w.actions.Apply(t.Context(), e.Compensation, 1)
			if err != nil || after != 0 {
				t.Fatalf("Apply = %d, %v, want 0 and no error", after, err)
			}
		})
	}
}

// A paid job: running -> cancelled (UNDONE); finished -> its result stays (result_ready); not stoppable -> cannot_stop;
// failed by itself -> nothing left to undo.
func TestUndoOfAPaidJob(t *testing.T) {
	tests := []struct {
		name   string
		before func(w *world)
		want   action.Status
		why    action.NoUndo
		status job.Status
	}{
		{"queued", func(*world) {}, action.StatusUndone, action.NoUndoUndone, job.StatusCancelled},
		{"finished", func(w *world) { w.jobs.set("job1", job.StatusCompleted) }, action.StatusDone, "", job.StatusCompleted},
		{"running, not stoppable", func(w *world) { w.jobs.set("job1", job.StatusRunning); w.jobs.stuck = true },
			action.StatusUndoRefused, action.NoUndoCannotStop, job.StatusRunning},
		{"failed by itself", func(w *world) { w.jobs.set("job1", job.StatusFailed) }, action.StatusUndone, action.NoUndoUndone, job.StatusFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			_, e := act(t, w, writeCall(t, "c1", values.ToolMakeSummary, `{"recording":"r1"}`))
			tt.before(w)
			u, err := w.journal.Undo(t.Context(), e.ID)
			if tt.name == "finished" {
				// The result is ready: Undo is an error and the entry stays DONE.
				got, gerr := w.store.Get(t.Context(), e.ID)
				if !errors.Is(err, action.ErrResultReady) || gerr != nil || got.Status() != tt.want {
					t.Fatalf("Undo = %+v, %v; entry %v, %v", u, err, got, gerr)
				}
			} else if err != nil || u.Status != tt.want || u.NoUndo != tt.why {
				t.Fatalf("Undo = %+v, %v", u, err)
			}
			if st := w.activity.jobs[0].Status; st != tt.status {
				t.Fatalf("job = %s, want %s", st, tt.status)
			}
		})
	}
}

// The same call run again (a replayed step) acts once: the second run answers from the journal entry.
func TestActionIsIdempotentByCallID(t *testing.T) {
	w := newWorld(t)
	c := writeCall(t, "c1", values.ToolCreateSubject, `{"name":"Химия"}`)
	first, e := act(t, w, c)
	second, _ := act(t, w, c)
	if second.out.Result != first.out.Result || second.out.Created != first.out.Created || first.out.Created.ID() != "s-new1" {
		t.Fatalf("second run = %+v, first %+v", second.out, first.out)
	}
	if n := len(w.lib.calls); n != 1 || len(w.store.order) != 1 {
		t.Fatalf("CreateSubject ran %d times, %d entries", n, len(w.store.order))
	}
	if _, err := w.journal.Finish(t.Context(), e.ID, agentjournal.Outcome{Failed: true}); err == nil {
		t.Fatal("a done entry was finished again")
	}

	job1 := writeCall(t, "c2", values.ToolMakeSummary, `{"recording":"r1"}`)
	a, _ := act(t, w, job1)
	b, _ := act(t, w, job1)
	if a.out.Result != b.out.Result || len(w.jobs.started) != 1 {
		t.Fatalf("job runs: %+v / %+v, started %v", a.out.Result, b.out.Result, w.jobs.started)
	}
}

// A state that is already the wanted one changes nothing and journals nothing.
func TestActionsWithNothingToDo(t *testing.T) {
	w := newWorld(t)
	if _, err := w.lib.Link(t.Context(), "m2", "l2"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.lessons.SetLessonInfo(t.Context(), "r1", "Физика", "lab"); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		tool   values.Tool
		args   string
		result string
	}{
		{values.ToolLinkMaterial, `{"material":"m2","lesson":"l2"}`, `{"changed":false,"done":true}`},
		{values.ToolSetLessonInfo, `{"recording":"r1","subject":"s1","kind":"lab"}`, `{"changed":false,"done":true}`},
		{values.ToolEditDue, `{"assignment":"a1","due":"2026-10-24"}`, `{"changed":false,"done":true}`},
	}
	for i, tt := range tests {
		r, e := act(t, w, writeCall(t, "n"+string(rune('0'+i)), tt.tool, tt.args))
		if r.out.Result.ResultJSON != tt.result {
			t.Errorf("%s: result %s", tt.tool, r.out.Result.ResultJSON)
		}
		if e.ID != "" {
			t.Errorf("%s: journaled %+v", tt.tool, e)
		}
	}
}

// Refusals are enums; a refused use case leaves the entry FAILED; a variant without a use case is unavailable.
func TestActionRefusals(t *testing.T) {
	w := newWorld(t)
	if _, err := w.lib.Place(t.Context(), "m2", "s1", ""); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		tool    values.Tool
		args    string
		want    values.Refusal
		journal action.Status // "" none
	}{
		{values.ToolCreateSubject, `{"name":"Физика"}`, values.RefuseNameTaken, action.StatusFailed},
		{values.ToolCreateFolder, `{"subject":"s1","name":"Лекции"}`, values.RefuseNameTaken, action.StatusFailed},
		{values.ToolCreateFolder, `{"subject":"s7","name":"Лабы"}`, values.RefuseNotFound, ""},
		{values.ToolPlaceUpload, `{"upload":"m9","subject":"s1","folder":"` + lectures() + `"}`, values.RefuseNotFound, ""},
		{values.ToolPlaceUpload, `{"upload":"m2","subject":"s1","folder":"` + lectures() + `"}`, values.RefuseInvalidArgs, ""},
		{values.ToolLinkMaterial, `{"material":"m1","lesson":"gone"}`, values.RefuseNotFound, action.StatusFailed},
		{values.ToolSetLessonInfo, `{"recording":"r9","subject":"s1","kind":"lab"}`, values.RefuseNotFound, ""},
		{values.ToolEditDue, `{"assignment":"a9","due":"2026-10-30"}`, values.RefuseNotFound, ""},
		{values.ToolMakeSummary, `{"material":"m1"}`, values.RefuseUnavailable, ""},
		{values.ToolFindHomework, `{"material":"m1"}`, values.RefuseUnavailable, ""},
		{values.ToolTriageAssignment, `{"assignment":"a1"}`, values.RefuseUnavailable, ""},
	}
	for i, tt := range tests {
		id := "x" + string(rune('a'+i))
		r, e := act(t, w, writeCall(t, id, tt.tool, tt.args))
		if !r.out.Result.IsError || r.out.Result.ResultJSON != `{"error":"`+string(tt.want)+`"}` {
			t.Errorf("%s %s: %s, want %s", tt.tool, tt.args, r.out.Result.ResultJSON, tt.want)
		}
		if e.Status != tt.journal {
			t.Errorf("%s %s: journal %q, want %q", tt.tool, tt.args, e.Status, tt.journal)
		}
		if strings.Contains(r.out.Result.ResultJSON, "library") {
			t.Errorf("an error text reached the planner: %s", r.out.Result.ResultJSON)
		}
	}
}

// An assignment the student deleted is unknown to the agent (ADR-0029): edit_due naming it is refused as unknown_id and
// changes nothing.
func TestDeletedAssignmentIsAnUnknownID(t *testing.T) {
	w := newWorld(t)
	w.assignments.deleted = map[shared.AssignmentID]bool{"a1": true}
	r, e := act(t, w, writeCall(t, "c1", values.ToolEditDue, `{"assignment":"a1","due":"2026-10-30"}`))
	if !r.out.Result.IsError || r.out.Result.ResultJSON != `{"error":"`+string(values.RefuseUnknownID)+`"}` {
		t.Fatalf("result = %s, want unknown_id", r.out.Result.ResultJSON)
	}
	if e.Status != "" || w.assignments.views["a1"].Version != 3 {
		t.Fatalf("journal %q, version %d: the call ran", e.Status, w.assignments.views["a1"].Version)
	}
}

// triage_assignment with a topic (TASK-188) runs the topic triage with the call's key and the thread's subject, as a
// free call (the card was off) and as a step of an approved plan: journaled with Undo RemoveWork, the planner sees the
// work and enums only, never the topic.
func TestTriageTopicRuns(t *testing.T) {
	for _, step := range []int{-1, 0} {
		w, f := withTurnkey(t, turnkey.Started{View: questionsView(2), Created: true}, nil)
		c := writeCall(t, "c1", values.ToolTriageAssignment, `{"topic":"Реформы Петра I, презентация"}`)
		c.PlanStep = step
		at := req
		at.Scope = mustScope(t, "s1")
		r := w.tools(t).write(t.Context(), at, c)
		e, err := w.journal.Find(t.Context(), req.Thread, req.Turn, "c1")
		if err != nil || e.Status != action.StatusDone || e.Compensation != (action.RemoveWork{Work: mustRef(t, values.KindWork, "w1")}) {
			t.Fatalf("plan step %d: journal %+v, %v", step, e, err)
		}
		if r.out.Result.IsError || strings.Contains(r.out.Result.ResultJSON, "Петра") {
			t.Fatalf("plan step %d: result %+v", step, r.out.Result)
		}
		want := topicCall{request: "ag:" + string(req.Thread) + ":1:c1", text: "Реформы Петра I, презентация", subject: "s1"}
		if len(f.topics) != 1 || f.topics[0] != want || len(f.calls) != 0 {
			t.Fatalf("plan step %d: topic calls %+v, triage calls %+v", step, f.topics, f.calls)
		}
		u, err := w.journal.Undo(t.Context(), e.ID)
		if err != nil || u.Status != action.StatusUndone {
			t.Fatalf("plan step %d: Undo = %+v, %v", step, u, err)
		}
	}
}

// The request key of a topic work fits the work's limit: a long one is hashed, and keys of other calls differ.
func TestTopicRequestKey(t *testing.T) {
	short := topicRequest(origin{thread: "th1", turn: 3}, "toolu_01")
	if short != "ag:th1:3:toolu_01" {
		t.Fatalf("key = %q", short)
	}
	long := topicRequest(origin{thread: shared.ThreadID(strings.Repeat("t", 100)), turn: 3}, strings.Repeat("c", 64))
	if len(long) != 64 || long == topicRequest(origin{thread: shared.ThreadID(strings.Repeat("t", 100)), turn: 4}, strings.Repeat("c", 64)) {
		t.Fatalf("long key = %q", long)
	}
}

// An action of the retired generate_draft stored before catalog v4 keeps its Undo: the running job is cancelled.
func TestUndoOfAStoredDraftAction(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	jobID, err := w.jobs.start("draft:a1")
	if err != nil {
		t.Fatal(err)
	}
	e, err := w.journal.Begin(ctx, agentjournal.BeginInput{Thread: req.Thread, Turn: req.Turn, CallID: "c1", Tool: values.ToolGenerateDraft})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.journal.Finish(ctx, e.ID, agentjournal.Outcome{
		Compensation: action.CancelJob{Job: mustRef(t, values.KindJob, string(jobID))}, TargetVersion: jobVersion,
	}); err != nil {
		t.Fatal(err)
	}
	u, err := w.journal.Undo(ctx, e.ID)
	if err != nil || u.Status != action.StatusUndone || u.Tool != values.ToolGenerateDraft {
		t.Fatalf("Undo = %+v, %v", u, err)
	}
	jobCancelled(t, w)
}

// Crashes between the steps of an action, settled by Recover at the next start: the use case did not run (FAILED),
// it ran but Finish was not saved (DONE, and Undo works), a paid job (UNKNOWN), Undo interrupted after it applied
// (UNDONE, not refused).
func TestRecoverAfterACrash(t *testing.T) {
	ctx := t.Context()
	begin := func(t *testing.T, w *world, c agentturn.Call) (prepared, agentjournal.Entry) {
		t.Helper()
		p, ok, err := w.actions.prepare(ctx, originOf(req), c.Call)
		if !ok || err != nil {
			t.Fatalf("prepare = %v, %v", ok, err)
		}
		e, err := w.journal.Begin(ctx, agentjournal.BeginInput{Thread: req.Thread, Turn: req.Turn, CallID: c.Call.ID(), Tool: c.Call.Tool(), Intent: p.intent})
		if err != nil {
			t.Fatal(err)
		}
		return p, e
	}
	status := func(t *testing.T, w *world, id shared.ActionID) agentjournal.Entry {
		t.Helper()
		list, err := w.journal.ListThread(ctx, req.Thread, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range list {
			if e.ID == id {
				return e
			}
		}
		t.Fatalf("no entry %s", id)
		return agentjournal.Entry{}
	}

	w := newWorld(t)
	_, notRun := begin(t, w, writeCall(t, "c1", values.ToolEditDue, `{"assignment":"a1","due":"2026-10-30"}`))
	p, ran := begin(t, w, writeCall(t, "c2", values.ToolPlaceUpload, `{"upload":"m1","subject":"s1","lesson":"l1"}`))
	if _, err := p.run(ctx); err != nil {
		t.Fatal(err)
	}
	pc, created := begin(t, w, writeCall(t, "c3", values.ToolCreateSubject, `{"name":"Химия"}`))
	if _, err := pc.run(ctx); err != nil {
		t.Fatal(err)
	}
	pj, paid := begin(t, w, writeCall(t, "c4", values.ToolMakeSummary, `{"recording":"r1"}`))
	if _, err := pj.run(ctx); err != nil {
		t.Fatal(err)
	}

	// The app stops here; at the next start the journal is recovered.
	rep, err := w.journal.Recover(ctx)
	if err != nil || rep != (agentjournal.RecoverReport{Done: 2, Failed: 1, Unknown: 1}) {
		t.Fatalf("Recover = %+v, %v", rep, err)
	}
	if e := status(t, w, notRun.ID); e.Status != action.StatusFailed {
		t.Fatalf("not run = %+v", e)
	}
	if e := status(t, w, paid.ID); e.Status != action.StatusUnknown || e.CanUndo {
		t.Fatalf("paid job = %+v", e)
	}
	for _, id := range []shared.ActionID{ran.ID, created.ID} {
		if u, err := w.journal.Undo(ctx, id); err != nil || u.Status != action.StatusUndone {
			t.Fatalf("Undo of a settled entry = %+v, %v", u, err)
		}
	}
	if !w.lib.materials["m1"].InInbox() || w.lib.subjects["s-new1"] != nil {
		t.Fatal("the settled entries were not undone")
	}

	// Undo interrupted after the compensation applied, before UNDONE was saved.
	w = newWorld(t)
	_, e := act(t, w, writeCall(t, "c1", values.ToolLinkMaterial, `{"material":"m2","lesson":"l2"}`))
	if _, err := w.journal.BeginUndo(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.actions.Apply(ctx, e.Compensation, w.lib.materials["m2"].Version()); err != nil {
		t.Fatal(err)
	}
	rep, err = w.journal.Recover(ctx)
	if err != nil || rep != (agentjournal.RecoverReport{Undone: 1}) {
		t.Fatalf("Recover after an interrupted Undo = %+v, %v", rep, err)
	}
	if e := status(t, w, e.ID); e.Status != action.StatusUndone || len(w.lib.materials["m2"].Lessons()) != 0 {
		t.Fatalf("interrupted Undo = %+v", e)
	}
}

// After a crash only the values the action meant to set count as its effect: another change of the lesson or the due
// date (the student's) leaves the entry UNKNOWN, not DONE with an Undo that would overwrite it.
func TestRecoverNeedsTheIntendedValues(t *testing.T) {
	tests := []struct {
		name string
		tool values.Tool
		args string
		then func(t *testing.T, w *world, p prepared)
		want action.Status
	}{
		{"lesson info set", values.ToolSetLessonInfo, `{"recording":"r1","subject":"s1","kind":"seminar"}`,
			func(t *testing.T, _ *world, p prepared) { mustRun(t, p) }, action.StatusDone},
		{"lesson info set otherwise by the student", values.ToolSetLessonInfo, `{"recording":"r1","subject":"s1","kind":"seminar"}`,
			func(t *testing.T, w *world, _ prepared) {
				if _, err := w.lessons.SetLessonInfo(t.Context(), "r1", "Физика", "lab"); err != nil {
					t.Fatal(err)
				}
			}, action.StatusUnknown},
		{"due set", values.ToolEditDue, `{"assignment":"a1","due":"2026-10-30"}`,
			func(t *testing.T, _ *world, p prepared) { mustRun(t, p) }, action.StatusDone},
		{"due set otherwise by the student", values.ToolEditDue, `{"assignment":"a1","due":"2026-10-30"}`,
			func(_ *testing.T, w *world, _ prepared) {
				v := w.assignments.views["a1"]
				v.Due, v.Version = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC), v.Version+1
				w.assignments.views["a1"] = v
			}, action.StatusUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			c := writeCall(t, "c1", tt.tool, tt.args)
			p, ok, err := w.actions.prepare(t.Context(), originOf(req), c.Call)
			if !ok || err != nil {
				t.Fatalf("prepare = %v, %v", ok, err)
			}
			if _, err := w.journal.Begin(t.Context(), agentjournal.BeginInput{Thread: req.Thread, Turn: req.Turn, CallID: "c1", Tool: tt.tool, Intent: p.intent}); err != nil {
				t.Fatal(err)
			}
			tt.then(t, w, p)
			if _, err := w.journal.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			if e, err := w.journal.Find(t.Context(), req.Thread, req.Turn, "c1"); err != nil || e.Status != tt.want {
				t.Fatalf("settled = %+v, %v; want %s", e, err, tt.want)
			}
		})
	}
}

func mustRun(t *testing.T, p prepared) {
	t.Helper()
	if _, err := p.run(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// A paid job that existed already (an earlier start found by its job key) is not the agent's: the entry is FAILED,
// the result says nothing changed and no «Отменить» line is shown, so Undo can never cancel the student's job.
func TestExistingJobIsNotClaimed(t *testing.T) {
	w := newWorld(t)
	w.jobs.existing = true
	r, e := act(t, w, writeCall(t, "c1", values.ToolMakeSummary, `{"recording":"r1"}`))
	if r.out.Result.IsError || r.out.Result.ResultJSON != `{"changed":false,"done":true,"job":"job1"}` || len(r.cards) != 0 {
		t.Fatalf("result = %+v, cards %+v", r.out.Result, r.cards)
	}
	if e.Status != action.StatusFailed || e.CanUndo {
		t.Fatalf("entry = %+v", e)
	}
}

// A call ID the planner reuses for another tool is refused, not answered with the first tool's result.
func TestReusedCallIDWithAnotherTool(t *testing.T) {
	w := newWorld(t)
	act(t, w, writeCall(t, "c1", values.ToolCreateSubject, `{"name":"Химия"}`))
	r, _ := act(t, w, writeCall(t, "c1", values.ToolCreateFolder, `{"subject":"s1","name":"Лабы"}`))
	if !r.out.Result.IsError || r.out.Result.ResultJSON != `{"error":"`+string(values.RefuseInvalidArgs)+`"}` {
		t.Fatalf("result = %+v", r.out.Result)
	}
	if _, ok := w.lib.subjects["s1"].Folder("f-new1"); ok || len(w.store.order) != 1 {
		t.Fatalf("the second tool ran: %d entries", len(w.store.order))
	}
}

// The Driver's context ends while a step's calls run: the action that ran reports its real outcome (and its action
// line), the call after it is not run and is answered «not done» by Interrupt (TASK-117 follow-up).
func TestDriverInterruptKeepsTheOutcomeOfAnAction(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	w.lib.onCreate = cancel // the app closes while the subject is created
	turns := (&scriptedTurns{}).then(agentturn.Step{Thread: th1, Turn: 1, Status: thread.StatusExecuting, Calls: []agentturn.Call{
		writeCall(t, "c1", values.ToolCreateSubject, `{"name":"Химия"}`),
		readCall(t, "c2", values.ToolListSubjects, `{}`),
	}}, nil)
	sink := &chanSink{}
	d, err := New(w.deps(turns, sink))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	if err := d.Ask(ctx, agentturn.AskInput{Text: "добавь химию"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()

	rep := turns.report()
	if len(rep.interrupted) != 1 || len(rep.submitted) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	outs := rep.interrupted[0].Outcomes
	if len(outs) != 1 || outs[0].Result.IsError || outs[0].Created.ID() != "s-new1" {
		t.Fatalf("outcomes kept = %+v", outs)
	}
	if e, err := w.journal.Find(t.Context(), th1, 1, "c1"); err != nil || e.Status != action.StatusDone {
		t.Fatalf("entry = %+v, %v", e, err)
	}
	lines := 0
	for _, u := range sink.all() {
		for _, c := range u.Cards {
			if c.Kind() == values.CardActionLine {
				lines++
			}
		}
	}
	if lines != 1 {
		t.Fatalf("action lines shown = %d", lines)
	}
}
