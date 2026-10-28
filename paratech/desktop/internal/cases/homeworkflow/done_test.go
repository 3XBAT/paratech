package homeworkflow

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// countingTx counts the transactions of a call.
type countingTx struct {
	Tx
	n *int
}

func (x countingTx) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	*x.n++
	return x.Tx.Do(ctx, fn)
}

// doneEnv is the create env with three assignments a, b, c added by the student, oldest first, and d deleted.
func doneEnv(t *testing.T) (*createEnv, []shared.AssignmentID, shared.AssignmentID) {
	t.Helper()
	e := newCreateEnv(t, testSubjects)
	ctx := context.Background()
	var ids []shared.AssignmentID
	for _, title := range []string{"a", "b", "c", "d"} {
		e.clock.Advance(time.Minute)
		v, err := e.svc.CreateAssignment(ctx, NewAssignment{Title: title, Date: "2026-10-16"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, v.ID)
	}
	if _, err := e.svc.DeleteAssignment(ctx, ids[3], 1); err != nil {
		t.Fatal(err)
	}
	return e, ids[:3], ids[3]
}

// codes is the outcome codes by id.
func codes(m DoneMarks) map[shared.AssignmentID]DoneCode {
	out := map[shared.AssignmentID]DoneCode{}
	for _, o := range m.Outcomes {
		out[o.ID] = o.Code
	}
	return out
}

// listed is the ids ListAssignmentsByState returns, in order.
func listed(t *testing.T, e *createEnv, st ListState) []shared.AssignmentID {
	t.Helper()
	list, err := e.svc.ListAssignmentsByState(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	ids := []shared.AssignmentID{}
	for _, v := range list {
		ids = append(ids, v.ID)
	}
	return ids
}

func TestMarkDonePartialFailures(t *testing.T) {
	e, ids, deleted := doneEnv(t)
	var txs int
	e.svc.tx = countingTx{Tx: e.svc.tx, n: &txs}
	e.clock.Advance(time.Minute)
	markedAt := e.clock.Now()

	got, err := e.svc.MarkDone(context.Background(), []shared.AssignmentID{ids[0], "missing", deleted, "bad id", ids[1], ids[0]}, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		id   shared.AssignmentID
		code DoneCode
		err  error
	}{
		{ids[0], DoneChanged, nil}, {"missing", DoneNotFound, shared.ErrNotFound}, {deleted, DoneDeleted, assignment.ErrDeleted},
		{"bad id", DoneNotFound, shared.ErrInvalidID}, {ids[1], DoneChanged, nil},
	}
	if len(got.Outcomes) != len(want) {
		t.Fatalf("outcomes = %+v, want one per distinct id", got.Outcomes)
	}
	for i, w := range want {
		o := got.Outcomes[i]
		if o.ID != w.id || o.Code != w.code || (w.err == nil) != (o.Err == nil) || (w.err != nil && !errors.Is(o.Err, w.err)) {
			t.Errorf("outcome %d = %+v, want %s %s %v", i, o, w.id, w.code, w.err)
		}
		if w.code == DoneChanged && (!o.Done || o.Version != 2) {
			t.Errorf("outcome %d = %+v, want done at version 2", i, o)
		}
	}
	if !slices.Equal(got.Prior, []DonePrior{{ID: ids[0], Done: false, Version: 2}, {ID: ids[1], Done: false, Version: 2}}) {
		t.Errorf("prior = %+v, want exactly the changed ones", got.Prior)
	}
	// One transaction per well-formed distinct id: a, missing, deleted, b.
	if txs != 4 {
		t.Errorf("transactions = %d, want 4", txs)
	}
	if st := e.assignments[ids[0]]; !st.DoneAt.Equal(markedAt) || !e.assignments[ids[2]].DoneAt.IsZero() {
		t.Errorf("stored: a done at %v, c done at %v", st.DoneAt, e.assignments[ids[2]].DoneAt)
	}
	if !e.assignments[deleted].DoneAt.IsZero() {
		t.Error("the deleted assignment was marked")
	}
}

func TestMarkDoneStorageFailureGoesOn(t *testing.T) {
	e, ids, _ := doneEnv(t)
	e.mu.Lock()
	e.failNextSave = errDiskFull
	e.mu.Unlock()

	got, err := e.svc.MarkDone(context.Background(), ids[:2], true)
	if err != nil {
		t.Fatal(err)
	}
	if c := codes(got); c[ids[0]] != DoneFailed || c[ids[1]] != DoneChanged || !errors.Is(got.Outcomes[0].Err, errDiskFull) {
		t.Fatalf("outcomes = %+v", got.Outcomes)
	}
	if len(got.Prior) != 1 || got.Prior[0].ID != ids[1] {
		t.Fatalf("prior = %+v, want only b", got.Prior)
	}
	if !e.assignments[ids[0]].DoneAt.IsZero() {
		t.Error("a failed save left a mark")
	}
}

func TestMarkDoneIsIdempotent(t *testing.T) {
	e, ids, _ := doneEnv(t)
	ctx := context.Background()
	first, err := e.svc.MarkDone(ctx, ids[:2], true)
	if err != nil || len(first.Prior) != 2 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	saves := e.saves

	again, err := e.svc.MarkDone(ctx, ids[:2], true)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range again.Outcomes {
		if o.Code != DoneUnchanged || !o.Done || o.Version != 2 || o.Err != nil {
			t.Errorf("repeat outcome = %+v, want unchanged, done at version 2", o)
		}
	}
	if len(again.Prior) != 0 || e.saves != saves {
		t.Fatalf("repeat: prior %+v, %d saves; want nothing to undo and nothing saved", again.Prior, e.saves-saves)
	}

	// «Вернуть в работу» of one never done is unchanged too.
	open, err := e.svc.MarkDone(ctx, ids[2:], false)
	if err != nil || codes(open)[ids[2]] != DoneUnchanged || len(open.Prior) != 0 {
		t.Fatalf("undone of an open one = %+v, %v", open, err)
	}
}

func TestMarkDoneLimits(t *testing.T) {
	e, ids, _ := doneEnv(t)
	ctx := context.Background()
	many := func(n int) []shared.AssignmentID {
		out := make([]shared.AssignmentID, n)
		for i := range out {
			out[i] = shared.AssignmentID(fmt.Sprintf("x%02d", i))
		}
		return out
	}
	full := append(many(MaxMarkDone-1), ids[0])
	got, err := e.svc.MarkDone(ctx, full, true)
	if err != nil || len(got.Outcomes) != MaxMarkDone || codes(got)[ids[0]] != DoneChanged {
		t.Fatalf("MarkDone of %d = %d outcomes, %v", MaxMarkDone, len(got.Outcomes), err)
	}

	for _, n := range []int{0, MaxMarkDone + 1} {
		saves := e.saves
		if _, err := e.svc.MarkDone(ctx, append(many(max(n-1, 0)), ids[1])[:n], true); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("MarkDone of %d: err = %v, want ErrInvalidRequest", n, err)
		}
		if e.saves != saves || !e.assignments[ids[1]].DoneAt.IsZero() {
			t.Errorf("MarkDone of %d changed something", n)
		}
	}

	prior := make([]DonePrior, MaxMarkDone+1)
	for i := range prior {
		prior[i] = DonePrior{ID: ids[0], Version: 2}
	}
	if _, err := e.svc.RestoreDone(ctx, prior); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("RestoreDone of %d: err = %v, want ErrInvalidRequest", len(prior), err)
	}
	if e.assignments[ids[0]].DoneAt.IsZero() {
		t.Error("a refused RestoreDone changed the mark")
	}
	if empty, err := e.svc.RestoreDone(ctx, nil); err != nil || len(empty.Outcomes) != 0 || len(empty.Prior) != 0 {
		t.Errorf("RestoreDone of none = %+v, %v; want nothing", empty, err)
	}
}

