package agent

import (
	"context"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
)

const markA1A2 = `{"assignments":["a1","a2","zz"],"done":true}`

// mark_assignments_done is ONE MarkDone call, one entry and one RestoreDone of the changed ones, built from the use
// case's result; each assignment gets its outcome by ID. A repeat changes nothing: no entry, no «Отменить».
func TestMarkAssignmentsDone(t *testing.T) {
	w, ch := newWave10World(t)
	r, e := run10(t, w, req, call10(t, "c1", values.ToolMarkAssignmentsDone, markA1A2, actionpolicy.ClassU))
	want := `{"changed":true,"done":true,"marks":[{"id":"a1","changed":true},{"id":"a2","changed":true},` +
		`{"id":"zz","changed":false,"refused":"not_found"}]}`
	if r.out.Result.ResultJSON != want {
		t.Fatalf("result = %s", r.out.Result.ResultJSON)
	}
	if ch.marks != 1 || !w.assignments.views["a1"].Done || !w.assignments.views["a2"].Done {
		t.Fatalf("MarkDone calls %d, views %+v", ch.marks, w.assignments.views)
	}
	a1, a2 := mustRef(t, values.KindAssignment, "a1"), mustRef(t, values.KindAssignment, "a2")
	comp, ok := e.Compensation.(action.RestoreDone)
	if e.Status != action.StatusDone || !ok || comp.Kind() != action.CompRestoreDone || len(w.store.order) != 1 {
		t.Fatalf("entry = %+v, entries %d", e, len(w.store.order))
	}
	wantMarks := []action.DoneMark{{Assignment: a1, WasDone: false, Version: 4}, {Assignment: a2, WasDone: false, Version: 2}}
	if got := comp.Marks(); len(got) != 2 || got[0] != wantMarks[0] || got[1] != wantMarks[1] {
		t.Fatalf("marks = %+v, want %+v", got, wantMarks)
	}
	if len(r.cards) != 1 {
		t.Fatalf("cards = %+v, want the action line", r.cards)
	}
	if ch.calendar != 0 {
		t.Fatal("Google Calendar was touched")
	}

	again, e2 := run10(t, w, req, call10(t, "c2", values.ToolMarkAssignmentsDone, markA1A2, actionpolicy.ClassU))
	if again.out.Result.ResultJSON != `{"changed":false,"done":true,"marks":[{"id":"a1","changed":false},{"id":"a2","changed":false},`+
		`{"id":"zz","changed":false,"refused":"not_found"}]}` || e2.ID != "" || ch.marks != 1 || len(again.cards) != 0 {
		t.Fatalf("repeat = %s, entry %+v, MarkDone calls %d", again.out.Result.ResultJSON, e2, ch.marks)
	}

	// The same call again answers from its entry.
	replay, _ := act(t, w, call10(t, "c1", values.ToolMarkAssignmentsDone, markA1A2, actionpolicy.ClassU))
	if replay.out.Result.IsError || !strings.Contains(replay.out.Result.ResultJSON, `"marks":[{"id":"a1","changed":true}`) || ch.marks != 1 {
		t.Fatalf("replay = %s", replay.out.Result.ResultJSON)
	}

	if u := undo(t, w, e); u.Status != action.StatusUndone || w.assignments.views["a1"].Done || w.assignments.views["a2"].Done {
		t.Fatalf("undo = %+v, views %+v", u, w.assignments.views)
	}
	if ch.restores != 1 {
		t.Fatalf("RestoreDone calls = %d", ch.restores)
	}
}

// Undo is all or nothing: a change of any mark since, also of the one with the LOWER version, refuses it as
// «Изменено после» and nothing is put back.
func TestMarkAssignmentsDoneUndoOfAChangedMark(t *testing.T) {
	for name, change := range map[string]func(w *world){
		"the lower-versioned mark changed": func(w *world) {
			v := w.assignments.views["a2"] // version 2 < a1's 4
			v.Title, v.Version = "Своё", v.Version+1
			w.assignments.views["a2"] = v
		},
		"the higher-versioned mark changed": func(w *world) {
			v := w.assignments.views["a1"]
			v.Done, v.Version = false, v.Version+1
			w.assignments.views["a1"] = v
		},
		"one was deleted": func(w *world) { w.assignments.deleted["a2"] = true },
	} {
		t.Run(name, func(t *testing.T) {
			w, ch := newWave10World(t)
			_, e := run10(t, w, req, call10(t, "c1", values.ToolMarkAssignmentsDone, markA1A2, actionpolicy.ClassU))
			change(w)
			snapshot := map[string]homeworkflow.AssignmentView{"a1": w.assignments.views["a1"], "a2": w.assignments.views["a2"]}
			u := undo(t, w, e)
			if u.Status != action.StatusUndoRefused || u.NoUndo != action.NoUndoChanged {
				t.Fatalf("undo = %+v, want «Изменено после»", u)
			}
			if w.assignments.views["a1"] != snapshot["a1"] || w.assignments.views["a2"] != snapshot["a2"] || ch.restores != 0 {
				t.Fatalf("a refused undo put something back: %+v, RestoreDone calls %d", w.assignments.views, ch.restores)
			}
		})
	}
}

