package homeworkflow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/shared"
)

// purgeNow is the time of the purge runs; the window of an assignment deleted at purgeNow-RestoreWindow ends exactly
// now, so it is not due yet.
var purgeNow = t0.Add(30 * 24 * time.Hour)

// Steps of the purge fake, for the failure injection and the checks.
const (
	pstepList       = "list"
	pstepCheck      = "re-check"
	pstepDeliveries = "list deliveries"
	pstepFile       = "file"
	pstepProposals  = "proposals"
	pstepDelivRows  = "deliveries"
	pstepDraft      = "draft"
	pstepWorks      = "works"
	pstepAssignment = "assignment"
)

var errInjected = errors.New("injected failure")

// purgeDB is the local data of the purge fakes: the rows of each kind by assignment and the files.
type purgeDB struct {
	t  *testing.T
	mu sync.Mutex

	assignments map[shared.AssignmentID]assignment.State
	proposals   map[shared.AssignmentID]int
	deliveries  map[shared.AssignmentID][]delivery.State
	drafts      map[shared.AssignmentID]int // the number of versions
	works       map[shared.AssignmentID]int
	files       map[shared.ArtifactRef]bool

	failStep string                                 // the step that fails, "" none
	failFor  shared.AssignmentID                    // only for this assignment, "" any
	before   map[string]func(a shared.AssignmentID) // runs before a step (a restore racing the purge)
	ran      []string                               // steps that ran inside a transaction
	lists    int                                    // calls of ListDeletedBefore
}

func newPurgeDB(t *testing.T) *purgeDB {
	return &purgeDB{
		t: t, assignments: map[shared.AssignmentID]assignment.State{}, proposals: map[shared.AssignmentID]int{},
		deliveries: map[shared.AssignmentID][]delivery.State{}, drafts: map[shared.AssignmentID]int{},
		works: map[shared.AssignmentID]int{}, files: map[shared.ArtifactRef]bool{},
		before: map[string]func(shared.AssignmentID){},
	}
}

// step checks a step of one assignment runs in a transaction, runs the hook and the injected failure of step for a.
func (db *purgeDB) step(ctx context.Context, step string, a shared.AssignmentID, inTx bool) error {
	if inTx && ctx.Value(purgeTxKey{}) == nil {
		db.t.Errorf("step %s of %s ran outside a transaction", step, a)
	}
	if hook := db.before[step]; hook != nil {
		hook(a)
	}
	if db.failStep == step && (db.failFor == "" || db.failFor == a) {
		return fmt.Errorf("%s %s: %w", step, a, errInjected)
	}
	if inTx {
		db.ran = append(db.ran, step)
	}
	return nil
}

// due is the guard every delete of the store repeats: id is still deleted before cutoff.
func (db *purgeDB) due(id shared.AssignmentID, cutoff time.Time) bool {
	st, ok := db.assignments[id]
	return ok && !st.DeletedAt.IsZero() && st.DeletedAt.Before(cutoff)
}

type purgeTxKey struct{}

// purgeTx rolls the rows back when fn fails, as SQLite does; files are not transactional. A row changed by a hook (a
// restore committed by somebody else) keeps that change: only the rows the purge removed come back.
type purgeTx struct{ db *purgeDB }

func (x purgeTx) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	db := x.db
	db.mu.Lock()
	rows, proposals, drafts, works := maps.Clone(db.assignments), maps.Clone(db.proposals), maps.Clone(db.drafts), maps.Clone(db.works)
	deliveries := maps.Clone(db.deliveries)
	db.mu.Unlock()

	err := fn(context.WithValue(ctx, purgeTxKey{}, true))
	if err != nil {
		db.mu.Lock()
		defer db.mu.Unlock()
		for id, st := range rows {
			if _, ok := db.assignments[id]; !ok {
				db.assignments[id] = st
			}
		}
		db.proposals, db.drafts, db.works, db.deliveries = proposals, drafts, works, deliveries
	}
	return err
}

type purgeAssignments struct{ db *purgeDB }

