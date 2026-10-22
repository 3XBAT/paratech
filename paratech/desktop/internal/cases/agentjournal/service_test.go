package agentjournal

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

type memActions struct {
	mu     sync.Mutex
	states map[shared.ActionID]action.State
	order  []shared.ActionID
}

func (r *memActions) Get(_ context.Context, id shared.ActionID) (*action.Action, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.states[id]
	if !ok {
		return nil, fmt.Errorf("action %s: %w", id, shared.ErrNotFound)
	}
	return action.Restore(st)
}

func (r *memActions) Save(_ context.Context, a *action.Action) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.states == nil {
		r.states = map[shared.ActionID]action.State{}
	}
	st := a.State()
	stored, ok := r.states[st.ID]
	if (st.Version == 0) == ok || (ok && stored.Version != st.Version) {
		return shared.ErrVersionConflict
	}
	if !ok {
		r.order = append(r.order, st.ID)
	}
	st.Version++
	r.states[st.ID] = st
	a.Persisted(st.Version)
	return nil
}

func (r *memActions) ListByThread(ctx context.Context, th shared.ThreadID, limit int) ([]*action.Action, error) {
	r.mu.Lock()
	ids := slices.Clone(r.order)
	r.mu.Unlock()
	slices.Reverse(ids)
	var out []*action.Action
	for _, id := range ids {
		a, err := r.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if a.Thread() == th && len(out) < limit {
			out = append(out, a)
		}
	}
	return out, nil
}

func (r *memActions) ListUnfinished(ctx context.Context) ([]*action.Action, error) {
	var out []*action.Action
	for _, id := range r.order {
		a, err := r.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if a.Status() == action.StatusPending || a.Status() == action.StatusUndoing {
			out = append(out, a)
		}
	}
	return out, nil
}

// fakeComp keeps target versions by the compensation kind; Apply compares and swaps the version like the adapter
// must, and bumps it like a real change would. afterCheck runs after TargetVersion, to change the target in between.
type fakeComp struct {
	versions   map[action.CompensationKind]int64
	gone       bool
	failing    error
	applied    int
	afterCheck func()
}

func (f *fakeComp) TargetVersion(_ context.Context, c action.Compensation) (int64, error) {
	if f.gone {
		return 0, shared.ErrNotFound
	}
	v := f.versions[c.Kind()]
	if f.afterCheck != nil {
		f.afterCheck()
	}
	return v, nil
}

func (f *fakeComp) Apply(_ context.Context, c action.Compensation, expectedVersion int64) error {
	if f.failing != nil {
		return f.failing
	}
	if f.versions[c.Kind()] != expectedVersion {
		return fmt.Errorf("fake compensator: %w", action.ErrTargetChanged)
	}
	f.applied++
	f.versions[c.Kind()]++
	return nil
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { c.now = c.now.Add(time.Second); return c.now }

type ids struct{ n int }

func (g *ids) NewActionID() shared.ActionID { g.n++; return shared.ActionID(fmt.Sprintf("a%d", g.n)) }

func newService(t *testing.T, comp Compensator) (*Service, *memActions) {
	t.Helper()
	repo := &memActions{}
	s, err := New(Deps{Actions: repo, Compensator: comp, Clock: &clock{now: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)}, IDs: &ids{}})
	if err != nil {
		t.Fatal(err)
	}
	return s, repo
}

