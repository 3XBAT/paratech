package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

var lesT0 = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

func newTestLesson(t *testing.T, id, uid string, startHour int) *lesson.Lesson {
	t.Helper()
	start := lesT0.Add(time.Duration(startHour) * time.Hour)
	l, err := lesson.New(lesson.Spec{
		ID: shared.LessonID(id), SourceUID: uid,
		Content: lesson.Content{Title: "Lesson " + id, Start: start, End: start.Add(90 * time.Minute), TimeZone: "Europe/Moscow"},
	}, lesT0)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func lessonIDs(ls []*lesson.Lesson) []shared.LessonID {
	out := make([]shared.LessonID, len(ls))
	for i, l := range ls {
		out[i] = l.ID()
	}
	return out
}

func sameIDs(got []*lesson.Lesson, want ...shared.LessonID) bool {
	g := lessonIDs(got)
	if len(g) != len(want) {
		return false
	}
	for i := range g {
		if g[i] != want[i] {
			return false
		}
	}
	return true
}

func TestLessonSaveRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := NewLessonRepository(openTest(t, nil))

	l := newTestLesson(t, "l1", "csv:id:1", 0)
	if err := repo.Save(ctx, l); err != nil {
		t.Fatal(err)
	}
	if l.Version() != 1 {
		t.Fatalf("version after insert = %d, want 1", l.Version())
	}
	got, err := repo.Get(ctx, "l1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Content().Equal(l.Content()) || got.SourceUID() != "csv:id:1" || got.Version() != 1 ||
		got.ContentRevision() != 1 || got.CalendarSyncedRevision() != 0 || !got.CreatedAt().Equal(lesT0) || got.Deleted() {
		t.Fatalf("round trip lost fields: %+v", got)
	}

	manual := newTestLesson(t, "manual", "", 3)
	if err := repo.Save(ctx, manual); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.Get(ctx, "manual"); err != nil || got.SourceUID() != "" {
		t.Fatalf("manual lesson: %v %+v", err, got)
	}

	if _, err := repo.Get(ctx, "missing"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get(missing) = %v, want ErrNotFound", err)
	}
}

func TestLessonSaveVersionProtocol(t *testing.T) {
	ctx := context.Background()
	repo := NewLessonRepository(openTest(t, nil))
	l := newTestLesson(t, "l1", "csv:id:1", 0)
	if err := repo.Save(ctx, l); err != nil {
		t.Fatal(err)
	}
	stale, err := repo.Get(ctx, "l1")
	if err != nil {
		t.Fatal(err)
	}

	c := l.Content()
	c.Title = "Changed"
	if _, err := l.ChangeContent(c, lesT0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	for want := int64(2); want <= 3; want++ {
		if err := repo.Save(ctx, l); err != nil || l.Version() != want {
			t.Fatalf("save of the same instance: %v, version %d want %d", err, l.Version(), want)
		}
	}
	if err := repo.Save(ctx, stale); !errors.Is(err, shared.ErrVersionConflict) || stale.Version() != 1 {
		t.Fatalf("stale save = %v (version %d), want ErrVersionConflict", err, stale.Version())
	}
	if err := repo.Save(ctx, newTestLesson(t, "l1", "", 5)); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("new lesson with an existing id = %v, want ErrVersionConflict", err)
	}
	if err := repo.Save(ctx, newTestLesson(t, "l2", "csv:id:1", 5)); !errors.Is(err, shared.ErrDuplicate) {
		t.Fatalf("second live lesson with the same source uid = %v, want ErrDuplicate", err)
	}
	if got, _ := repo.Get(ctx, "l1"); got.Content().Title != "Changed" || got.ContentRevision() != 2 {
		t.Fatalf("stored = %+v", got)
	}
}

func TestLessonSoftDelete(t *testing.T) {
	ctx := context.Background()
	db := openTest(t, nil)
	repo := NewLessonRepository(db)
	l := newTestLesson(t, "l1", "csv:id:1", 0)
	if err := repo.Save(ctx, l); err != nil {
		t.Fatal(err)
	}
	if err := l.MarkDeleted(lesT0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := repo.ApplyChanges(ctx, nil, []*lesson.Lesson{l}); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Get(ctx, "l1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get of a deleted lesson = %v, want ErrNotFound", err)
	}
	var n int
	if err := db.sql.QueryRowContext(ctx, "SELECT count(*) FROM schedule_entries WHERE id = 'l1' AND deleted_at IS NOT NULL").Scan(&n); err != nil || n != 1 {
		t.Fatalf("soft-deleted row: count %d, %v", n, err)
	}

	// The deleted occurrence does not block the same uid on a new live row.
	if err := repo.Save(ctx, newTestLesson(t, "l2", "csv:id:1", 0)); err != nil {
		t.Fatalf("re-import of a deleted occurrence: %v", err)
	}
}

func TestLessonApplyChangesIsAtomic(t *testing.T) {
	ctx := context.Background()
	repo := NewLessonRepository(openTest(t, nil))
	keep := newTestLesson(t, "keep", "csv:k", 0)
	if err := repo.Save(ctx, keep); err != nil {
		t.Fatal(err)
	}
	stale, err := repo.Get(ctx, "keep")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, keep); err != nil { // moves the row to version 2
		t.Fatal(err)
	}

	added := newTestLesson(t, "new", "csv:n", 2)
	err = repo.ApplyChanges(ctx, []*lesson.Lesson{added, stale}, nil)
	if !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("ApplyChanges = %v, want ErrVersionConflict", err)
	}
	if _, err := repo.Get(ctx, "new"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("a failed batch inserted a lesson: %v", err)
	}
	if added.Version() != 0 || stale.Version() != 1 {
		t.Fatalf("Persisted called after a failed batch: %d, %d", added.Version(), stale.Version())
	}
}

