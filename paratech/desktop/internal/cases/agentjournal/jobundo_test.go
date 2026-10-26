package agentjournal

import (
	"context"
	"errors"
	"testing"

	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
)

// jobDone records a DONE generate_draft whose Undo cancels job j1.
func jobDone(t *testing.T, s *Service, turn int, call string) Entry {
	t.Helper()
	e := beginIn(t, s, BeginInput{Thread: "th1", Turn: turn, CallID: call, Tool: values.ToolGenerateDraft})
	e, err := s.Finish(t.Context(), e.ID, Outcome{Compensation: action.CancelJob{Job: ref(t, values.KindJob, "j1")}, TargetVersion: 1})
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	return e
}

func jobTargets(tg action.Target) map[action.CompensationKind]action.Target {
	return map[action.CompensationKind]action.Target{action.CompCancelJob: tg, action.CompRemoveSubject: {Version: 1}}
}

// «Журнал» and the panel read the job of a DONE paid action: a finished one offers no «Отменить» (result_ready), a
// running one does; a job that cannot be read is offered and left to Undo.
func TestListJobResultReady(t *testing.T) {
	tests := []struct {
		name string
		comp *fakeComp
		can  bool
		why  action.NoUndo
	}{
		{"finished", &fakeComp{targets: jobTargets(action.Target{Version: 1, Final: true})}, false, action.NoUndoResultReady},
		{"running", &fakeComp{targets: jobTargets(action.Target{Version: 1})}, true, action.NoUndoNone},
		{"gone", &fakeComp{gone: true}, true, action.NoUndoNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newService(t, tt.comp)
			e := jobDone(t, s, 1, "c1")

			lines, err := s.List(t.Context(), 0)
			if err != nil || len(lines) != 1 || lines[0].Action != e.ID || lines[0].Status != action.StatusDone ||
				lines[0].CanUndo != tt.can || lines[0].NoUndo != tt.why {
				t.Fatalf("List = %+v, %v", lines, err)
			}
			got, err := s.ListThread(t.Context(), "th1", 0)
			if err != nil || len(got) != 1 || got[0].CanUndo != tt.can || got[0].NoUndo != tt.why {
				t.Fatalf("ListThread = %+v, %v", got, err)
			}
		})
	}
}

// «Отменить» of a paid action whose result is ready: refused before anything is saved, the entry stays DONE.
func TestUndoJobResultReady(t *testing.T) {
	comp := &fakeComp{targets: jobTargets(action.Target{Version: 1, Final: true})}
	s, repo := newService(t, comp)
	e := jobDone(t, s, 1, "c1")
	before, _ := repo.Get(t.Context(), e.ID)

	_, err := s.Undo(t.Context(), e.ID)
	if why, ok := action.UndoRefusalOf(err); !errors.Is(err, action.ErrResultReady) || !ok || why != action.UndoResultReady {
		t.Fatalf("Undo = %v", err)
	}
	after, _ := repo.Get(t.Context(), e.ID)
	if after.Status() != action.StatusDone || after.State().Version != before.State().Version || len(comp.applied) != 0 {
		t.Fatalf("entry %s v%d (was v%d), applied %d", after.Status(), after.State().Version, before.State().Version, len(comp.applied))
	}
}

// A running job is cancelled; one that finishes between the check and the compensation is still UNDO_REFUSED with
// its reason.
func TestUndoJob(t *testing.T) {
	tests := []struct {
		name    string
		comp    func() *fakeComp
		want    action.Status
		reason  action.NoUndo
		applied int
	}{
		{"running", func() *fakeComp { return &fakeComp{targets: jobTargets(action.Target{Version: 1})} }, action.StatusUndone, action.NoUndoUndone, 1},
		{"finished after the check", func() *fakeComp {
			c := &fakeComp{targets: jobTargets(action.Target{Version: 1})}
			c.afterCheck = func() { c.targets[action.CompCancelJob] = action.Target{Version: 1, Final: true} }
			return c
		}, action.StatusUndoRefused, action.NoUndoResultReady, 0},
		{"finished before the cancel", func() *fakeComp {
			return &fakeComp{targets: jobTargets(action.Target{Version: 1}), applyErr: action.ErrResultReady}
		}, action.StatusUndoRefused, action.NoUndoResultReady, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp := tt.comp()
			s, _ := newService(t, comp)
			e := jobDone(t, s, 1, "c1")
			got, err := s.Undo(t.Context(), e.ID)
			if err != nil || got.Status != tt.want || got.NoUndo != tt.reason || len(comp.applied) != tt.applied {
				t.Fatalf("Undo = %+v, %v, applied %d", got, err, len(comp.applied))
			}
		})
	}
}

// «Отменить всё» skips a paid action whose result is ready (it stays DONE, no error) and undoes the rest.
func TestUndoRequestSkipsReadyResult(t *testing.T) {
	comp := &fakeComp{targets: jobTargets(action.Target{Version: 1, Final: true})}
	s, repo := newService(t, comp)
	sub := done(t, s, begin(t, s, "c1").ID)
	job := jobDone(t, s, 1, "c2")

	got, err := s.UndoRequest(t.Context(), "th1", 1)
	if err != nil || len(got) != 1 || got[0].ID != sub.ID || got[0].Status != action.StatusUndone {
		t.Fatalf("UndoRequest = %+v, %v", got, err)
	}
	if a, _ := repo.Get(t.Context(), job.ID); a.Status() != action.StatusDone {
		t.Fatalf("ready job entry = %s, want DONE", a.Status())
	}
}

// erroringComp cannot read or apply anything.
type erroringComp struct{ err error }

func (f *erroringComp) Check(context.Context, action.Compensation) (action.Target, error) {
	return action.Target{}, f.err
}

func (f *erroringComp) Apply(context.Context, action.Compensation, int64) (int64, error) {
	return 0, f.err
}

// A check that fails before «Отменить» saves nothing and is returned.
func TestUndoJobCheckFails(t *testing.T) {
	comp := &erroringComp{err: errors.New("disk")}
	s, repo := newService(t, comp)
	e := jobDone(t, s, 1, "c1")
	if _, err := s.Undo(t.Context(), e.ID); !errors.Is(err, comp.err) {
		t.Fatalf("Undo = %v", err)
	}
	if a, _ := repo.Get(t.Context(), e.ID); a.Status() != action.StatusDone {
		t.Fatalf("entry = %s, want DONE", a.Status())
	}
	if lines, err := s.List(t.Context(), 0); err != nil || !lines[0].CanUndo {
		t.Fatalf("List with an unreadable job = %+v, %v", lines, err)
	}
}
