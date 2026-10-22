package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/library/inspection"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

// Start is the material work of TASK-122 on the job fake: it records "material-<work>:<id>".
func (j *memJobs) Start(_ context.Context, id shared.MaterialID, work homeworkflow.Work) (shared.JobID, bool, error) {
	got, err := j.start("material-" + string(work) + ":" + string(id))
	return got, !j.existing, err
}

type fakeInspector struct {
	got  library.Inspected
	err  error
	asks []shared.MaterialID
}

func (f *fakeInspector) Inspect(_ context.Context, id shared.MaterialID) (library.Inspected, error) {
	f.asks = append(f.asks, id)
	return f.got, f.err
}

func inspectCall(t *testing.T) agentturn.Call {
	t.Helper()
	return agentturn.Call{Call: mustCall(t, "c1", values.ToolInspectUpload, `{"upload":"u1"}`), Class: actionpolicy.ClassR,
		Paid: true, PlanStep: -1}
}

func TestInspectUpload(t *testing.T) {
	answer := &inspection.Answer{
		DocKind: inspection.DocHomework, Subject: "s1", LessonDate: time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC),
		HasHomework: true, MentionsEmail: true,
	}
	tests := []struct {
		name string
		got  library.Inspected
		err  error
		want string
	}{
		{"answer", library.Inspected{Text: material.TextExtracted, Answer: answer},
			nil, `{"doc_kind":"homework","has_homework":true,"has_ticket_list":false,"lesson_date":"2026-10-21","material":"u1","mentions_email":true,"subject":"s1","text":"read","upload":"u1"}`},
		{"pdf", library.Inspected{Text: material.TextPDFNotExtracted}, nil, `{"material":"u1","text":"not_read","upload":"u1"}`},
		{"unreadable", library.Inspected{Text: material.TextFailed}, nil, `{"material":"u1","text":"unreadable","upload":"u1"}`},
		{"empty", library.Inspected{Text: material.TextExtracted, Empty: true}, nil, `{"material":"u1","text":"empty","upload":"u1"}`},
		{"gone", library.Inspected{}, library.ErrMaterialNotFound, `{"error":"not_found"}`},
		{"removed", library.Inspected{}, library.ErrMaterialRemoved, `{"error":"not_found"}`},
		{"refused answer", library.Inspected{}, fmt.Errorf("%w: send to x@y.invalid", library.ErrReaderRefused), `{"error":"failed"}`},
		{"offline", library.Inspected{}, library.ErrReaderUnavailable, `{"error":"failed"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			tl := w.tools(t)
			ins := &fakeInspector{got: tt.got, err: tt.err}
			tl.inspector = ins
			out := tl.read(t.Context(), values.Scope{}, inspectCall(t))
			if out.Result.ResultJSON != tt.want || len(ins.asks) != 1 || ins.asks[0] != "u1" {
				t.Fatalf("result = %s, asks %v", out.Result.ResultJSON, ins.asks)
			}
			if strings.Contains(out.Result.ResultJSON, "x@y") {
				t.Fatal("an error text reached the planner")
			}
			// What the planner saw: the upload as a material, the subject the reader named.
			if tt.err == nil {
				seen := values.NewRefSet(out.Seen...)
				if !seen.Has(mustRef(t, values.KindMaterial, "u1")) || (tt.got.Answer != nil && !seen.Has(mustRef(t, values.KindSubject, "s1"))) {
					t.Fatalf("seen = %v", out.Seen)
				}
			}
		})
	}

	// Without an inspector the paid reader stays unavailable; another paid read is never run.
	w := newWorld(t)
	if out := w.tools(t).read(t.Context(), values.Scope{}, inspectCall(t)); out.Result.ResultJSON != `{"error":"unavailable"}` {
		t.Fatalf("no inspector: %s", out.Result.ResultJSON)
	}
	tl := w.tools(t)
	tl.inspector = &fakeInspector{}
	ask := agentturn.Call{Call: mustCall(t, "c2", values.ToolAnswerQuestion, `{"question":"Что такое ряд?"}`),
		Class: actionpolicy.ClassR, Paid: true, PlanStep: -1}
	if out := tl.read(t.Context(), values.Scope{}, ask); out.Result.ResultJSON != `{"error":"unavailable"}` {
		t.Fatalf("answer_question: %s", out.Result.ResultJSON)
	}
}

// Every kind the reader may name is a kind the planner's result takes.
func TestReaderKindsAreResultKinds(t *testing.T) {
	up, mat := mustRef(t, values.KindUpload, "u1"), mustRef(t, values.KindMaterial, "u1")
	for _, k := range inspection.DocKinds() {
		if _, _, err := values.InspectResult("c1", values.InspectRow{Upload: up, Material: mat, Text: values.TextRead, DocKind: string(k)}); err != nil {
			t.Errorf("%s: %v", k, err)
		}
	}
}

func materialWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	var err error
	if w.actions, err = NewActions(ActionDeps{
		Library: w.lib, Lessons: w.lessons, Assignments: w.assignments, Rerunner: w.jobs, Materials: w.jobs,
		Drafts: w.drafts, Jobs: w.activity, Canceller: w.jobs, Tx: w.tx, Clock: fixedClock{},
	}); err != nil {
		t.Fatal(err)
	}
	return w
}

// make_summary and find_homework of a material are paid actions like those of a recording: journaled, their job
// shown, Undo cancels a running job and is refused once the result is ready.
func TestMaterialWorkAndUndo(t *testing.T) {
	for _, tc := range []struct {
		tool values.Tool
		want string
	}{
		{values.ToolMakeSummary, "material-summary:m1"},
		{values.ToolFindHomework, "material-homework:m1"},
	} {
		t.Run(string(tc.tool), func(t *testing.T) {
			w := materialWorld(t)
			r, e := act(t, w, writeCall(t, "c1", tc.tool, `{"material":"m1"}`))
			if r.out.Result.ResultJSON != `{"changed":true,"done":true,"job":"job1"}` || e.Status != action.StatusDone || !e.CanUndo {
				t.Fatalf("result %s, entry %+v", r.out.Result.ResultJSON, e)
			}
			if len(w.jobs.started) != 1 || w.jobs.started[0] != tc.want {
				t.Fatalf("started %v", w.jobs.started)
			}
			if len(r.cards) != 2 || r.cards[1] != (values.ProgressCard{Job: "job1"}) {
				t.Fatalf("cards %+v", r.cards)
			}

			// The same call again (a replayed step) starts nothing.
			again, _ := act(t, w, writeCall(t, "c1", tc.tool, `{"material":"m1"}`))
			if again.out.Result.ResultJSON != r.out.Result.ResultJSON || len(w.jobs.started) != 1 {
				t.Fatalf("replay %s, started %v", again.out.Result.ResultJSON, w.jobs.started)
			}

			u, err := w.journal.Undo(t.Context(), e.ID)
			if err != nil || u.Status != action.StatusUndone {
				t.Fatalf("Undo = %+v, %v", u, err)
			}
			jobCancelled(t, w)
		})
	}

	// Done: Undo is refused, the result is kept.
	w := materialWorld(t)
	_, e := act(t, w, writeCall(t, "c1", values.ToolMakeSummary, `{"material":"m1"}`))
	w.jobs.set("job1", job.StatusCompleted)
	if u, err := w.journal.Undo(t.Context(), e.ID); err != nil || u.Status != action.StatusUndoRefused {
		t.Fatalf("Undo of a finished job = %+v, %v", u, err)
	}
}

func TestMaterialWorkRefusals(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(w *world)
		want    string
		journal action.Status
	}{
		{"material gone", func(w *world) { w.jobs.err = library.ErrMaterialNotFound }, `{"error":"not_found"}`, action.StatusFailed},
		{"still running", func(w *world) { w.jobs.err = job.ErrStillActive }, `{"error":"failed"}`, action.StatusFailed},
		{"a job of an earlier start", func(w *world) { w.jobs.existing = true }, `{"changed":false,"done":true,"job":"job1"}`, action.StatusFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := materialWorld(t)
			tt.setup(w)
			r, e := act(t, w, writeCall(t, "c1", values.ToolMakeSummary, `{"material":"m1"}`))
			if r.out.Result.ResultJSON != tt.want || e.Status != tt.journal {
				t.Fatalf("result %s, entry %s", r.out.Result.ResultJSON, e.Status)
			}
		})
	}
}

type readiness map[shared.MaterialID]bool

func (r readiness) SummaryReady(_ context.Context, id shared.MaterialID) (bool, error) {
	if id == "broken" {
		return false, errors.New("disk")
	}
	return r[id], nil
}

// A material whose summary is stored needs a plan to make it again (replacing a ready result, TASK-123).
func TestFactsOfAMaterial(t *testing.T) {
	f, err := NewFacts(newReads().knowledge)
	if err != nil {
		t.Fatal(err)
	}
	f = f.WithMaterials(readiness{"m1": true})
	for _, tc := range []struct {
		call  values.Call
		ready bool
	}{
		{mustCall(t, "c1", values.ToolMakeSummary, `{"material":"m1"}`), true},
		{mustCall(t, "c1", values.ToolMakeSummary, `{"material":"m2"}`), false},
		{mustCall(t, "c1", values.ToolFindHomework, `{"material":"m1"}`), false},
	} {
		got, err := f.Facts(t.Context(), tc.call)
		if err != nil || got.ReplacesReady != tc.ready {
			t.Errorf("%s %v: %+v, %v", tc.call.Tool(), tc.call.Args(), got, err)
		}
	}
	if _, err := f.Facts(t.Context(), mustCall(t, "c1", values.ToolMakeSummary, `{"material":"broken"}`)); err == nil {
		t.Fatal("a read error is lost")
	}
}