func TestRestoreDoneUndoesExactlyTheChangedOnes(t *testing.T) {
	e, ids, _ := doneEnv(t)
	ctx := context.Background()
	if _, err := e.svc.MarkDone(ctx, ids[2:], true); err != nil { // c was done before
		t.Fatal(err)
	}
	cDone := e.assignments[ids[2]].DoneAt

	e.clock.Advance(time.Minute)
	marked, err := e.svc.MarkDone(ctx, ids, true)
	if err != nil || len(marked.Prior) != 2 {
		t.Fatalf("marked = %+v, %v", marked, err)
	}

	e.clock.Advance(time.Minute)
	undo, err := e.svc.RestoreDone(ctx, marked.Prior)
	if err != nil {
		t.Fatal(err)
	}
	if c := codes(undo); len(c) != 2 || c[ids[0]] != DoneChanged || c[ids[1]] != DoneChanged {
		t.Fatalf("undo = %+v", undo.Outcomes)
	}
	if !e.assignments[ids[0]].DoneAt.IsZero() || !e.assignments[ids[1]].DoneAt.IsZero() || !e.assignments[ids[2]].DoneAt.Equal(cDone) {
		t.Fatal("the undo did not put back exactly the prior state: a and b open, c done as before")
	}
	if !slices.Equal(listed(t, e, ListDone), []shared.AssignmentID{ids[2]}) {
		t.Fatalf("done list = %v", listed(t, e, ListDone))
	}

	// Undo of the undo marks a and b done again, at the time of that restore.
	e.clock.Advance(time.Minute)
	redo, err := e.svc.RestoreDone(ctx, undo.Prior)
	if err != nil || len(redo.Prior) != 2 {
		t.Fatalf("redo = %+v, %v", redo, err)
	}
	for _, id := range ids[:2] {
		if st := e.assignments[id]; !st.DoneAt.Equal(e.clock.Now()) || st.Version != 4 {
			t.Errorf("%s after redo: %+v", id, st)
		}
	}

	// A stale compensation: the same undo again meets the versions the redo saved.
	stale, err := e.svc.RestoreDone(ctx, undo.Prior)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range stale.Outcomes {
		if o.Code != DoneConflict || !errors.Is(o.Err, shared.ErrVersionConflict) {
			t.Errorf("stale outcome = %+v, want a conflict", o)
		}
	}
	if len(stale.Prior) != 0 || e.assignments[ids[0]].DoneAt.IsZero() {
		t.Fatal("a stale compensation changed something")
	}
}

