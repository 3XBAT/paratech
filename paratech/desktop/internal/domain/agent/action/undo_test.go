package action

import (
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/values"
)

func pendingWith(t *testing.T, tool values.Tool, in Intent) *Action {
	t.Helper()
	s := spec(tool)
	s.Intent = in
	a, err := New(s, t0)
	if err != nil {
		t.Fatalf("New with intent: %v", err)
	}
	return a
}

func TestIntentValidation(t *testing.T) {
	a1, m1 := ref(t, values.KindAssignment, "a1"), ref(t, values.KindMaterial, "m1")
	tests := []struct {
		name string
		tool values.Tool
		in   Intent
		ok   bool
	}{
		{"none", values.ToolEditDue, Intent{}, true},
		{"edit due", values.ToolEditDue, Intent{Target: a1, Before: 3, Prior: RestoreDue{Assignment: a1}}, true},
		{"create subject by name", values.ToolCreateSubject, Intent{Name: "Химия"}, true},
		{"target without version", values.ToolEditDue, Intent{Target: a1}, false},
		{"version without target", values.ToolEditDue, Intent{Before: 2}, false},
		{"prior of another tool", values.ToolEditDue, Intent{Target: m1, Before: 1, Prior: ReturnToInbox{Material: m1}}, false},
		{"broken prior", values.ToolPlaceUpload, Intent{Target: m1, Before: 1, Prior: ReturnToInbox{}}, false},
		{"edit due with next", values.ToolEditDue, Intent{Target: a1, Before: 3, Prior: RestoreDue{Assignment: a1}, Next: RestoreDue{Assignment: a1}}, true},
		{"next of another tool", values.ToolEditDue, Intent{Target: a1, Before: 3, Next: ReturnToInbox{Material: m1}}, false},
		{"long name", values.ToolCreateSubject, Intent{Name: string(make([]byte, maxIntentName+1))}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := spec(tt.tool)
			s.Intent = tt.in
			a, err := New(s, t0)
			if tt.ok != (err == nil) {
				t.Fatalf("New = %v, want ok=%v", err, tt.ok)
			}
			if err == nil && a.Intent() != tt.in {
				t.Fatal("intent not kept")
			}
		})
	}
	if _, err := NewProposed(Spec{ID: "act1", Thread: "th1", Turn: 1, CallID: "c1", Tool: values.ToolProposeSend,
		Intent: Intent{Name: "x"}}, ref(t, values.KindProposal, "p1"), t0); !errors.Is(err, ErrInvalidAction) {
		t.Fatalf("proposal with an intent = %v", err)
	}
}

// Recover after a crash between the two transactions of an entry (ADR-0021).
func TestSettle(t *testing.T) {
	a1 := ref(t, values.KindAssignment, "a1")
	prior := RestoreDue{Assignment: a1, OldDue: time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC)}
	edit := Intent{Target: a1, Before: 3, Prior: prior}
	create := Intent{Name: "Химия"}
	s9 := RemoveSubject{Subject: ref(t, values.KindSubject, "s9")}
	tests := []struct {
		name string
		tool values.Tool
		in   Intent
		obs  Observation
		want Status
		ver  int64
	}{
		{"use case did not run", values.ToolEditDue, edit, Observation{Known: true, Version: 3}, StatusFailed, 0},
		{"use case ran", values.ToolEditDue, edit, Observation{Known: true, Version: 4, Effect: true, Compensation: prior}, StatusDone, 4},
		{"target changed otherwise", values.ToolEditDue, edit, Observation{Known: true, Version: 7}, StatusUnknown, 0},
		{"target gone", values.ToolEditDue, edit, Observation{Known: true}, StatusUnknown, 0},
		{"create found", values.ToolCreateSubject, create, Observation{Known: true, Version: 1, Effect: true, Compensation: s9}, StatusDone, 1},
		{"create made nothing", values.ToolCreateSubject, create, Observation{Known: true}, StatusFailed, 0},
		{"job cannot be told", values.ToolMakeSummary, Intent{}, Observation{}, StatusUnknown, 0},
		{"no intent", values.ToolEditDue, Intent{}, Observation{Known: true, Version: 3}, StatusUnknown, 0},
		{"effect with a wrong compensation", values.ToolEditDue, edit, Observation{Known: true, Version: 4, Effect: true, Compensation: s9}, StatusUnknown, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := pendingWith(t, tt.tool, tt.in)
			if err := a.Settle(tt.obs, at(5)); err != nil {
				t.Fatalf("Settle: %v", err)
			}
			if a.Status() != tt.want || a.TargetVersion() != tt.ver {
				t.Fatalf("status %s version %d, want %s %d", a.Status(), a.TargetVersion(), tt.want, tt.ver)
			}
		})
	}
	if err := reach(t, StatusDone).Settle(Observation{Known: true}, at(5)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("Settle of a DONE entry = %v", err)
	}
}