func TestLessonApplyChangesJoinsOuterTx(t *testing.T) {
	ctx := context.Background()
	db := openTest(t, nil)
	repo := NewLessonRepository(db)
	boom := errors.New("boom")

	err := db.Do(ctx, func(ctx context.Context) error {
		if err := repo.ApplyChanges(ctx, []*lesson.Lesson{newTestLesson(t, "l1", "", 0)}, nil); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Do = %v", err)
	}
	if _, err := repo.Get(ctx, "l1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("the outer rollback did not undo ApplyChanges: %v", err)
	}
}

func TestLessonApplyChangesRejectsBadBatches(t *testing.T) {
	ctx := context.Background()
	repo := NewLessonRepository(openTest(t, nil))
	l := newTestLesson(t, "l1", "", 0)
	tests := []struct {
		name             string
		upserts, deleted []*lesson.Lesson
	}{
		{"nil lesson", []*lesson.Lesson{nil}, nil},
		{"twice", []*lesson.Lesson{l}, []*lesson.Lesson{l}},
		{"deleted but live", nil, []*lesson.Lesson{l}},
	}
	for _, tt := range tests {
		if err := repo.ApplyChanges(ctx, tt.upserts, tt.deleted); err == nil {
			t.Errorf("%s: ApplyChanges accepted the batch", tt.name)
		}
	}
	if l.Version() != 0 {
		t.Fatal("a rejected batch stored a lesson")
	}
	if err := repo.ApplyChanges(ctx, nil, nil); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
}

func TestLessonLists(t *testing.T) {
	ctx := context.Background()
	db := openTest(t, nil)
	repo := NewLessonRepository(db)
	for _, l := range []*lesson.Lesson{
		newTestLesson(t, "c", "", 4), newTestLesson(t, "a", "", 0), newTestLesson(t, "b", "", 2), newTestLesson(t, "d", "", 6),
	} {
		if err := repo.Save(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	gone, err := repo.Get(ctx, "d")
	if err != nil {
		t.Fatal(err)
	}
	if err := gone.MarkDeleted(lesT0); err != nil {
		t.Fatal(err)
	}
	if err := repo.ApplyChanges(ctx, nil, []*lesson.Lesson{gone}); err != nil {
		t.Fatal(err)
	}

	live, err := repo.ListLive(ctx)
	if err != nil || !sameIDs(live, "a", "b", "c") {
		t.Fatalf("ListLive = %v, %v", lessonIDs(live), err)
	}
	between, err := repo.ListStartingBetween(ctx, lesT0, lesT0.Add(4*time.Hour))
	if err != nil || !sameIDs(between, "a", "b") {
		t.Fatalf("ListStartingBetween = %v, %v (end is exclusive)", lessonIDs(between), err)
	}
	if all, _ := repo.ListStartingBetween(ctx, lesT0, lesT0.Add(24*time.Hour)); !sameIDs(all, "a", "b", "c") {
		t.Fatalf("ListStartingBetween returned deleted lessons: %v", lessonIDs(all))
	}

	// a: synced at its revision (not pending); b: synced at an older revision; c: never synced; d: deleted.
	mustExec(t, db, "UPDATE schedule_entries SET calendar_synced_revision = 1 WHERE id = 'a'")
	mustExec(t, db, "UPDATE schedule_entries SET content_revision = 2, calendar_synced_revision = 1 WHERE id = 'b'")
	pending, err := repo.ListPendingCalendarSync(ctx, 10)
	if err != nil || !sameIDs(pending, "b", "c", "d") {
		t.Fatalf("ListPendingCalendarSync = %v, %v", lessonIDs(pending), err)
	}
	for _, l := range pending {
		if !l.CalendarPending() {
			t.Errorf("lesson %s is listed as pending but the aggregate disagrees", l.ID())
		}
	}
	if a, _ := repo.Get(ctx, "a"); a.CalendarPending() {
		t.Error("lesson a is not listed but the aggregate says pending")
	}
	if two, _ := repo.ListPendingCalendarSync(ctx, 2); !sameIDs(two, "b", "c") {
		t.Fatalf("limit 2 = %v", lessonIDs(two))
	}
	if none, err := repo.ListPendingCalendarSync(ctx, 0); err != nil || len(none) != 0 {
		t.Fatalf("limit 0 = %v, %v", lessonIDs(none), err)
	}
}

func TestLessonPurgeDeleted(t *testing.T) {
	ctx := context.Background()
	repo := NewLessonRepository(openTest(t, nil))
	live, gone := newTestLesson(t, "live", "", 0), newTestLesson(t, "gone", "", 2)
	if err := repo.ApplyChanges(ctx, []*lesson.Lesson{live, gone}, nil); err != nil {
		t.Fatal(err)
	}
	if err := gone.MarkDeleted(lesT0); err != nil {
		t.Fatal(err)
	}
	if err := repo.ApplyChanges(ctx, nil, []*lesson.Lesson{gone}); err != nil {
		t.Fatal(err)
	}

	if err := repo.PurgeDeleted(ctx, "live"); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("purge of a live lesson = %v, want ErrConflict", err)
	}
	if _, err := repo.Get(ctx, "live"); err != nil {
		t.Fatalf("a refused purge removed the lesson: %v", err)
	}
	for i := 0; i < 2; i++ { // idempotent
		if err := repo.PurgeDeleted(ctx, "gone"); err != nil {
			t.Fatalf("purge #%d: %v", i+1, err)
		}
	}
	if pending, _ := repo.ListPendingCalendarSync(ctx, 10); !sameIDs(pending, "live") {
		t.Fatalf("purged lesson still listed: %v", lessonIDs(pending))
	}
	if err := repo.Save(ctx, gone); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("save of a purged lesson = %v, want ErrVersionConflict (never re-inserted)", err)
	}
}

// Subject, kind and teacher survive a save and a change, for every kind.
func TestLessonDetailsRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := NewLessonRepository(openTest(t, nil))

	for i, kind := range lesson.Kinds() {
		l := newTestLesson(t, "l"+kind.String(), "csv:id:"+kind.String(), i*2)
		c := l.Content()
		c.Subject, c.Kind, c.Teacher = "Матанализ", kind, "Иванов И. И."
		if _, err := l.ChangeContent(c, lesT0); err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, l); err != nil {
			t.Fatalf("%v: %v", kind, err)
		}
		got, err := repo.Get(ctx, l.ID())
		if err != nil || !got.Content().Equal(c) {
			t.Fatalf("%v: got %+v, %v; want %+v", kind, got.Content(), err, c)
		}

		c.Teacher = ""
		if _, err := got.ChangeContent(c, lesT0); err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, got); err != nil {
			t.Fatal(err)
		}
		if again, err := repo.Get(ctx, l.ID()); err != nil || !again.Content().Equal(c) {
			t.Fatalf("%v after update: got %+v, %v; want %+v", kind, again.Content(), err, c)
		}
	}
}