func TestRestoreDoneConflictWhenChangedAfter(t *testing.T) {
	e, ids, deleted := doneEnv(t)
	ctx := context.Background()
	marked, err := e.svc.MarkDone(ctx, ids[:2], true)
	if err != nil {
		t.Fatal(err)
	}
	// a is edited after the mark; b is fine.
	if _, err := e.svc.EditAssignment(ctx, AssignmentEdit{ID: ids[0], ExpectedVersion: 2, Title: "a, правка"}); err != nil {
		t.Fatal(err)
	}
	prior := append(slices.Clone(marked.Prior), DonePrior{ID: deleted, Version: 2}, DonePrior{ID: "missing", Version: 1})

	undo, err := e.svc.RestoreDone(ctx, prior)
	if err != nil {
		t.Fatal(err)
	}
	c := codes(undo)
	if c[ids[0]] != DoneConflict || c[ids[1]] != DoneChanged || c[deleted] != DoneDeleted || c["missing"] != DoneNotFound {
		t.Fatalf("undo = %+v", undo.Outcomes)
	}
	if e.assignments[ids[0]].DoneAt.IsZero() || !e.assignments[ids[1]].DoneAt.IsZero() {
		t.Fatal("want a still done (changed after the mark) and b open again")
	}
	if len(undo.Prior) != 1 || undo.Prior[0] != (DonePrior{ID: ids[1], Done: true, Version: 3}) {
		t.Fatalf("prior = %+v", undo.Prior)
	}
}

// The filter of «Открытые / Выполненные / Все»: open by default, done after open in all, deleted never.
func TestListAssignmentsByState(t *testing.T) {
	e, ids, deleted := doneEnv(t)
	ctx := context.Background()
	if _, err := e.svc.MarkDone(ctx, []shared.AssignmentID{ids[0], ids[2]}, true); err != nil { // the oldest and the newest
		t.Fatal(err)
	}

	want := map[ListState][]shared.AssignmentID{
		ListOpen: {ids[1]}, "": {ids[1]}, ListDone: {ids[2], ids[0]}, ListAll: {ids[1], ids[2], ids[0]},
	}
	for st, w := range want {
		if got := listed(t, e, st); !slices.Equal(got, w) {
			t.Errorf("state %q = %v, want %v", st, got, w)
		}
	}
	// ListAssignments is the open list: what «Сегодня» and the agent's list_assignments (catalog 9) read.
	list, err := e.svc.ListAssignments(ctx)
	if err != nil || len(list) != 1 || list[0].ID != ids[1] || list[0].Done || !list[0].DoneAt.IsZero() {
		t.Fatalf("ListAssignments = %+v, %v; want only the open one", list, err)
	}
	all, err := e.svc.ListAssignmentsByState(ctx, ListAll)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range all {
		if v.ID == deleted {
			t.Error("a deleted assignment is listed")
		}
		if v.Done != (v.ID != ids[1]) || v.Done == v.DoneAt.IsZero() {
			t.Errorf("view %s: done %v at %v", v.ID, v.Done, v.DoneAt)
		}
	}
	if _, err := e.svc.ListAssignmentsByState(ctx, "later"); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("unknown state: err = %v, want ErrInvalidRequest", err)
	}
}