func TestPlanUndo(t *testing.T) {
	tests := []struct {
		name   string
		target Target
		apply  bool
		err    error
		reason UndoRefusal
	}{
		{"unchanged", Target{Version: 1}, true, nil, ""},
		{"changed since", Target{Version: 2}, false, ErrTargetChanged, UndoChanged},
		{"already compensated", Target{Version: 2, Compensated: true}, false, nil, ""},
		{"result ready", Target{Version: 1, Final: true}, false, ErrResultReady, UndoResultReady},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := reach(t, StatusUndoing)
			apply, err := a.PlanUndo(tt.target)
			if apply != tt.apply || !errors.Is(err, tt.err) {
				t.Fatalf("PlanUndo = %v, %v", apply, err)
			}
			if err == nil {
				return
			}
			why, ok := UndoRefusalOf(err)
			if !ok || why != tt.reason {
				t.Fatalf("UndoRefusalOf = %s, %v", why, ok)
			}
			if err := a.RefuseUndo(why, at(4)); err != nil || a.Refusal() != why {
				t.Fatalf("RefuseUndo = %v, refusal %s", err, a.Refusal())
			}
			if ok, no := a.Undoable(at(4), Target{}); ok || no != NoUndo(why) {
				t.Fatalf("Undoable = %v %s", ok, no)
			}
		})
	}
	if _, err := reach(t, StatusDone).PlanUndo(Target{Version: 1}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("PlanUndo before UNDOING = %v", err)
	}
	if err := reach(t, StatusUndoing).RefuseUndo("tired", at(4)); !errors.Is(err, ErrInvalidAction) {
		t.Fatalf("RefuseUndo with an unknown reason = %v", err)
	}
	if _, ok := UndoRefusalOf(errors.New("disk")); ok {
		t.Fatal("a plain error is a refusal")
	}
	if why, ok := UndoRefusalOf(ErrCannotStop); !ok || why != UndoCannotStop {
		t.Fatalf("cannot stop = %s", why)
	}
}

func TestUndoable(t *testing.T) {
	want := map[Status]NoUndo{
		StatusPending: NoUndoRunning, StatusDone: NoUndoNone, StatusFailed: NoUndoFailed, StatusUnknown: NoUndoUnknown,
		StatusUndoing: NoUndoRunning, StatusUndone: NoUndoUndone, StatusUndoRefused: NoUndoChanged,
		StatusProposed: NoUndoIrreversible,
	}
	for _, s := range Statuses() {
		ok, why := reach(t, s).Undoable(at(2), Target{}) // UNDOING since at(2): still running
		if ok != (s == StatusDone) || why != want[s] {
			t.Errorf("%s: Undoable = %v %s, want %s", s, ok, why, want[s])
		}
	}
}