// The CHECK of schedule_entries.kind holds exactly the codes of lesson.Kinds; an unreadable stored kind fails the
// load instead of becoming unknown.
func TestLessonKindColumn(t *testing.T) {
	ctx := context.Background()
	db := openTest(t, nil)
	const insert = `INSERT INTO schedule_entries (id, title, starts_at, ends_at, timezone, kind, created_at, updated_at)
		VALUES (?, 'math', 0, 10, 'UTC', ?, 0, 0)`
	for _, k := range lesson.Kinds() {
		mustExec(t, db, insert, "l-"+k.String(), k.String())
	}
	if _, err := db.sql.ExecContext(ctx, insert, "l-bad", "exam"); !errors.Is(MapError(ctx, err), ErrConstraint) {
		t.Fatalf("unknown kind code: err = %v, want ErrConstraint", err)
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE schedule_entries SET teacher = ? WHERE id = 'l-lab'`,
		strings.Repeat("я", lesson.MaxTeacherRunes+1)); !errors.Is(MapError(ctx, err), ErrConstraint) {
		t.Fatalf("long teacher: err = %v, want ErrConstraint", err)
	}

	mustExec(t, db, `PRAGMA ignore_check_constraints = 1`)
	mustExec(t, db, `UPDATE schedule_entries SET kind = 'exam' WHERE id = 'l-lab'`)
	if _, err := NewLessonRepository(db).Get(ctx, "l-lab"); !errors.Is(err, lesson.ErrInvalidLesson) {
		t.Fatalf("stored unknown kind code: err = %v, want ErrInvalidLesson", err)
	}
}

// Version 13 adds subject, kind and teacher: a lesson stored before reads as unknown with no subject or teacher, and
// the migration goes back down keeping it.
func TestMigrationToVersion13AddsLessonDetails(t *testing.T) {
	ctx := context.Background()
	db := openTest(t, func(o *Options) { o.Migrate = false })
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 12); err != nil {
		t.Fatalf("up to 12: %v", err)
	}
	mustExec(t, db, `INSERT INTO schedule_entries (id, title, starts_at, ends_at, timezone, source_uid, version,
		created_at, updated_at) VALUES ('l-old', 'Матанализ, лекция', 0, 5400000, 'Europe/Moscow', 'csv:id:1', 1, 0, 0)`)

	if _, err := p.UpTo(ctx, 13); err != nil {
		t.Fatalf("up to 13: %v", err)
	}
	l, err := NewLessonRepository(db).Get(ctx, "l-old")
	if err != nil {
		t.Fatal(err)
	}
	if c := l.Content(); c.Title != "Матанализ, лекция" || c.Subject != "" || c.Kind != lesson.KindUnknown || c.Teacher != "" {
		t.Fatalf("existing lesson = %+v; want no subject, unknown kind, no teacher", c)
	}

	if _, err := p.DownTo(ctx, 12); err != nil {
		t.Fatalf("down to 12: %v", err)
	}
	const detailColumns = "SELECT count(*) FROM pragma_table_info('schedule_entries') WHERE name IN ('subject', 'kind', 'teacher')"
	if n := scanInt(t, db.sql, detailColumns); n != 0 {
		t.Fatalf("%d detail columns still exist at version 12", n)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM schedule_entries"); n != 1 {
		t.Fatalf("lessons = %d, want the lesson kept", n)
	}
}