func (r purgeAssignments) ListDeletedBefore(ctx context.Context, cutoff, afterAt time.Time, afterID shared.AssignmentID,
	limit int) ([]*assignment.Assignment, error) {
	db := r.db
	db.mu.Lock()
	defer db.mu.Unlock()
	db.lists++
	if err := db.step(ctx, pstepList, "", false); err != nil {
		return nil, err
	}
	var out []*assignment.Assignment
	for _, st := range db.assignments {
		if st.DeletedAt.IsZero() || !st.DeletedAt.Before(cutoff) {
			continue
		}
		if !afterAt.IsZero() && (st.DeletedAt.Before(afterAt) || st.DeletedAt.Equal(afterAt) && st.ID <= afterID) {
			continue
		}
		a, err := assignment.Restore(st)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b *assignment.Assignment) int {
		if c := a.DeletedAt().Compare(b.DeletedAt()); c != 0 {
			return c
		}
		return strings.Compare(a.ID().String(), b.ID().String())
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r purgeAssignments) StillDeletedBefore(ctx context.Context, id shared.AssignmentID, cutoff time.Time) (bool, error) {
	db := r.db
	db.mu.Lock()
	defer db.mu.Unlock()
	if err := db.step(ctx, pstepCheck, id, true); err != nil {
		return false, err
	}
	return db.due(id, cutoff), nil
}

func (r purgeAssignments) PurgeDeleted(ctx context.Context, id shared.AssignmentID, cutoff time.Time) (bool, error) {
	db := r.db
	db.mu.Lock()
	defer db.mu.Unlock()
	if err := db.step(ctx, pstepAssignment, id, true); err != nil {
		return false, err
	}
	if !db.due(id, cutoff) {
		return false, nil
	}
	if len(db.deliveries[id]) > 0 || db.drafts[id] > 0 { // ON DELETE RESTRICT
		return false, assignment.ErrInUse
	}
	delete(db.assignments, id)
	delete(db.works, id) // ON DELETE CASCADE
	return true, nil
}

type purgeProposals struct{ db *purgeDB }

func (r purgeProposals) DeleteOfAssignment(ctx context.Context, a shared.AssignmentID, cutoff time.Time) (int, error) {
	r.db.mu.Lock()
	defer r.db.mu.Unlock()
	if err := r.db.step(ctx, pstepProposals, a, true); err != nil {
		return 0, err
	}
	if !r.db.due(a, cutoff) {
		return 0, nil
	}
	n := r.db.proposals[a]
	delete(r.db.proposals, a)
	return n, nil
}

type purgeDeliveries struct{ db *purgeDB }

func (r purgeDeliveries) ListByAssignment(ctx context.Context, a shared.AssignmentID, limit int) ([]*delivery.Delivery, error) {
	r.db.mu.Lock()
	defer r.db.mu.Unlock()
	if err := r.db.step(ctx, pstepDeliveries, a, true); err != nil {
		return nil, err
	}
	var out []*delivery.Delivery
	for _, st := range r.db.deliveries[a] {
		d, err := delivery.Restore(st)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r purgeDeliveries) DeleteFinalOfAssignment(ctx context.Context, a shared.AssignmentID, cutoff time.Time) (int, error) {
	r.db.mu.Lock()
	defer r.db.mu.Unlock()
	if err := r.db.step(ctx, pstepDelivRows, a, true); err != nil {
		return 0, err
	}
	if !r.db.due(a, cutoff) {
		return 0, nil
	}
	var kept []delivery.State
	n := 0
	for _, st := range r.db.deliveries[a] {
		if st.Status.IsTerminal() {
			n++
			continue
		}
		kept = append(kept, st)
	}
	if len(kept) == 0 {
		delete(r.db.deliveries, a)
	} else {
		r.db.deliveries[a] = kept
	}
	return n, nil
}

type purgeDrafts struct{ db *purgeDB }

func (r purgeDrafts) DeleteOfAssignment(ctx context.Context, a shared.AssignmentID, cutoff time.Time) (int, error) {
	r.db.mu.Lock()
	defer r.db.mu.Unlock()
	if err := r.db.step(ctx, pstepDraft, a, true); err != nil {
		return 0, err
	}
	if !r.db.due(a, cutoff) || r.db.drafts[a] == 0 {
		return 0, nil
	}
	delete(r.db.drafts, a)
	return 1, nil
}

type purgeWorks struct{ db *purgeDB }

func (r purgeWorks) DeleteOfAssignment(ctx context.Context, a shared.AssignmentID, cutoff time.Time) (int, error) {
	r.db.mu.Lock()
	defer r.db.mu.Unlock()
	if err := r.db.step(ctx, pstepWorks, a, true); err != nil {
		return 0, err
	}
	if !r.db.due(a, cutoff) {
		return 0, nil
	}
	n := r.db.works[a]
	delete(r.db.works, a)
	return n, nil
}

type purgeFiles struct{ db *purgeDB }

func (f purgeFiles) Delete(ctx context.Context, ref shared.ArtifactRef) error {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	// The file names the assignment in exports/<id>/...; a copy of a work's file is matched by the fake's own map.
	if err := f.db.step(ctx, pstepFile, fileOwner(ref), true); err != nil {
		return err
	}
	delete(f.db.files, ref)
	return nil
}

// fileOwner is the assignment of exports/<id>/..., the proposal directory of exports/sent/<proposal>/... otherwise.
func fileOwner(ref shared.ArtifactRef) shared.AssignmentID {
	first, rest, _ := strings.Cut(ref.Path(), "/")
	if first == "sent" {
		first, _, _ = strings.Cut(rest, "/")
	}
	return shared.AssignmentID(first)
}

type purgeClock struct{ now time.Time }

func (c purgeClock) Now() time.Time { return c.now }

type purgeEnv struct {
	db  *purgeDB
	p   *Purger
	log *bytes.Buffer
}

func newPurgeEnv(t *testing.T) *purgeEnv {
	t.Helper()
	db := newPurgeDB(t)
	var buf bytes.Buffer
	p, err := NewPurger(PurgeDeps{
		Assignments: purgeAssignments{db}, Proposals: purgeProposals{db}, Deliveries: purgeDeliveries{db},
		Drafts: purgeDrafts{db}, Works: purgeWorks{db}, Files: purgeFiles{db}, Clock: purgeClock{purgeNow},
		Tx: purgeTx{db}, Log: slog.New(slog.NewTextHandler(&buf, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &purgeEnv{db: db, p: p, log: &buf}
}

func mustRef(t *testing.T, kind shared.ArtifactKind, path string) shared.ArtifactRef {
	t.Helper()
	ref, err := shared.NewArtifactRef(kind, path)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

// deliveryState is a stored delivery of assignment a in status s attaching ref.
func deliveryState(id string, a shared.AssignmentID, s delivery.Status, ref shared.ArtifactRef) delivery.State {
	st := delivery.State{
		Version: 1, ID: shared.DeliveryID(id), Status: s, AssignmentID: a, Recipient: "teacher@example.test",
		Subject: "Домашнее задание", Attachment: ref, SHA256: strings.Repeat("a", 64), Size: 10,
		MessageID: "<" + id + "@paratech.invalid>", Key: "proposal:" + id, Attempts: 1,
		ConfirmedAt: t0, CreatedAt: t0, UpdatedAt: t0,
	}
	if s == delivery.StatusSent {
		st.SentAt = t0
	}
	if s == delivery.StatusPending {
		st.Attempts = 0
	}
	return st
}

// add stores assignment id deleted at deletedAt (zero: live) with a link of every kind: two proposals, a SENT and a
// FAILED email (one of the text export, one of the copy of a work's file), a draft with three versions, a work, the
// text and Word exports and the copy of the work's file.
func (e *purgeEnv) add(t *testing.T, id shared.AssignmentID, deletedAt time.Time) {
	t.Helper()
	db := e.db
	db.assignments[id] = assignment.State{
		Version: 2, ID: id, SourceIndex: -1, Title: "Задание " + id.String(), CreatedAt: t0, UpdatedAt: t0,
		DeletedAt: deletedAt,
	}
	db.proposals[id] = 2
	text := mustRef(t, shared.ArtifactExports, id.String()+"/homework.md")
	docx := mustRef(t, shared.ArtifactExports, id.String()+"/homework.docx")
	copyRef := mustRef(t, shared.ArtifactExports, "sent/"+id.String()+"/работа.docx")
	db.deliveries[id] = []delivery.State{
		deliveryState("d-"+id.String()+"-1", id, delivery.StatusSent, text),
		deliveryState("d-"+id.String()+"-2", id, delivery.StatusFailed, copyRef),
	}
	db.drafts[id] = 3
	db.works[id] = 1
	for _, ref := range []shared.ArtifactRef{text, docx, copyRef} {
		db.files[ref] = true
	}
}

// links counts what is left of assignment id: its row and every link.
func (e *purgeEnv) links(id shared.AssignmentID) map[string]int {
	db := e.db
	out := map[string]int{
		"proposals": db.proposals[id], "deliveries": len(db.deliveries[id]), "draft versions": db.drafts[id],
		"works": db.works[id],
	}
	if _, ok := db.assignments[id]; ok {
		out["row"] = 1
	}
	for ref := range db.files {
		if fileOwner(ref) == id {
			out["files"]++
		}
	}
	return out
}

func (e *purgeEnv) assertGone(t *testing.T, id shared.AssignmentID) {
	t.Helper()
	for kind, n := range e.links(id) {
		if n != 0 {
			t.Errorf("%s: %d %s left after the purge", id, n, kind)
		}
	}
}

func (e *purgeEnv) assertWhole(t *testing.T, id shared.AssignmentID) {
	t.Helper()
	want := map[string]int{"row": 1, "proposals": 2, "deliveries": 2, "draft versions": 3, "works": 1, "files": 3}
	got := e.links(id)
	for kind, n := range want {
		if got[kind] != n {
			t.Errorf("%s: %s = %d, want %d (untouched)", id, kind, got[kind], n)
		}
	}
}

func TestNewPurgerNeedsEveryPort(t *testing.T) {
	db := newPurgeDB(t)
	full := PurgeDeps{
		Assignments: purgeAssignments{db}, Proposals: purgeProposals{db}, Deliveries: purgeDeliveries{db},
		Drafts: purgeDrafts{db}, Works: purgeWorks{db}, Files: purgeFiles{db}, Clock: purgeClock{purgeNow}, Tx: purgeTx{db},
	}
	if _, err := NewPurger(full); err != nil {
		t.Fatalf("without a log: %v", err)
	}
	for name, clear := range map[string]func(*PurgeDeps){
		"assignments": func(d *PurgeDeps) { d.Assignments = nil },
		"proposals":   func(d *PurgeDeps) { d.Proposals = nil },
		"deliveries":  func(d *PurgeDeps) { d.Deliveries = nil },
		"drafts":      func(d *PurgeDeps) { d.Drafts = nil },
		"works":       func(d *PurgeDeps) { d.Works = nil },
		"files":       func(d *PurgeDeps) { d.Files = nil },
		"clock":       func(d *PurgeDeps) { d.Clock = nil },
		"tx":          func(d *PurgeDeps) { d.Tx = nil },
	} {
		d := full
		clear(&d)
		if _, err := NewPurger(d); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("without %s: err = %v", name, err)
		}
	}
}

// ADR-0029 addendum 2: an assignment deleted longer than the window goes with every link and file; one whose window
// ends exactly now, one deleted an hour ago and a live one keep everything.
func TestPurgeDeletedRemovesEveryLink(t *testing.T) {
	e := newPurgeEnv(t)
	e.add(t, "due", purgeNow.Add(-assignment.RestoreWindow-time.Millisecond))
	e.add(t, "boundary", purgeNow.Add(-assignment.RestoreWindow))
	e.add(t, "recent", purgeNow.Add(-time.Hour))
	e.add(t, "live", time.Time{})
	library := mustRef(t, shared.ArtifactMaterials, strings.Repeat("b", 64))
	e.db.files[library] = true
	e.db.deliveries["due"] = append(e.db.deliveries["due"], deliveryState("d-due-3", "due", delivery.StatusSent, library))

	n, err := e.p.PurgeDeleted(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("PurgeDeleted = %d, %v; want 1, nil", n, err)
	}
	e.assertGone(t, "due")
	for _, id := range []shared.AssignmentID{"boundary", "recent", "live"} {
		e.assertWhole(t, id)
	}
	if !e.db.files[library] {
		t.Fatal("a library file was deleted: only files below exports go")
	}
	want := []string{pstepCheck, pstepDeliveries, pstepProposals, pstepDelivRows, pstepDraft, pstepWorks, pstepAssignment,
		pstepFile, pstepFile, pstepFile}
	if !slices.Equal(e.db.ran, want) {
		t.Fatalf("steps = %v, want %v: the re-check first, the rows, the row last of them, then the files", e.db.ran, want)
	}

	// A second run finds nothing.
	if n, err := e.p.PurgeDeleted(context.Background()); err != nil || n != 0 {
		t.Fatalf("second run = %d, %v", n, err)
	}
}

// An assignment with an email that may still go out or may have gone out unseen is never purged (hard rule 7):
// nothing of it is removed, the run logs it and goes on.
func TestPurgeDeletedSkipsAnUnresolvedDelivery(t *testing.T) {
	for _, status := range []delivery.Status{delivery.StatusPending, delivery.StatusSending, delivery.StatusUnknown} {
		t.Run(string(status), func(t *testing.T) {
			e := newPurgeEnv(t)
			old := purgeNow.Add(-10 * 24 * time.Hour)
			e.add(t, "held", old)
			e.add(t, "other", old)
			ref := mustRef(t, shared.ArtifactExports, "held/homework.md")
			e.db.deliveries["held"] = append(e.db.deliveries["held"], deliveryState("d-held-3", "held", status, ref))

			n, err := e.p.PurgeDeleted(context.Background())
			if err != nil || n != 1 {
				t.Fatalf("PurgeDeleted = %d, %v; want 1 (the other), nil", n, err)
			}
			got := e.links("held")
			want := map[string]int{"row": 1, "proposals": 2, "deliveries": 3, "draft versions": 3, "works": 1, "files": 3}
			for kind, n := range want {
				if got[kind] != n {
					t.Errorf("%s = %d, want %d", kind, got[kind], n)
				}
			}
			e.assertGone(t, "other")
			if line := e.log.String(); !strings.Contains(line, "unresolved") || !strings.Contains(line, "held") {
				t.Fatalf("no log line of the skip: %q", line)
			}
		})
	}
}

// A failure (or a crash) at any step rolls the rows back and leaves the assignment deleted, so still invisible, and the
// next run finishes it: every step is idempotent.
func TestPurgeDeletedFinishesAfterAFailedStep(t *testing.T) {
	for _, step := range []string{pstepCheck, pstepDeliveries, pstepProposals, pstepDelivRows, pstepDraft, pstepWorks,
		pstepAssignment, pstepFile} {
		t.Run(step, func(t *testing.T) {
			e := newPurgeEnv(t)
			deletedAt := purgeNow.Add(-4 * 24 * time.Hour)
			e.add(t, "a1", deletedAt)
			e.db.failStep = step

			n, err := e.p.PurgeDeleted(context.Background())
			if !errors.Is(err, errInjected) || n != 0 {
				t.Fatalf("failed run = %d, %v; want 0 and the failure", n, err)
			}
			if !strings.Contains(err.Error(), "a1") {
				t.Fatalf("the error does not name the assignment: %v", err)
			}
			st, ok := e.db.assignments["a1"]
			if !ok || !st.DeletedAt.Equal(deletedAt) {
				t.Fatalf("after a failed step the row is %+v, %v; want it still deleted", st, ok)
			}
			e.assertWhole(t, "a1") // the rows rolled back; the files go only after the rows

			e.db.failStep = ""
			if n, err := e.p.PurgeDeleted(context.Background()); err != nil || n != 1 {
				t.Fatalf("rerun = %d, %v; want 1, nil", n, err)
			}
			e.assertGone(t, "a1")
		})
	}
}

func TestPurgeDeletedOneFailureDoesNotStopTheOthers(t *testing.T) {
	e := newPurgeEnv(t)
	old := purgeNow.Add(-5 * 24 * time.Hour)
	for _, id := range []shared.AssignmentID{"a1", "a2", "a3"} {
		e.add(t, id, old)
	}
	e.db.failStep, e.db.failFor = pstepDraft, "a2"

	n, err := e.p.PurgeDeleted(context.Background())
	if n != 2 || !errors.Is(err, errInjected) || !strings.Contains(err.Error(), "a2") {
		t.Fatalf("PurgeDeleted = %d, %v; want 2 and the failure of a2", n, err)
	}
	e.assertGone(t, "a1")
	e.assertGone(t, "a3")
	if _, ok := e.db.assignments["a2"]; !ok {
		t.Fatal("the failed assignment lost its row")
	}
}

func TestPurgeDeletedListFailure(t *testing.T) {
	e := newPurgeEnv(t)
	e.add(t, "a1", purgeNow.Add(-5*24*time.Hour))
	e.db.failStep = pstepList
	if n, err := e.p.PurgeDeleted(context.Background()); !errors.Is(err, errInjected) || n != 0 {
		t.Fatalf("PurgeDeleted = %d, %v", n, err)
	}
	e.assertWhole(t, "a1")
}

// An assignment that is no longer due when a step runs (restored after the listing, or deleted again later after the
// clock was set back) keeps its row, every link and every file, whichever step finds it; it is not counted, is no
// error, and the others are purged.
func TestPurgeDeletedKeepsEverythingOfARowNoLongerDue(t *testing.T) {
	changes := map[string]func(st *assignment.State){
		"restored":      func(st *assignment.State) { st.DeletedAt = time.Time{} },
		"deleted later": func(st *assignment.State) { st.DeletedAt = purgeNow.Add(-time.Hour) },
	}
	for name, change := range changes {
		for _, step := range []string{pstepCheck, pstepDeliveries, pstepProposals, pstepDelivRows, pstepDraft, pstepWorks,
			pstepAssignment} {
			t.Run(name+" before "+step, func(t *testing.T) {
				e := newPurgeEnv(t)
				e.add(t, "a1", purgeNow.Add(-5*24*time.Hour))
				e.add(t, "a2", purgeNow.Add(-4*24*time.Hour))
				var want assignment.State
				e.db.before[step] = func(a shared.AssignmentID) {
					if a != "a1" {
						return
					}
					st := e.db.assignments[a]
					change(&st)
					e.db.assignments[a], want = st, st
				}

				n, err := e.p.PurgeDeleted(context.Background())
				if err != nil || n != 1 {
					t.Fatalf("PurgeDeleted = %d, %v; want 1 (a2), nil", n, err)
				}
				e.assertWhole(t, "a1")
				if got := e.db.assignments["a1"]; !got.DeletedAt.Equal(want.DeletedAt) {
					t.Fatalf("a1 deleted at %v, want %v: the change made meanwhile was undone", got.DeletedAt, want.DeletedAt)
				}
				e.assertGone(t, "a2")
			})
		}
	}
}

// The listing goes by keyset pages: a full page of assignments kept for an unresolved email does not hide the later
// ones, and each one is looked at once.
func TestPurgeDeletedReachesPastAPageOfKeptOnes(t *testing.T) {
	e := newPurgeEnv(t)
	old := purgeNow.Add(-20 * 24 * time.Hour)
	held := purgePage + purgePage/2
	for i := range held + 3 {
		id := shared.AssignmentID(fmt.Sprintf("a%03d", i))
		e.add(t, id, old.Add(time.Duration(i)*time.Minute))
		if i < held {
			ref := mustRef(t, shared.ArtifactExports, id.String()+"/homework.md")
			e.db.deliveries[id] = append(e.db.deliveries[id], deliveryState("d-"+id.String()+"-3", id, delivery.StatusUnknown, ref))
		}
	}

	n, err := e.p.PurgeDeleted(context.Background())
	if err != nil || n != 3 {
		t.Fatalf("PurgeDeleted = %d, %v; want the 3 after the kept ones", n, err)
	}
	if e.db.lists != 2 {
		t.Fatalf("ListDeletedBefore called %d times, want 2 pages", e.db.lists)
	}
	if left := len(e.db.assignments); left != held {
		t.Fatalf("%d rows left, want the %d kept", left, held)
	}
	if checks := strings.Count(strings.Join(e.db.ran, ","), pstepCheck); checks != held+3 {
		t.Fatalf("%d re-checks, want each of the %d once", checks, held+3)
	}
}

// One run looks at maxPurgeExamined assignments at most; the next run goes on.
func TestPurgeDeletedExaminesABoundedNumber(t *testing.T) {
	e := newPurgeEnv(t)
	old := purgeNow.Add(-20 * 24 * time.Hour)
	for i := range maxPurgeExamined + 1 {
		id := shared.AssignmentID(fmt.Sprintf("a%05d", i))
		e.db.assignments[id] = assignment.State{
			Version: 2, ID: id, SourceIndex: -1, Title: "Задание", CreatedAt: t0, UpdatedAt: t0, DeletedAt: old,
		}
	}
	if n, err := e.p.PurgeDeleted(context.Background()); err != nil || n != maxPurgeExamined {
		t.Fatalf("first run = %d, %v; want %d", n, err, maxPurgeExamined)
	}
	if n, err := e.p.PurgeDeleted(context.Background()); err != nil || n != 1 {
		t.Fatalf("second run = %d, %v; want 1", n, err)
	}
}

// A cancelled run stops before the next assignment and says so.
func TestPurgeDeletedStopsWhenCancelled(t *testing.T) {
	e := newPurgeEnv(t)
	e.add(t, "a1", purgeNow.Add(-5*24*time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if n, err := e.p.PurgeDeleted(ctx); !errors.Is(err, context.Canceled) || n != 0 {
		t.Fatalf("PurgeDeleted = %d, %v", n, err)
	}
	e.assertWhole(t, "a1")
}

// «Вернуть» after the window is ErrRestoreExpired and changes nothing; exactly at its end it still works.
func TestRestoreAssignmentAfterTheWindow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		after   time.Duration
		wantErr error
	}{
		{"at the end of the window", assignment.RestoreWindow, nil},
		{"after the window", assignment.RestoreWindow + time.Millisecond, assignment.ErrRestoreExpired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newCreateEnv(t, testSubjects)
			ctx := context.Background()
			v, err := e.svc.CreateAssignment(ctx, NewAssignment{Title: "Задание"})
			if err != nil {
				t.Fatal(err)
			}
			d, err := e.svc.DeleteAssignment(ctx, v.ID, v.Version)
			if err != nil {
				t.Fatal(err)
			}
			e.clock.Advance(tc.after)

			_, err = e.svc.RestoreAssignment(ctx, v.ID, d.Version)
			switch {
			case tc.wantErr == nil && err != nil:
				t.Fatalf("RestoreAssignment: %v", err)
			case tc.wantErr != nil && !errors.Is(err, tc.wantErr):
				t.Fatalf("RestoreAssignment: err = %v, want %v", err, tc.wantErr)
			}
			if deleted := !e.assignments[v.ID].DeletedAt.IsZero(); deleted != (tc.wantErr != nil) {
				t.Fatalf("deleted after the restore = %v", deleted)
			}
		})
	}
}

// The export files the purge deletes are those of the core's derivation (proposal.ExportRefOf).
func TestPurgeDeletesTheExportFiles(t *testing.T) {
	e := newPurgeEnv(t)
	e.add(t, "a1", purgeNow.Add(-5*24*time.Hour))
	e.db.deliveries["a1"] = nil // the files are deleted without any email too
	for _, f := range []proposal.Format{proposal.FormatText, proposal.FormatDocx} {
		ref, err := proposal.ExportRefOf("a1", f)
		if err != nil {
			t.Fatal(err)
		}
		if !e.db.files[ref] {
			t.Fatalf("fixture lacks %s", ref)
		}
	}
	if n, err := e.p.PurgeDeleted(context.Background()); err != nil || n != 1 {
		t.Fatalf("PurgeDeleted = %d, %v", n, err)
	}
	for ref := range e.db.files {
		if fileOwner(ref) == "a1" && !strings.HasPrefix(ref.Path(), "sent/") {
			t.Fatalf("export %s left", ref)
		}
	}
}