// A DONE paid job whose result is ready offers no «Отменить» and CheckUndo refuses it; only a job reads its target.
func TestUndoableOfAFinishedJob(t *testing.T) {
	job := func(t *testing.T, tool values.Tool, c Compensation) *Action {
		t.Helper()
		a, err := New(spec(tool), t0)
		if err != nil {
			t.Fatal(err)
		}
		if err := a.Complete(c, 1, at(1)); err != nil {
			t.Fatal(err)
		}
		return a
	}
	j1, w1 := ref(t, values.KindJob, "j1"), ref(t, values.KindWork, "w1")
	tests := []struct {
		name   string
		a      func(t *testing.T) *Action
		target Target
		needs  bool
		ok     bool
		why    NoUndo
		err    error
	}{
		{"job running", func(t *testing.T) *Action { return job(t, values.ToolGenerateDraft, CancelJob{Job: j1}) }, Target{Version: 1}, true, true, NoUndoNone, nil},
		{"job finished", func(t *testing.T) *Action { return job(t, values.ToolGenerateDraft, CancelJob{Job: j1}) }, Target{Version: 1, Final: true}, true, false, NoUndoResultReady, ErrResultReady},
		{"job not read", func(t *testing.T) *Action { return job(t, values.ToolMakeSummary, CancelJob{Job: j1}) }, Target{}, true, true, NoUndoNone, nil},
		{"finished work stays undoable", func(t *testing.T) *Action { return job(t, values.ToolWriteWork, StopWork{Work: w1, Job: j1}) }, Target{Version: 1, Final: true}, false, true, NoUndoNone, nil},
		{"not a job", func(t *testing.T) *Action { return reach(t, StatusDone) }, Target{Version: 1, Final: true}, false, true, NoUndoNone, nil},
		{"undone entry", func(t *testing.T) *Action { return reach(t, StatusUndone) }, Target{Final: true}, false, false, NoUndoUndone, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := tt.a(t)
			if a.UndoNeedsTarget() != tt.needs {
				t.Fatalf("UndoNeedsTarget = %v", a.UndoNeedsTarget())
			}
			if ok, why := a.Undoable(at(2), tt.target); ok != tt.ok || why != tt.why {
				t.Fatalf("Undoable = %v %s, want %v %s", ok, why, tt.ok, tt.why)
			}
			if err := a.CheckUndo(tt.target); !errors.Is(err, tt.err) || (tt.err == nil) != (err == nil) {
				t.Fatalf("CheckUndo = %v, want %v", err, tt.err)
			}
		})
	}

	// A refused job entry is not rewritten: it keeps its own reason.
	a := job(t, values.ToolMakeSummary, CancelJob{Job: j1})
	if err := errors.Join(a.BeginUndo(at(2)), a.RefuseUndo(UndoCannotStop, at(3))); err != nil {
		t.Fatal(err)
	}
	if a.UndoNeedsTarget() {
		t.Fatal("an UNDO_REFUSED entry reads its target")
	}
	if ok, why := a.Undoable(at(4), Target{Final: true}); ok || why != NoUndoCannotStop {
		t.Fatalf("refused entry = %v %s", ok, why)
	}
}

// An Undo whose compensation failed stays UNDOING; once stale it is offered again and StartUndo retries it.
func TestStaleUndoIsRetried(t *testing.T) {
	stale := at(2).Add(StaleUndo)
	tests := []struct {
		name    string
		now     time.Time
		offered bool
	}{
		{"running", at(2).Add(StaleUndo - time.Second), false},
		{"stale", stale, true},
		{"no time", time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := reach(t, StatusUndoing)
			ok, why := a.Undoable(tt.now, Target{})
			if ok != tt.offered || a.CanUndo(tt.now) != tt.offered {
				t.Fatalf("Undoable = %v %s, CanUndo = %v; want %v", ok, why, a.CanUndo(tt.now), tt.offered)
			}
			err := a.StartUndo(tt.now)
			if tt.offered != (err == nil) {
				t.Fatalf("StartUndo = %v, want ok=%v", err, tt.offered)
			}
			if !tt.offered && !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("StartUndo while running = %v", err)
			}
		})
	}

	a := reach(t, StatusUndoing)
	if err := a.StartUndo(stale); err != nil || a.Status() != StatusUndoing || a.State().UpdatedAt != stale {
		t.Fatalf("retry = %v, %s at %v", err, a.Status(), a.State().UpdatedAt)
	}
	if ok, why := a.Undoable(stale.Add(time.Second), Target{}); ok || why != NoUndoRunning {
		t.Fatalf("a renewed retry = %v %s, want running", ok, why)
	}
}