// Marking done sends nothing to Google Calendar and leaves the deadline-event projection as it is (ADR-0033 О-13 is
// open): no calendar call, no proposal, no removal queued, no sync asked.
func TestMarkDoneLeavesTheCalendarAlone(t *testing.T) {
	e, ids, _ := doneEnv(t)
	removals, syncs := withRemovals(e)
	ctx := context.Background()
	cal := e.svc.calendar.(*testenv.FakeCalendar)
	props := e.svc.proposals.(proposalRepo).s
	props.mu.Lock()
	proposals := len(props.proposals)
	props.mu.Unlock()

	marked, err := e.svc.MarkDone(ctx, ids, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RestoreDone(ctx, marked.Prior); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.MarkDone(ctx, ids, false); err != nil {
		t.Fatal(err)
	}

	props.mu.Lock()
	defer props.mu.Unlock()
	if len(cal.Calls()) != 0 || len(removals.queued) != 0 || syncs.n != 0 || len(props.proposals) != proposals {
		t.Fatalf("calendar calls %d, removals %v, syncs %d, proposals %d -> %d; want none",
			len(cal.Calls()), removals.queued, syncs.n, proposals, len(props.proposals))
	}
}

// Reprocessing a transcript keeps a done assignment as it is, with its mark, and updates the open ones.
func TestReprocessingKeepsTheDoneMark(t *testing.T) {
	e := newCreateEnv(t, testSubjects)
	e.llm.Answer(values.OperationExtractHomework, extractAnswer)
	if err := e.handle(e.snapshot("j1", ExtractHomeworkJobType, "t1")); err != nil {
		t.Fatal(err)
	}
	id := e.assignmentsOf("t1")[0].ID
	if _, err := e.svc.MarkDone(context.Background(), []shared.AssignmentID{id}, true); err != nil {
		t.Fatal(err)
	}
	doneAt := e.assignments[id].DoneAt

	e.llm.Answer(values.OperationExtractHomework, `{"assignments":[`+
		`{"title":"Read chapter 5","description":"","due":"2026-10-17T23:59:00+03:00"},`+
		`{"title":"Map of rivers and lakes","description":"","due":null}]}`)
	if err := e.handle(e.snapshot("j2", ExtractHomeworkJobType, "t1")); err != nil {
		t.Fatal(err)
	}
	got := e.assignmentsOf("t1")
	if st := got[0]; st.ID != id || st.Title != "Read chapter 4" || !st.DoneAt.Equal(doneAt) {
		t.Fatalf("after reprocessing = %+v, want the old title and the mark kept at %v", st, doneAt)
	}
	if st := got[1]; st.Title != "Map of rivers and lakes" || !st.DoneAt.IsZero() {
		t.Fatalf("open assignment after reprocessing = %+v, want the new title", st)
	}
}

// Editing or correcting a done assignment keeps its mark: only MarkDone changes it.
func TestEditKeepsTheDoneMark(t *testing.T) {
	e, ids, _ := doneEnv(t)
	ctx := context.Background()
	if _, err := e.svc.MarkDone(ctx, ids[:1], true); err != nil {
		t.Fatal(err)
	}
	doneAt := e.assignments[ids[0]].DoneAt

	e.clock.Advance(time.Minute)
	edited, err := e.svc.EditAssignment(ctx, AssignmentEdit{ID: ids[0], ExpectedVersion: 2, Title: "a, правка", Date: "2026-10-20"})
	if err != nil {
		t.Fatal(err)
	}
	corrected, err := e.svc.CorrectLesson(ctx, LessonCorrection{ID: ids[0], Teacher: "Иванов"})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []AssignmentView{edited, corrected} {
		if !v.Done || !v.DoneAt.Equal(doneAt) {
			t.Errorf("view = done %v at %v, want done at %v", v.Done, v.DoneAt, doneAt)
		}
	}
	if !strings.HasPrefix(e.assignments[ids[0]].Title, "a, правка") || !e.assignments[ids[0]].DoneAt.Equal(doneAt) {
		t.Fatalf("stored = %+v", e.assignments[ids[0]])
	}
	if !slices.Equal(listed(t, e, ListDone), ids[:1]) {
		t.Fatal("the edited done assignment left the done list")
	}
}
