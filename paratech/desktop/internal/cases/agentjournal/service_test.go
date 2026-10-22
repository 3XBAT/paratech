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
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/job"
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

func (r *memActions) GetByCall(ctx context.Context, th shared.ThreadID, turn int, callID string) (*action.Action, error) {
	for _, a := range r.all(ctx) {
		if a.Thread() == th && a.Turn() == turn && a.CallID() == callID {
			return a, nil
		}
	}
	return nil, fmt.Errorf("call %s: %w", callID, shared.ErrNotFound)
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
		for _, other := range r.states {
			if other.Thread == st.Thread && other.Turn == st.Turn && other.CallID == st.CallID {
				return shared.ErrVersionConflict // UNIQUE (thread_id, turn, call_id)
			}
		}
		r.order = append(r.order, st.ID)
	}
	st.Version++
	r.states[st.ID] = st
	a.Persisted(st.Version)
	return nil
}

// all returns every entry, newest first.
func (r *memActions) all(ctx context.Context) []*action.Action {
	r.mu.Lock()
	ids := slices.Clone(r.order)
	r.mu.Unlock()
	slices.Reverse(ids)
	out := make([]*action.Action, 0, len(ids))
	for _, id := range ids {
		a, _ := r.Get(ctx, id)
		out = append(out, a)
	}
	return out
}

func (r *memActions) ListByThread(ctx context.Context, th shared.ThreadID, limit int) ([]*action.Action, error) {
	var out []*action.Action
	for _, a := range r.all(ctx) {
		if a.Thread() == th && len(out) < limit {
			out = append(out, a)
		}
	}
	return out, nil
}

func (r *memActions) ListRecent(ctx context.Context, limit int) ([]*action.Action, error) {
	all := r.all(ctx)
	return all[:min(limit, len(all))], nil
}

func (r *memActions) ListUnfinished(ctx context.Context) ([]*action.Action, error) {
	var out []*action.Action
	for _, a := range slices.Backward(r.all(ctx)) {
		if a.Status() == action.StatusPending || a.Status() == action.StatusUndoing {
			out = append(out, a)
		}
	}
	return out, nil
}

// fakeComp keeps target states by the compensation kind; Apply compares and swaps the version like the adapter must,
// and bumps it like a real change would. afterCheck runs after Check, to change the target in between.
type fakeComp struct {
	targets    map[action.CompensationKind]action.Target
	gone       bool
	failing    error
	applyErr   error
	applied    []action.Compensation
	afterCheck func()
}

func (f *fakeComp) Check(_ context.Context, c action.Compensation) (action.Target, error) {
	if f.gone {
		return action.Target{}, shared.ErrNotFound
	}
	t := f.targets[c.Kind()]
	if f.afterCheck != nil {
		f.afterCheck()
	}
	return t, nil
}

func (f *fakeComp) Apply(_ context.Context, c action.Compensation, expectedVersion int64) (int64, error) {
	switch {
	case f.failing != nil:
		return 0, f.failing
	case f.applyErr != nil:
		return 0, f.applyErr
	case f.targets[c.Kind()].Version != expectedVersion:
		return 0, fmt.Errorf("fake compensator: %w", action.ErrTargetChanged)
	}
	f.applied = append(f.applied, c)
	t := f.targets[c.Kind()]
	t.Version++
	t.Compensated = true
	f.targets[c.Kind()] = t
	return t.Version, nil
}

func at(v int64) map[action.CompensationKind]action.Target {
	return map[action.CompensationKind]action.Target{action.CompRemoveSubject: {Version: v}}
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { c.now = c.now.Add(time.Second); return c.now }

type ids struct{ n int }

func (g *ids) NewActionID() shared.ActionID { g.n++; return shared.ActionID(fmt.Sprintf("a%d", g.n)) }

func newService(t *testing.T, comp Compensator) (*Service, *memActions) {
	t.Helper()
	return newServiceWith(t, Deps{Compensator: comp})
}

func newServiceWith(t *testing.T, d Deps) (*Service, *memActions) {
	t.Helper()
	repo := &memActions{}
	d.Actions, d.IDs = repo, &ids{}
	if d.Clock == nil {
		d.Clock = &clock{now: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)}
	}
	s, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	return s, repo
}

