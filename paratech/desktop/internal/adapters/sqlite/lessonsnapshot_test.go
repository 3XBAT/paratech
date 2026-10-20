package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

func testLessonSnapshot() values.LessonSnapshot {
	return values.NewLessonSnapshot(values.LessonSnapshotSpec{
		Title: "Тестовый урок", Subject: "Матанализ", Kind: "lecture", Teacher: "Иванов И. И.",
		Start: lesT0, End: lesT0.Add(90 * time.Minute),
	})
}

func snapshotRecording(t *testing.T, id string, lessonID shared.LessonID, snap values.LessonSnapshot) *recording.Recording {
	t.Helper()
	r, err := recording.New(recording.Spec{
		ID: shared.RecordingID(id), LessonID: lessonID, Lesson: snap, MaxDuration: 3 * time.Hour,
	}, recT0)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The snapshot is stored with the recording, survives later saves and the soft deletion of the lesson by an import
// (TASK-088); a recording without one stores NULL columns and restores the zero snapshot.
func TestRecordingLessonSnapshotRoundTrip(t *testing.T) {
	db := openTest(t, nil)
	lessons, repo, ctx := NewLessonRepository(db), NewRecordingRepository(db), context.Background()
	l := newTestLesson(t, "l1", "uid-1", 0)
	if err := lessons.Save(ctx, l); err != nil {
		t.Fatal(err)
	}

	with := snapshotRecording(t, "r-snap", "l1", testLessonSnapshot())
	saveRecording(t, repo, with)
	without := snapshotRecording(t, "r-plain", "l1", values.LessonSnapshot{})
	saveRecording(t, repo, without)
	adhoc := snapshotRecording(t, "r-adhoc", "", values.LessonSnapshot{})
	saveRecording(t, repo, adhoc)

	for _, want := range []*recording.Recording{with, without, adhoc} {
		got, err := repo.Get(ctx, want.ID())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round trip differs:\n got  %+v\n want %+v", got, want)
		}
	}
	for _, col := range []string{"lesson_title", "lesson_subject", "lesson_kind", "lesson_teacher", "lesson_start", "lesson_end"} {
		if n := scanInt(t, db.sql, "SELECT count(*) FROM recordings WHERE "+col+" IS NULL"); n != 2 {
			t.Errorf("%s is NULL in %d rows; want the 2 without a snapshot", col, n)
		}
	}

	// A save after the start does not touch the snapshot; neither does the import that deletes the lesson.
	if err := with.Begin(recT0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	saveRecording(t, repo, with)
	if err := l.MarkDeleted(lesT0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := lessons.ApplyChanges(ctx, nil, []*lesson.Lesson{l}); err != nil {
		t.Fatal(err)
	}
	if _, err := lessons.Get(ctx, "l1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("deleted lesson Get = %v; want ErrNotFound", err)
	}
	got, err := repo.Get(ctx, "r-snap")
	if err != nil {
		t.Fatal(err)
	}
	if !got.LessonSnapshot().Equal(testLessonSnapshot()) || got.LessonID() != "l1" {
		t.Fatalf("after the lesson was deleted: lesson %q, snapshot %+v", got.LessonID(), got.LessonSnapshot())
	}
}

// Stored text is cleaned on the way back like new text: the database is not trusted to hold safe text.
func TestRecordingLessonSnapshotCleanedOnRead(t *testing.T) {
	db := openTest(t, nil)
	repo, ctx := NewRecordingRepository(db), context.Background()
	saveRecording(t, repo, newTestRecording(t, "r1", ""))
	mustExec(t, db, "UPDATE recordings SET lesson_title = 'Урок'||char(10)||'1'||char(8238), lesson_kind = 'не код' WHERE id = 'r1'")

	got, err := repo.Get(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if s := got.LessonSnapshot(); s.Title() != "Урок 1" || s.Kind() != "" {
		t.Fatalf("snapshot = %+v", s)
	}
}

// Version 13 adds the snapshot columns, NULL for the existing rows, and goes back down keeping the rows.
func TestMigrationToVersion13AddsLessonSnapshot(t *testing.T) {
	ctx := context.Background()
	db := openTest(t, func(o *Options) { o.Migrate = false })
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 12); err != nil {
		t.Fatalf("up to 12: %v", err)
	}
	insertOldRecording(t, db, "r-old")

	if _, err := p.UpTo(ctx, 13); err != nil {
		t.Fatalf("up to 13: %v", err)
	}
	// Raw SQL: the repository reads the columns of later migrations too.
	if n := scanInt(t, db.sql, `SELECT count(*) FROM recordings WHERE id = 'r-old' AND lesson_title IS NULL
		AND lesson_subject IS NULL AND lesson_kind IS NULL AND lesson_teacher IS NULL AND lesson_start IS NULL
		AND lesson_end IS NULL`); n != 1 {
		t.Fatalf("existing recording with an empty snapshot = %d, want 1", n)
	}

	if _, err := p.DownTo(ctx, 12); err != nil {
		t.Fatalf("down to 12: %v", err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM pragma_table_info('recordings') WHERE name LIKE 'lesson_%' AND name <> 'lesson_id'"); n != 0 {
		t.Fatalf("%d snapshot columns still exist at version 12", n)
	}
	if n := countRecordings(t, db); n != 1 {
		t.Fatalf("recordings = %d, want the recording kept", n)
	}
}