func subject(t *testing.T) values.Ref {
	t.Helper()
	r, err := values.NewRef(values.KindSubject, "s9")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func begin(t *testing.T, s *Service, call string) Entry {
	t.Helper()
	e, err := s.Begin(t.Context(), BeginInput{Thread: "th1", Turn: 1, CallID: call, Tool: values.ToolCreateSubject})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	return e
}

func done(t *testing.T, s *Service, id shared.ActionID) Entry {
	t.Helper()
	e, err := s.Finish(t.Context(), id, Outcome{Compensation: action.RemoveSubject{Subject: subject(t)}, TargetVersion: 1})
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	return e
}

func TestNewNeedsPorts(t *testing.T) {
	if _, err := New(Deps{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New = %v", err)
	}
}

func TestBeginFinish(t *testing.T) {
	s, _ := newService(t, nil)
	e := begin(t, s, "c1")
	if e.Status != action.StatusPending || e.Key != "ag:th1:act:"+string(e.ID) || e.CanUndo {
		t.Fatalf("begun = %+v", e)
	}
	e = done(t, s, e.ID)
	if e.Status != action.StatusDone || !e.CanUndo {
		t.Fatalf("finished = %+v", e)
	}
	if _, err := s.Finish(t.Context(), e.ID, Outcome{Failed: true}); !errors.Is(err, action.ErrInvalidTransition) {
		t.Fatalf("second finish = %v", err)
	}

	failed := begin(t, s, "c2")
	if e, err := s.Finish(t.Context(), failed.ID, Outcome{Failed: true}); err != nil || e.Status != action.StatusFailed {
		t.Fatalf("failed = %+v, %v", e, err)
	}
	if _, err := s.Finish(t.Context(), "nope", Outcome{Failed: true}); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("finish of an unknown entry = %v", err)
	}
	if _, err := s.Begin(t.Context(), BeginInput{Thread: "th1", Turn: 1, CallID: "c3", Tool: values.ToolListSubjects}); !errors.Is(err, action.ErrInvalidAction) {
		t.Fatalf("journal of a read = %v", err)
	}
	if _, err := s.Finish(t.Context(), begin(t, s, "c4").ID, Outcome{Compensation: action.CancelJob{}, TargetVersion: 1}); !errors.Is(err, action.ErrInvalidCompensation) {
		t.Fatalf("finish with a wrong compensation = %v", err)
	}
}

func TestPropose(t *testing.T) {
	s, _ := newService(t, nil)
	p, _ := values.NewRef(values.KindProposal, "p1")
	e, err := s.Propose(t.Context(), BeginInput{Thread: "th1", Turn: 1, CallID: "c1", Tool: values.ToolProposeSend}, p)
	if err != nil || e.Status != action.StatusProposed || e.Proposal != p {
		t.Fatalf("proposed = %+v, %v", e, err)
	}
}

func TestUndo(t *testing.T) {
	tests := []struct {
		name    string
		comp    *fakeComp
		want    action.Status
		applied int
	}{
		{"target unchanged", &fakeComp{versions: map[action.CompensationKind]int64{action.CompRemoveSubject: 1}}, action.StatusUndone, 1},
		{"target changed", &fakeComp{versions: map[action.CompensationKind]int64{action.CompRemoveSubject: 2}}, action.StatusUndoRefused, 0},
		{"target gone", &fakeComp{gone: true}, action.StatusUndone, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newService(t, tt.comp)
			e := done(t, s, begin(t, s, "c1").ID)
			got, err := s.Undo(t.Context(), e.ID)
			if err != nil || got.Status != tt.want || tt.comp.applied != tt.applied {
				t.Fatalf("Undo = %+v, %v, applied %d", got, err, tt.comp.applied)
			}
			if _, err := s.Undo(t.Context(), e.ID); !errors.Is(err, action.ErrInvalidTransition) {
				t.Fatalf("second Undo = %v", err)
			}
		})
	}

	s, _ := newService(t, nil)
	e := done(t, s, begin(t, s, "c1").ID)
	if _, err := s.Undo(t.Context(), e.ID); !errors.Is(err, ErrUndoUnavailable) {
		t.Fatalf("Undo without a compensator = %v", err)
	}
	if got, _ := s.List(t.Context(), "th1", 10); got[0].Status != action.StatusDone {
		t.Fatalf("refused Undo changed the entry: %+v", got[0])
	}
}