func ref(t *testing.T, kind values.IDKind, id string) values.Ref {
	t.Helper()
	r, err := values.NewRef(kind, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func begin(t *testing.T, s *Service, call string) Entry {
	t.Helper()
	return beginIn(t, s, BeginInput{Thread: "th1", Turn: 1, CallID: call, Tool: values.ToolCreateSubject})
}

func beginIn(t *testing.T, s *Service, in BeginInput) Entry {
	t.Helper()
	e, err := s.Begin(t.Context(), in)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	return e
}

func done(t *testing.T, s *Service, id shared.ActionID) Entry {
	t.Helper()
	e, err := s.Finish(t.Context(), id, Outcome{Compensation: action.RemoveSubject{Subject: ref(t, values.KindSubject, "s9")}, TargetVersion: 1})
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
	if e.Status != action.StatusPending || e.Key != "ag:th1:act:"+string(e.ID) || e.CanUndo || e.NoUndo != action.NoUndoRunning {
		t.Fatalf("begun = %+v", e)
	}
	e = done(t, s, e.ID)
	if e.Status != action.StatusDone || !e.CanUndo || e.Compensation == nil {
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

// A call run again under the same call ID finds its entry instead of acting twice.
func TestFindAndDuplicateCall(t *testing.T) {
	s, _ := newService(t, nil)
	e := done(t, s, begin(t, s, "c1").ID)
	got, err := s.Find(t.Context(), "th1", 1, "c1")
	if err != nil || got.ID != e.ID || got.Status != action.StatusDone {
		t.Fatalf("Find = %+v, %v", got, err)
	}
	if _, err := s.Begin(t.Context(), BeginInput{Thread: "th1", Turn: 1, CallID: "c1", Tool: values.ToolCreateSubject}); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("second Begin of the same call = %v", err)
	}
	if _, err := s.Find(t.Context(), "th1", 2, "c1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Find in another request = %v", err)
	}
}

func TestPropose(t *testing.T) {
	s, _ := newService(t, nil)
	p := ref(t, values.KindProposal, "p1")
	e, err := s.Propose(t.Context(), BeginInput{Thread: "th1", Turn: 1, CallID: "c1", Tool: values.ToolProposeSend}, p)
	if err != nil || e.Status != action.StatusProposed || e.Proposal != p || e.NoUndo != action.NoUndoIrreversible {
		t.Fatalf("proposed = %+v, %v", e, err)
	}
}

func TestUndo(t *testing.T) {
	jobTarget := func(tg action.Target) map[action.CompensationKind]action.Target {
		return map[action.CompensationKind]action.Target{action.CompRemoveSubject: tg}
	}
	tests := []struct {
		name    string
		comp    *fakeComp
		want    action.Status
		reason  action.NoUndo
		applied int
	}{
		{"target unchanged", &fakeComp{targets: at(1)}, action.StatusUndone, action.NoUndoUndone, 1},
		{"target changed", &fakeComp{targets: at(2)}, action.StatusUndoRefused, action.NoUndoChanged, 0},
		{"target gone", &fakeComp{gone: true}, action.StatusUndone, action.NoUndoUndone, 0},
		{"already compensated", &fakeComp{targets: jobTarget(action.Target{Version: 2, Compensated: true})}, action.StatusUndone, action.NoUndoUndone, 0},
		{"paid result ready", &fakeComp{targets: jobTarget(action.Target{Version: 1, Final: true})}, action.StatusUndoRefused, action.NoUndoResultReady, 0},
		{"job cannot stop", &fakeComp{targets: at(1), applyErr: action.ErrCannotStop}, action.StatusUndoRefused, action.NoUndoCannotStop, 0},
		{"job finished meanwhile", &fakeComp{targets: at(1), applyErr: action.ErrResultReady}, action.StatusUndoRefused, action.NoUndoResultReady, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newService(t, tt.comp)
			e := done(t, s, begin(t, s, "c1").ID)
			got, err := s.Undo(t.Context(), e.ID)
			if err != nil || got.Status != tt.want || got.NoUndo != tt.reason || len(tt.comp.applied) != tt.applied {
				t.Fatalf("Undo = %+v, %v, applied %d", got, err, len(tt.comp.applied))
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
	if got, _ := s.ListThread(t.Context(), "th1", 10); got[0].Status != action.StatusDone {
		t.Fatalf("refused Undo changed the entry: %+v", got[0])
	}
}

// The race «правка после действия»: the target changes between the version check and the compensation; Apply's
// compare-and-swap refuses it and nothing is overwritten.
func TestUndoTargetChangedAfterCheck(t *testing.T) {
	comp := &fakeComp{targets: at(1)}
	comp.afterCheck = func() { comp.targets = at(2) } // the student edits the subject meanwhile
	s, _ := newService(t, comp)
	e := done(t, s, begin(t, s, "c1").ID)
	got, err := s.Undo(t.Context(), e.ID)
	if err != nil || got.Status != action.StatusUndoRefused || got.NoUndo != action.NoUndoChanged || len(comp.applied) != 0 {
		t.Fatalf("Undo = %+v, %v, applied %d", got, err, len(comp.applied))
	}
}

func TestFailedCompensationStaysUndoing(t *testing.T) {
	comp := &fakeComp{targets: at(1), failing: errors.New("disk")}
	s, _ := newService(t, comp)
	e := done(t, s, begin(t, s, "c1").ID)
	if _, err := s.Undo(t.Context(), e.ID); err == nil {
		t.Fatal("Undo with a failing compensation succeeded")
	}
	list, _ := s.ListThread(t.Context(), "th1", 10)
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

// An Undo whose Apply failed transiently stays UNDOING: shown running at first, then offered again and retried by
// «Отменить» without a restart.
func TestFailedUndoIsRetriedOnceStale(t *testing.T) {
	comp := &fakeComp{targets: at(1), failing: errors.New("disk")}
	c := &clock{now: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)}
	s, _ := newServiceWith(t, Deps{Compensator: comp, Clock: c})
	e := done(t, s, begin(t, s, "c1").ID)
	if _, err := s.Undo(t.Context(), e.ID); err == nil {
		t.Fatal("Undo with a failing compensation succeeded")
	}
	comp.failing = nil

	if list, _ := s.ListThread(t.Context(), "th1", 10); list[0].Status != action.StatusUndoing || list[0].CanUndo || list[0].NoUndo != action.NoUndoRunning {
		t.Fatalf("right after the failure = %+v", list[0])
	}
	if _, err := s.Undo(t.Context(), e.ID); !errors.Is(err, action.ErrInvalidTransition) {
		t.Fatalf("Undo while it may still run = %v", err)
	}

	c.now = c.now.Add(action.StaleUndo)
	if list, _ := s.ListThread(t.Context(), "th1", 10); !list[0].CanUndo || list[0].NoUndo != action.NoUndoNone {
		t.Fatalf("stale UNDOING = %+v, want offered again", list[0])
	}
	got, err := s.Undo(t.Context(), e.ID)
	if err != nil || got.Status != action.StatusUndone || len(comp.applied) != 1 {
		t.Fatalf("retried Undo = %+v, %v, applied %d", got, err, len(comp.applied))
	}
}

// refComp keeps one version per target (a folder's is its subject's), like the library: undoing a folder moves the
// version of the subject an earlier create_subject left.
type refComp struct {
	versions map[values.Ref]int64
	applied  []int64 // the expected versions Apply was called with
}

func refOf(c action.Compensation) values.Ref {
	switch v := c.(type) {
	case action.RemoveSubject:
		return v.Subject
	case action.RemoveFolder:
		return v.Subject
	}
	return values.Ref{}
}

func (f *refComp) Check(_ context.Context, c action.Compensation) (action.Target, error) {
	return action.Target{Version: f.versions[refOf(c)]}, nil
}

func (f *refComp) Apply(_ context.Context, c action.Compensation, expected int64) (int64, error) {
	r := refOf(c)
	if f.versions[r] != expected {
		return 0, action.ErrTargetChanged
	}
	f.applied = append(f.applied, expected)
	f.versions[r]++
	return f.versions[r], nil
}

// «Отменить всё» of create_subject + create_folder in it: undoing the folder moves the subject's version, and the
// subject follows it (action.Rebase), so both are undone; a change by the student in between still refuses.
func TestUndoRequestFollowsTheTarget(t *testing.T) {
	tests := []struct {
		name        string
		edited      bool // the student renamed the subject between the two actions
		wantSubject action.Status
		wantApplied []int64
	}{
		{"both undone", false, action.StatusUndone, []int64{2, 3}},
		{"student change in between", true, action.StatusUndoRefused, []int64{3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s9 := ref(t, values.KindSubject, "s9")
			comp := &refComp{versions: map[values.Ref]int64{s9: 1}}
			s, _ := newService(t, comp)
			sub := done(t, s, begin(t, s, "c1").ID) // RemoveSubject s9 at version 1
			if tt.edited {
				comp.versions[s9]++
			}
			before := comp.versions[s9]
			folder := beginIn(t, s, BeginInput{Thread: "th1", Turn: 1, CallID: "c2", Tool: values.ToolCreateFolder,
				Intent: action.Intent{Target: s9, Before: before, Name: "Лабы"}})
			comp.versions[s9]++
			if _, err := s.Finish(t.Context(), folder.ID, Outcome{TargetVersion: comp.versions[s9], Compensation: action.RemoveFolder{
				Subject: s9, Folder: ref(t, values.KindFolder, "f1"),
			}}); err != nil {
				t.Fatal(err)
			}

			got, err := s.UndoRequest(t.Context(), "th1", 1)
			if err != nil || len(got) != 2 || got[0].ID != folder.ID || got[1].ID != sub.ID {
				t.Fatalf("UndoRequest = %+v, %v", got, err)
			}
			if got[0].Status != action.StatusUndone || got[1].Status != tt.wantSubject || !slices.Equal(comp.applied, tt.wantApplied) {
				t.Fatalf("statuses %s, %s; applied %v, want %s %v", got[0].Status, got[1].Status, comp.applied, tt.wantSubject, tt.wantApplied)
			}
		})
	}
}

// A crash after the compensation applied but before UNDONE was saved: at the next start the target is already
// compensated (its version moved on), which is UNDONE, not «Изменено после».
func TestUndoInterruptedAfterApply(t *testing.T) {
	comp := &fakeComp{targets: at(1)}
	s, repo := newService(t, comp)
	e := done(t, s, begin(t, s, "c1").ID)
	if _, err := s.BeginUndo(t.Context(), e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := comp.Apply(t.Context(), action.RemoveSubject{}, 1); err != nil { // applied, then the app stopped
		t.Fatal(err)
	}
	rep, err := s.Recover(t.Context())
	if err != nil || rep != (RecoverReport{Undone: 1}) {
		t.Fatalf("Recover = %+v, %v", rep, err)
	}
	if a, _ := repo.Get(t.Context(), e.ID); a.Status() != action.StatusUndone || len(comp.applied) != 1 {
		t.Fatalf("status %s, applied %d", a.Status(), len(comp.applied))
	}
}

// fakeResolver answers Observe from a table by tool.
type fakeResolver struct {
	obs map[values.Tool]action.Observation
	err error
}

func (f fakeResolver) Observe(_ context.Context, tool values.Tool, _ action.Intent, _ time.Time) (action.Observation, error) {
	return f.obs[tool], f.err
}

// A crash between the two transactions of an action: the use case did not run (FAILED), it ran but Finish was not
// saved (DONE with Undo), a job (UNKNOWN), a target that cannot be read now (left for the next start).
func TestRecoverSettlesPendingEntries(t *testing.T) {
	a1 := ref(t, values.KindAssignment, "a1")
	r1 := ref(t, values.KindRecording, "r1")
	due := action.RestoreDue{Assignment: a1, OldDue: time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC)}
	info := action.RestoreLessonInfo{Recording: r1, OldKind: "lab"}
	res := fakeResolver{obs: map[values.Tool]action.Observation{
		values.ToolEditDue:       {Known: true, Version: 5, Effect: true, Compensation: due},
		values.ToolSetLessonInfo: {Known: true, Version: 7},
	}}
	s, repo := newServiceWith(t, Deps{Compensator: &fakeComp{}, Resolver: res})
	ran := beginIn(t, s, BeginInput{Thread: "th1", Turn: 1, CallID: "c1", Tool: values.ToolEditDue,
		Intent: action.Intent{Target: a1, Before: 4, Prior: due}})
	notRun := beginIn(t, s, BeginInput{Thread: "th1", Turn: 1, CallID: "c2", Tool: values.ToolSetLessonInfo,
		Intent: action.Intent{Target: r1, Before: 7, Prior: info}})
	job := beginIn(t, s, BeginInput{Thread: "th1", Turn: 1, CallID: "c3", Tool: values.ToolMakeSummary})

	rep, err := s.Recover(t.Context())
	if err != nil || rep != (RecoverReport{Done: 1, Failed: 1, Unknown: 1}) {
		t.Fatalf("Recover = %+v, %v", rep, err)
	}
	for id, want := range map[shared.ActionID]action.Status{
		ran.ID: action.StatusDone, notRun.ID: action.StatusFailed, job.ID: action.StatusUnknown,
	} {
		if a, _ := repo.Get(t.Context(), id); a.Status() != want {
			t.Errorf("%s = %s, want %s", id, a.Status(), want)
		}
	}
	if e, _ := s.Find(t.Context(), "th1", 1, "c1"); !e.CanUndo || e.Compensation != due {
		t.Fatalf("settled entry = %+v", e)
	}

	broken, _ := newServiceWith(t, Deps{Resolver: fakeResolver{err: errors.New("disk")}})
	left := beginIn(t, broken, BeginInput{Thread: "th1", Turn: 1, CallID: "c1", Tool: values.ToolEditDue,
		Intent: action.Intent{Target: a1, Before: 4, Prior: due}})
	if rep, err := broken.Recover(t.Context()); err != nil || rep != (RecoverReport{Left: 1}) {
		t.Fatalf("Recover with an unreadable target = %+v, %v", rep, err)
	}
	if e, _ := broken.Find(t.Context(), "th1", 1, "c1"); e.ID != left.ID || e.Status != action.StatusPending {
		t.Fatalf("unreadable entry = %+v", e)
	}
}

func TestRecover(t *testing.T) {
	comp := &fakeComp{targets: at(1)}
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

// «Отменить всё»: the actions of one request, newest first; a refusal does not stop the others.
func TestUndoRequest(t *testing.T) {
	order := []shared.ActionID{}
	comp := &fakeComp{targets: map[action.CompensationKind]action.Target{
		action.CompRemoveSubject: {Version: 1}, action.CompRemoveFolder: {Version: 9},
	}}
	s, _ := newService(t, comp)
	first := done(t, s, begin(t, s, "c1").ID)
	folder := beginIn(t, s, BeginInput{Thread: "th1", Turn: 1, CallID: "c2", Tool: values.ToolCreateFolder})
	if _, err := s.Finish(t.Context(), folder.ID, Outcome{TargetVersion: 1, Compensation: action.RemoveFolder{
		Subject: ref(t, values.KindSubject, "s9"), Folder: ref(t, values.KindFolder, "f1"),
	}}); err != nil {
		t.Fatal(err)
	}
	third := beginIn(t, s, BeginInput{Thread: "th1", Turn: 1, CallID: "c3", Tool: values.ToolCreateSubject})
	if _, err := s.Finish(t.Context(), third.ID, Outcome{Failed: true}); err != nil {
		t.Fatal(err)
	}
	other := beginIn(t, s, BeginInput{Thread: "th1", Turn: 2, CallID: "c1", Tool: values.ToolCreateSubject})
	done(t, s, other.ID)

	got, err := s.UndoRequest(t.Context(), "th1", 1)
	if err != nil || len(got) != 2 {
		t.Fatalf("UndoRequest = %+v, %v", got, err)
	}
	for _, e := range got {
		order = append(order, e.ID)
	}
	if !slices.Equal(order, []shared.ActionID{folder.ID, first.ID}) {
		t.Fatalf("order = %v, want newest first", order)
	}
	if got[0].Status != action.StatusUndoRefused || got[1].Status != action.StatusUndone {
		t.Fatalf("statuses = %s, %s", got[0].Status, got[1].Status)
	}
	if e, _ := s.Find(t.Context(), "th1", 2, "c1"); e.Status != action.StatusDone {
		t.Fatalf("another request was undone: %+v", e)
	}
}

func TestListThread(t *testing.T) {
	s, _ := newService(t, nil)
	for _, c := range []string{"c1", "c2", "c3"} {
		begin(t, s, c)
	}
	got, err := s.ListThread(t.Context(), "th1", 2)
	if err != nil || len(got) != 2 || got[0].CallID != "c3" {
		t.Fatalf("ListThread = %+v, %v", got, err)
	}
	if got, _ := s.ListThread(t.Context(), "other", 0); len(got) != 0 {
		t.Fatalf("other thread = %+v", got)
	}
}

type memThreads map[shared.ThreadID]*thread.Thread

func (m memThreads) Get(_ context.Context, id shared.ThreadID) (*thread.Thread, error) {
	if th, ok := m[id]; ok {
		return th, nil
	}
	return nil, shared.ErrNotFound
}

type memJobs []*job.Job

func (m memJobs) ListActivity(context.Context, int, []job.Type) ([]*job.Job, []shared.JobID, error) {
	return m, nil, nil
}

// «Журнал» is one list: the actions of paratech (undone ones included, with the subject of their thread) and the
// processing jobs, newest first.
func TestListMergesActionsAndProcessing(t *testing.T) {
	key, err := thread.SubjectKey(ref(t, values.KindSubject, "s1"))
	if err != nil {
		t.Fatal(err)
	}
	th, err := thread.New("th1", key, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	j, err := job.New(job.Spec{ID: "j1", Type: "summarize", Key: "k1", MaxAttempts: 1}, time.Date(2026, 10, 22, 9, 0, 2, 500, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	comp := &fakeComp{targets: at(1)}
	s, _ := newServiceWith(t, Deps{Compensator: comp, Threads: memThreads{"th1": th}, Processing: memJobs{j}})
	undone := done(t, s, begin(t, s, "c1").ID) // created at 09:00:01
	if _, err := s.Undo(t.Context(), undone.ID); err != nil {
		t.Fatal(err)
	}
	later := done(t, s, beginIn(t, s, BeginInput{Thread: "th2", Turn: 1, CallID: "c1", Tool: values.ToolCreateSubject}).ID)

	lines, err := s.List(t.Context(), 0)
	if err != nil || len(lines) != 3 {
		t.Fatalf("List = %+v, %v", lines, err)
	}
	if lines[0].Action != later.ID || lines[0].Subject != "" || !lines[0].CanUndo {
		t.Fatalf("newest line = %+v", lines[0])
	}
	if lines[1].Kind != LineProcessing || lines[1].Job != "j1" || lines[1].JobStatus != job.StatusQueued {
		t.Fatalf("processing line = %+v", lines[1])
	}
	if lines[2].Action != undone.ID || lines[2].Status != action.StatusUndone || lines[2].Subject != "s1" ||
		lines[2].CanUndo || lines[2].NoUndo != action.NoUndoUndone {
		t.Fatalf("undone line = %+v", lines[2])
	}
	if got, _ := s.List(t.Context(), 1); len(got) != 1 {
		t.Fatalf("List(1) = %d lines", len(got))
	}
}
