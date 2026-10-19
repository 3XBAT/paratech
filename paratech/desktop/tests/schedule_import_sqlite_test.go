package tests

// Scenario tests of the schedule import (PRD sections 11.2 and 14 case 9, AC-16) on the real SQLite adapter, the real
// CSV parser and the real use case; only the clock and the id generator are fakes (ARCH-61).

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/csvschedule"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/schedule/services/importplan"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

var (
	_ schedulesync.LessonRepository = (*sqlite.LessonRepository)(nil)
	_ schedulesync.Tx               = (*sqlite.DB)(nil)
	_ schedulesync.ScheduleParser   = (*csvschedule.Parser)(nil)
	_ schedulesync.Clock            = (*testenv.FakeClock)(nil)
)

var schedT0 = time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)

type lessonIDGen struct {
	mu sync.Mutex
	n  int
}

func (g *lessonIDGen) NewLessonID() shared.LessonID {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	return shared.LessonID(fmt.Sprintf("lesson-%d", g.n))
}

func (g *lessonIDGen) NewJobID() shared.JobID { return "job-unused" }

type scheduleEnv struct {
	t     *testing.T
	repo  *sqlite.LessonRepository
	clock *testenv.FakeClock
	svc   *schedulesync.Service
}

func newScheduleEnv(t *testing.T) *scheduleEnv {
	t.Helper()
	db, err := sqlite.Open(context.Background(), sqlite.Options{Path: filepath.Join(t.TempDir(), "app.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	e := &scheduleEnv{t: t, repo: sqlite.NewLessonRepository(db), clock: testenv.NewFakeClock(schedT0)}
	e.svc, err = schedulesync.New(schedulesync.Deps{
		Lessons: e.repo,
		Parsers: map[importplan.Source]schedulesync.ScheduleParser{importplan.SourceCSV: csvschedule.New()},
		Clock:   e.clock, IDs: &lessonIDGen{}, Tx: db,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *scheduleEnv) preview(file string) schedulesync.Preview {
	e.t.Helper()
	p, err := e.svc.PreviewImport(context.Background(), schedulesync.ImportRequest{Source: importplan.SourceCSV, Data: []byte(file)})
	if err != nil {
		e.t.Fatalf("PreviewImport: %v", err)
	}
	return p
}

func (e *scheduleEnv) commit(file, token string) (schedulesync.CommitResult, error) {
	e.clock.Advance(time.Minute)
	return e.svc.CommitImport(context.Background(), schedulesync.CommitRequest{
		Source: importplan.SourceCSV, Data: []byte(file), ConfirmToken: token,
	})
}

func (e *scheduleEnv) importFile(file string) schedulesync.CommitResult {
	e.t.Helper()
	p := e.preview(file)
	if !p.Plan.Valid() {
		e.t.Fatalf("preview errors: %+v", p.Plan.Errors)
	}
	res, err := e.commit(file, p.ConfirmToken)
	if err != nil {
		e.t.Fatalf("CommitImport: %v", err)
	}
	return res
}

// lessonRow is the stored state of a lesson as the scenario compares it.
type lessonRow struct {
	ID       shared.LessonID
	UID      string
	Title    string
	Start    time.Time
	End      time.Time
	Zone     string
	Subject  string
	Kind     lesson.Kind
	Teacher  string
	Revision int64
	Version  int64
	Deleted  bool
}

// schedule returns every stored lesson, deleted ones included. Nothing is synchronized to a calendar in these tests,
// so every row is pending and ListPendingCalendarSync lists them all.
func (e *scheduleEnv) schedule() map[shared.LessonID]lessonRow {
	e.t.Helper()
	all, err := e.repo.ListPendingCalendarSync(context.Background(), 1000)
	if err != nil {
		e.t.Fatal(err)
	}
	out := map[shared.LessonID]lessonRow{}
	for _, l := range all {
		c := l.Content()
		out[l.ID()] = lessonRow{
			ID: l.ID(), UID: l.SourceUID(), Title: c.Title, Start: c.Start.UTC(), End: c.End.UTC(), Zone: c.TimeZone,
			Subject: c.Subject, Kind: c.Kind, Teacher: c.Teacher,
			Revision: l.ContentRevision(), Version: l.Version(), Deleted: l.Deleted(),
		}
	}
	return out
}

func (e *scheduleEnv) byUID(uid string) lessonRow {
	e.t.Helper()
	for _, r := range e.schedule() {
		if r.UID == uid && !r.Deleted {
			return r
		}
	}
	e.t.Fatalf("no live lesson with uid %q", uid)
	return lessonRow{}
}

const semester = `title,date,start,end,timezone,uid
Math,2026-10-05,09:00,10:30,Europe/Moscow,math-1
Physics,2026-10-05,10:45,12:15,Europe/Moscow,phys-1
History,2026-10-06,09:00,10:30,Europe/Moscow,hist-1
`

func TestScheduleImportValidFile(t *testing.T) {
	e := newScheduleEnv(t)

	p := e.preview(semester)
	if len(p.Plan.Added) != 3 || len(p.Plan.Changed)+len(p.Plan.Removed)+len(p.Plan.Conflicts) != 0 {
		t.Fatalf("preview of a new schedule = %+v", p.Plan)
	}
	if got := e.schedule(); len(got) != 0 {
		t.Fatalf("preview stored lessons: %+v", got)
	}

	res, err := e.commit(semester, p.ConfirmToken)
	if err != nil {
		t.Fatal(err)
	}
	if res != (schedulesync.CommitResult{Added: 3}) {
		t.Fatalf("commit = %+v", res)
	}
	math := e.byUID("csv:id:math-1")
	wantStart := time.Date(2026, 10, 5, 6, 0, 0, 0, time.UTC) // 09:00 Moscow
	if math.Title != "Math" || !math.Start.Equal(wantStart) || !math.End.Equal(wantStart.Add(90*time.Minute)) ||
		math.Zone != "Europe/Moscow" || math.Version != 1 || math.Revision != 1 {
		t.Fatalf("stored lesson = %+v", math)
	}

	// Re-importing the same file is a no-op: the preview shows no change and the commit writes nothing.
	before := e.schedule()
	again := e.preview(semester)
	if again.Plan.HasChanges() || len(again.Plan.Unchanged) != 3 {
		t.Fatalf("re-import preview = %+v", again.Plan)
	}
	if res, err := e.commit(semester, again.ConfirmToken); err != nil || res != (schedulesync.CommitResult{}) {
		t.Fatalf("re-import commit = %+v, %v", res, err)
	}
	if after := e.schedule(); !reflect.DeepEqual(before, after) {
		t.Fatalf("re-import changed the schedule:\nbefore %+v\nafter  %+v", before, after)
	}
}

// Test step 2: a file with an error in row 3 does not change the schedule.
func TestScheduleImportErrorInRow3LeavesScheduleUnchanged(t *testing.T) {
	e := newScheduleEnv(t)
	e.importFile(semester)
	before := e.schedule()

	const broken = `title,date,start,end,timezone,uid
Math,2026-10-05,09:30,11:00,Europe/Moscow,math-1
Physics,2026-10-05,12:15,10:45,Europe/Moscow,phys-1
Chemistry,2026-10-07,09:00,10:30,Europe/Moscow,chem-1
`
	p := e.preview(broken)
	if p.Plan.Valid() || len(p.Plan.Errors) != 1 || p.Plan.Errors[0].Line != 3 || p.Plan.Errors[0].Field != "end" {
		t.Fatalf("preview errors = %+v, want one error on line 3 (end)", p.Plan.Errors)
	}
	if p.ConfirmToken != "" || p.Plan.HasChanges() {
		t.Fatalf("a preview with errors offers a commit: %+v", p)
	}

	valid := e.preview(semester).ConfirmToken
	for _, token := range []string{"", "forged", valid} {
		_, err := e.commit(broken, token)
		want := schedulesync.ErrInvalidImport
		if token == "" {
			want = schedulesync.ErrNotConfirmed
		}
		if !errors.Is(err, want) {
			t.Fatalf("commit with token %q = %v, want %v", token, err, want)
		}
	}
	if after := e.schedule(); !reflect.DeepEqual(before, after) {
		t.Fatalf("a file with an error changed the schedule:\nbefore %+v\nafter  %+v", before, after)
	}

	// An unreadable file is refused as a whole, too.
	_, err := e.svc.PreviewImport(context.Background(), schedulesync.ImportRequest{Source: importplan.SourceCSV, Data: []byte("title,date\n")})
	if !errors.Is(err, importplan.ErrUnreadableFile) {
		t.Fatalf("preview of a file without the required columns = %v", err)
	}
	if after := e.schedule(); !reflect.DeepEqual(before, after) {
		t.Fatal("an unreadable file changed the schedule")
	}
}

// Test step 3: the preview shows added, changed and conflicting lessons before the commit; removal touches only the
// lessons of the CSV source.
func TestScheduleImportPreviewShowsChangesAndConflicts(t *testing.T) {
	ctx := context.Background()
	e := newScheduleEnv(t)
	e.importFile(semester)

	msk, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	other := func(id, uid, title string, day, hour int) {
		start := time.Date(2026, 10, day, hour, 0, 0, 0, msk)
		l, err := lesson.New(lesson.Spec{ID: shared.LessonID(id), SourceUID: uid,
			Content: lesson.Content{Title: title, Start: start, End: start.Add(time.Hour), TimeZone: "Europe/Moscow"}}, schedT0)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.repo.Save(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	other("manual-1", "", "Tutor", 7, 9)                // a manual lesson
	other("ics-1", "ics:abc#20261008", "Seminar", 8, 9) // a lesson of another source
	before := e.schedule()

	const next = `title,date,start,end,timezone,uid
Math,2026-10-05,09:00,10:30,Europe/Moscow,math-1
Physics,2026-10-05,11:00,12:30,Europe/Moscow,phys-1
Chemistry,2026-10-07,09:30,11:00,Europe/Moscow,chem-1
Lab,2026-10-09,09:00,11:00,Europe/Moscow,lab-1
Lab review,2026-10-09,10:00,11:30,Europe/Moscow,lab-2
`
	p := e.preview(next)
	plan := p.Plan
	if !plan.Valid() || p.ConfirmToken == "" {
		t.Fatalf("preview errors: %+v", plan.Errors)
	}
	if len(plan.Added) != 3 || plan.Added[0].UID != "csv:id:chem-1" || plan.Added[1].UID != "csv:id:lab-1" || plan.Added[2].UID != "csv:id:lab-2" {
		t.Errorf("added = %+v", plan.Added)
	}
	if len(plan.Changed) != 1 || plan.Changed[0].UID != "csv:id:phys-1" || plan.Changed[0].Before.Start.Equal(plan.Changed[0].After.Start) {
		t.Errorf("changed = %+v", plan.Changed)
	}
	if len(plan.Unchanged) != 1 || plan.Unchanged[0].UID != "csv:id:math-1" {
		t.Errorf("unchanged = %+v", plan.Unchanged)
	}
	if len(plan.Removed) != 1 || plan.Removed[0].UID != "csv:id:hist-1" {
		t.Errorf("removed = %+v (manual and ICS lessons must never be removed)", plan.Removed)
	}
	if len(plan.Conflicts) != 2 ||
		plan.Conflicts[0].Line != 4 || plan.Conflicts[0].WithLesson != "manual-1" ||
		plan.Conflicts[1].Line != 5 || plan.Conflicts[1].WithLine != 6 {
		t.Errorf("conflicts = %+v", plan.Conflicts)
	}
	if after := e.schedule(); !reflect.DeepEqual(before, after) {
		t.Fatal("the preview changed the schedule")
	}

	res, err := e.commit(next, p.ConfirmToken)
	if err != nil {
		t.Fatal(err)
	}
	if res != (schedulesync.CommitResult{Added: 3, Changed: 1, Removed: 1}) {
		t.Fatalf("commit = %+v", res)
	}
	after := e.schedule()
	hist := before[e.idOf(before, "csv:id:hist-1")]
	if got := after[hist.ID]; !got.Deleted || got.Revision != 2 {
		t.Errorf("removed lesson = %+v, want soft-deleted with a new revision", got)
	}
	for _, id := range []shared.LessonID{"manual-1", "ics-1"} {
		if !reflect.DeepEqual(after[id], before[id]) {
			t.Errorf("lesson %s of another source changed: %+v", id, after[id])
		}
	}
	if phys := e.byUID("csv:id:phys-1"); !phys.Start.Equal(time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC)) || phys.Revision != 2 {
		t.Errorf("changed lesson = %+v", phys)
	}
	if len(after) != len(before)+3 {
		t.Errorf("%d rows after the commit, want %d (soft delete keeps the row)", len(after), len(before)+3)
	}
}

// A preview confirmed before the schedule changed is not committed.
func TestScheduleImportStalePreviewIsRefused(t *testing.T) {
	e := newScheduleEnv(t)
	e.importFile(semester)
	const next = `title,date,start,end,timezone,uid
Math,2026-10-05,09:00,10:30,Europe/Moscow,math-1
`
	p := e.preview(next)
	e.importFile(semester + "Extra,2026-10-10,09:00,10:00,UTC,extra-1\n")
	before := e.schedule()

	if _, err := e.commit(next, p.ConfirmToken); !errors.Is(err, schedulesync.ErrStalePreview) {
		t.Fatalf("commit of a stale preview = %v, want ErrStalePreview", err)
	}
	if after := e.schedule(); !reflect.DeepEqual(before, after) {
		t.Fatal("a stale preview changed the schedule")
	}
}

func (e *scheduleEnv) idOf(rows map[shared.LessonID]lessonRow, uid string) shared.LessonID {
	e.t.Helper()
	for id, r := range rows {
		if r.UID == uid {
			return id
		}
	}
	e.t.Fatalf("no lesson with uid %q", uid)
	return ""
}