// The target changes between the version check and the compensation: Apply's compare-and-swap refuses it.
func TestUndoTargetChangedAfterCheck(t *testing.T) {
	comp := &fakeComp{versions: map[action.CompensationKind]int64{action.CompRemoveSubject: 1}}
	comp.afterCheck = func() { comp.versions[action.CompRemoveSubject]++ } // the student edits the subject meanwhile
	s, _ := newService(t, comp)
	e := done(t, s, begin(t, s, "c1").ID)
	got, err := s.Undo(t.Context(), e.ID)
	if err != nil || got.Status != action.StatusUndoRefused || comp.applied != 0 {
		t.Fatalf("Undo = %+v, %v, applied %d", got, err, comp.applied)
	}
}

func TestFailedCompensationStaysUndoing(t *testing.T) {
	comp := &fakeComp{versions: map[action.CompensationKind]int64{action.CompRemoveSubject: 1}, failing: errors.New("disk")}
	s, _ := newService(t, comp)
	e := done(t, s, begin(t, s, "c1").ID)
	if _, err := s.Undo(t.Context(), e.ID); err == nil {
		t.Fatal("Undo with a failing compensation succeeded")
	}
	list, _ := s.List(t.Context(), "th1", 10)
	if list[0].Status != action.StatusUndoing {
		t.Fatalf("status = %s, want UNDOING", list[0].Status)
	}

	// At the next start the compensation is retried.
	comp.failing = nil
	rep, err := s.Recover(t.Context())
	if err != nil || rep != (RecoverReport{Undone: 1}) {
		t.Fatalf("Recover = %+v, %v", rep, err)
	}
}

func TestRecover(t *testing.T) {
	comp := &fakeComp{versions: map[action.CompensationKind]int64{action.CompRemoveSubject: 1}}
	s, repo := newService(t, comp)
	pending := begin(t, s, "c1")
	undoing := done(t, s, begin(t, s, "c2").ID)
	if _, err := s.BeginUndo(t.Context(), undoing.ID); err != nil {
		t.Fatal(err)
	}
	kept := done(t, s, begin(t, s, "c3").ID)

	rep, err := s.Recover(t.Context())
	if err != nil || rep != (RecoverReport{Unknown: 1, Undone: 1}) {
		t.Fatalf("Recover = %+v, %v", rep, err)
	}
	for id, want := range map[shared.ActionID]action.Status{
		pending.ID: action.StatusUnknown, undoing.ID: action.StatusUndone, kept.ID: action.StatusDone,
	} {
		a, _ := repo.Get(t.Context(), id)
		if a.Status() != want {
			t.Errorf("%s = %s, want %s", id, a.Status(), want)
		}
	}
	if rep, err := s.Recover(t.Context()); err != nil || rep != (RecoverReport{}) {
		t.Fatalf("second Recover = %+v, %v", rep, err)
	}

	noComp, _ := newService(t, nil)
	undo := done(t, noComp, begin(t, noComp, "c1").ID)
	if _, err := noComp.BeginUndo(t.Context(), undo.ID); !errors.Is(err, ErrUndoUnavailable) {
		t.Fatalf("BeginUndo without a compensator = %v", err)
	}
	if rep, err := noComp.Recover(t.Context()); err != nil || rep != (RecoverReport{}) {
		t.Fatalf("Recover without entries to retry = %+v, %v", rep, err)
	}
}

func TestList(t *testing.T) {
	s, _ := newService(t, nil)
	for _, c := range []string{"c1", "c2", "c3"} {
		begin(t, s, c)
	}
	got, err := s.List(t.Context(), "th1", 2)
	if err != nil || len(got) != 2 || got[0].CallID != "c3" {
		t.Fatalf("List = %+v, %v", got, err)
	}
	if got, _ := s.List(t.Context(), "other", 0); len(got) != 0 {
		t.Fatalf("other thread = %+v", got)
	}
}