// «Отменить всё»: an earlier action follows its target through the Undo of a later action of the request that
// started from the version it left; a change by someone else in between breaks the chain.
func TestRebase(t *testing.T) {
	s1, f1 := ref(t, values.KindSubject, "s1"), ref(t, values.KindFolder, "f1")
	m1 := ref(t, values.KindMaterial, "m1")
	done := func(t *testing.T, s Spec, created time.Time, c Compensation, v int64) *Action {
		t.Helper()
		a, err := New(s, created)
		if err != nil {
			t.Fatal(err)
		}
		if err := a.Complete(c, v, created); err != nil {
			t.Fatal(err)
		}
		return a
	}
	undone := func(t *testing.T, a *Action) *Action {
		t.Helper()
		if err := a.BeginUndo(at(10)); err != nil {
			t.Fatal(err)
		}
		if err := a.FinishUndo(at(10)); err != nil {
			t.Fatal(err)
		}
		return a
	}
	subjectSpec := Spec{ID: "a1", Thread: "th1", Turn: 1, CallID: "c1", Tool: values.ToolCreateSubject, Intent: Intent{Name: "Химия"}}
	folderSpec := func(before int64) Spec {
		return Spec{ID: "a2", Thread: "th1", Turn: 1, CallID: "c2", Tool: values.ToolCreateFolder,
			Intent: Intent{Target: s1, Before: before, Name: "Лабы"}}
	}
	folder := RemoveFolder{Subject: s1, Folder: f1}
	tests := []struct {
		name   string
		later  func(t *testing.T) *Action
		after  int64
		rebase bool
	}{
		{"chain", func(t *testing.T) *Action { return undone(t, done(t, folderSpec(1), at(1), folder, 2)) }, 3, true},
		{"changed in between", func(t *testing.T) *Action { return undone(t, done(t, folderSpec(2), at(1), folder, 3)) }, 4, false},
		{"later not undone", func(t *testing.T) *Action { return done(t, folderSpec(1), at(1), folder, 2) }, 3, false},
		{"another request", func(t *testing.T) *Action {
			s := folderSpec(1)
			s.Turn = 2
			return undone(t, done(t, s, at(1), folder, 2))
		}, 3, false},
		{"another target", func(t *testing.T) *Action {
			s := Spec{ID: "a2", Thread: "th1", Turn: 1, CallID: "c2", Tool: values.ToolLinkMaterial,
				Intent: Intent{Target: m1, Before: 1}}
			return undone(t, done(t, s, at(1), UnlinkMaterial{Material: m1, Lesson: ref(t, values.KindLesson, "l1")}, 2))
		}, 3, false},
		{"earlier than this one", func(t *testing.T) *Action {
			return undone(t, done(t, folderSpec(1), t0.Add(-time.Minute), folder, 2))
		}, 3, false},
		{"no version after", func(t *testing.T) *Action { return undone(t, done(t, folderSpec(1), at(1), folder, 2)) }, 0, false},
		{"none", func(*testing.T) *Action { return nil }, 3, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := done(t, subjectSpec, t0, RemoveSubject{Subject: s1}, 1)
			if got := a.Rebase(tt.later(t), tt.after); got != tt.rebase {
				t.Fatalf("Rebase = %v, want %v", got, tt.rebase)
			}
			want := int64(1)
			if tt.rebase {
				want = tt.after
			}
			if a.TargetVersion() != want {
				t.Fatalf("target version %d, want %d", a.TargetVersion(), want)
			}
		})
	}

	undoing := done(t, subjectSpec, t0, RemoveSubject{Subject: s1}, 1)
	if err := undoing.BeginUndo(at(2)); err != nil {
		t.Fatal(err)
	}
	if undoing.Rebase(undone(t, done(t, folderSpec(1), at(1), folder, 2)), 3) {
		t.Fatal("an entry that is not DONE was rebased")
	}
	job := done(t, Spec{ID: "a3", Thread: "th1", Turn: 1, CallID: "c3", Tool: values.ToolMakeSummary}, t0,
		CancelJob{Job: ref(t, values.KindJob, "j1")}, 1)
	if !job.TargetRef().IsZero() {
		t.Fatal("a job has a target version that moves")
	}
}
