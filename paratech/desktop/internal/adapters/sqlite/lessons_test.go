package sqlite

import (
	"context"
	"errors"
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