func TestMarkAssignmentsDoneRecover(t *testing.T) {
	t.Run("marked, DONE not recorded", func(t *testing.T) {
		w, _ := newWave10World(t)
		crash(t, w, call10(t, "c1", values.ToolMarkAssignmentsDone, markA1A2, actionpolicy.ClassU))
		e := recovered(t, w, "c1")
		comp, ok := e.Compensation.(action.RestoreDone)
		if e.Status != action.StatusDone || !ok || len(comp.Marks()) != 2 || comp.ExpectedVersion() != 4 {
			t.Fatalf("recovered = %+v", e)
		}
		if u := undo(t, w, e); u.Status != action.StatusUndone || w.assignments.views["a1"].Done {
			t.Fatalf("undo after recover = %+v", u)
		}
	})
	t.Run("not marked: FAILED", func(t *testing.T) {
		w, _ := newWave10World(t)
		p, _, err := w.actions.prepare(t.Context(), originOf(req), call10(t, "c1", values.ToolMarkAssignmentsDone, markA1A2, actionpolicy.ClassU).Call)
		if err != nil {
			t.Fatal(err)
		}
		begin(t, w, "c1", values.ToolMarkAssignmentsDone, p.intent)
		if e := recovered(t, w, "c1"); e.Status != action.StatusFailed {
			t.Fatalf("recovered = %+v", e)
		}
	})
	t.Run("changed otherwise meanwhile: UNKNOWN", func(t *testing.T) {
		w, _ := newWave10World(t)
		p, _, err := w.actions.prepare(t.Context(), originOf(req), call10(t, "c1", values.ToolMarkAssignmentsDone, markA1A2, actionpolicy.ClassU).Call)
		if err != nil {
			t.Fatal(err)
		}
		begin(t, w, "c1", values.ToolMarkAssignmentsDone, p.intent)
		v := w.assignments.views["a1"]
		v.Version += 2
		w.assignments.views["a1"] = v
		if e := recovered(t, w, "c1"); e.Status != action.StatusUnknown {
			t.Fatalf("recovered = %+v", e)
		}
	})
}

func begin(t *testing.T, w *world, callID string, tool values.Tool, in action.Intent) {
	t.Helper()
	in2 := agentjournal.BeginInput{Thread: req.Thread, Turn: req.Turn, CallID: callID, Tool: tool, Intent: in}
	if _, err := w.journal.Begin(t.Context(), in2); err != nil {
		t.Fatal(err)
	}
}

// --- list_assignments with state --------------------------------------------------------------------------

// stateAssignments is the assignments fake that also filters by state, as homeworkflow.Service.
type stateAssignments struct {
	*fakeAssignments
	asked []homeworkflow.ListState
}

func (s *stateAssignments) ListAssignmentsByState(_ context.Context, st homeworkflow.ListState) ([]homeworkflow.AssignmentView, error) {
	s.asked = append(s.asked, st)
	var out []homeworkflow.AssignmentView
	for _, v := range s.views {
		if st == homeworkflow.ListAll || (st == homeworkflow.ListDone) == v.Done {
			out = append(out, v)
		}
	}
	return out, nil
}

func TestListAssignmentsByState(t *testing.T) {
	r := newReads()
	list := &stateAssignments{fakeAssignments: &fakeAssignments{views: []homeworkflow.AssignmentView{{ID: "a1"}, {ID: "a2", Done: true}}}}
	d := r.deps(&scriptedTurns{}, &chanSink{})
	d.Assignments = list
	drv, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		args string
		want string
	}{
		{`{"state":"done"}`, `{"assignments":[{"id":"a2","status":"no_due","done":true}],"count":1,"truncated":false}`},
		{`{"state":"all"}`, `{"assignments":[{"id":"a1","status":"no_due","done":false},{"id":"a2","status":"no_due","done":true}],"count":2,"truncated":false}`},
		{`{"state":"open"}`, `{"assignments":[{"id":"a1","status":"no_due","done":false}],"count":1,"truncated":false}`},
	}
	for _, tt := range tests {
		c := call10(t, "c1", values.ToolListAssignments, tt.args, actionpolicy.ClassR)
		out := drv.tools.read(t.Context(), values.Scope{}, c)
		if out.Result.ResultJSON != tt.want {
			t.Errorf("%s: result = %s", tt.args, out.Result.ResultJSON)
		}
	}

	// Catalog v9 (no state): the open list as before, byte for byte, no done field.
	out := drv.tools.read(t.Context(), values.Scope{}, readCall(t, "c1", values.ToolListAssignments, `{}`))
	if out.Result.ResultJSON != `{"assignments":[{"id":"a1","status":"no_due"},{"id":"a2","status":"no_due"}],"count":2,"truncated":false}` {
		t.Fatalf("v9 = %s", out.Result.ResultJSON)
	}
	if len(list.asked) != 3 {
		t.Fatalf("asked = %v, the v9 call reads ListAssignments", list.asked)
	}
}

// Without the state filter wired, catalog v10 lists the open ones and refuses done or all.
func TestListAssignmentsByStateWithoutTheFilter(t *testing.T) {
	r := newReads()
	r.assignments.views = []homeworkflow.AssignmentView{{ID: "a1"}}
	tl := r.tools()
	open := tl.read(t.Context(), values.Scope{}, call10(t, "c1", values.ToolListAssignments, `{"state":"open"}`, actionpolicy.ClassR))
	if open.Result.ResultJSON != `{"assignments":[{"id":"a1","status":"no_due","done":false}],"count":1,"truncated":false}` {
		t.Fatalf("open = %s", open.Result.ResultJSON)
	}
	done := tl.read(t.Context(), values.Scope{}, call10(t, "c2", values.ToolListAssignments, `{"state":"done"}`, actionpolicy.ClassR))
	if !done.Result.IsError {
		t.Fatalf("done = %s", done.Result.ResultJSON)
	}
}
