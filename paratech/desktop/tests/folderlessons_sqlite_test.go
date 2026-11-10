package tests

// Scenario test of the recorded lessons in the folders of their subject (TASK-429, ADR-0034 amendment 2026-11-10) on
// the real stack: SQLite, the library and the knowledge base. A recorded lecture of «Матанализ» is a row of its
// «Лекции»; corrected to a seminar it moves to «Семинары»; marked for deletion it is in no folder. Nothing is stored
// for the rows and the schema stays as it was.

import (
	"context"
	"path/filepath"
	"testing"

	"paratech/desktop/internal/adapters/doctext"
	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/random"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/adapters/textfile"
	"paratech/desktop/internal/cases/knowledgebase"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

func TestFolderLessonsScenario(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := openSQLite(ctx, t, sqlite.Options{Path: filepath.Join(dir, "folders.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := filestore.New(filepath.Join(dir, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	version, err := db.SchemaVersion(ctx)
	if err != nil || version == 0 {
		t.Fatalf("SchemaVersion = %d, %v", version, err)
	}

	clock := testenv.NewFakeClock(libScenarioT0)
	lessons, recordings, noteRepo := sqlite.NewLessonRepository(db), sqlite.NewRecordingRepository(db), sqlite.NewNoteRepository(db)
	lib, err := library.New(library.Deps{
		Subjects: sqlite.NewSubjectRepository(db), Materials: sqlite.NewMaterialRepository(db), Blobs: store,
		Lessons: lessons, Strings: sqlite.NewSubjectStrings(db), Clock: clock, IDs: random.IDs{}, Texts: doctext.New(),
		Notes: noteRepo,
	})
	if err != nil {
		t.Fatal(err)
	}
	base, err := knowledgebase.New(knowledgebase.Deps{
		Library: lib, Notes: noteRepo, NoteTexts: lib, Subjects: lib, Lessons: sqlite.NewKnowledgeBase(db),
		Recordings: recordings, Schedule: lessons, Transcripts: sqlite.NewTranscriptRepository(db), Artifacts: store,
		Summaries: sqlite.NewSummaryRepository(db), LectureTexts: sqlite.NewLectureTextRepository(db),
		Writer: textfile.New(), Clock: clock,
	})
	if err != nil {
		t.Fatal(err)
	}

	math, err := lib.CreateSubject(ctx, library.NewSubject{Name: "Матанализ"})
	if err != nil {
		t.Fatal(err)
	}
	saveScheduleLesson(t, lessons, "l1", "Пределы", "Матанализ", libScenarioT0)
	saveRecordedLesson(t, recordings, "r1", "l1", "Матанализ", libScenarioT0)

	// placed returns the folder of the only row of math, "" for none.
	placed := func(step string) shared.FolderID {
		t.Helper()
		rows, err := base.FolderLessons(ctx, math.ID())
		if err != nil || len(rows) > 1 {
			t.Fatalf("%s: FolderLessons = %+v, %v", step, rows, err)
		}
		if len(rows) == 0 {
			return ""
		}
		if rows[0].Lesson.Recording != "r1" || rows[0].Folder != subject.SystemFolderID(math.ID(), rows[0].System) {
			t.Fatalf("%s: row = %+v", step, rows[0])
		}
		return rows[0].Folder
	}

	// 1. A recorded lecture is in «Лекции».
	if got, want := placed("lecture"), subject.SystemFolderID(math.ID(), subject.SystemLectures); got != want {
		t.Fatalf("lecture in %q, want %q", got, want)
	}

	// 2. The student corrects the kind: the row moves to «Семинары».
	if _, err := base.SetLessonInfo(ctx, "r1", "", "seminar"); err != nil {
		t.Fatal(err)
	}
	if got, want := placed("seminar"), subject.SystemFolderID(math.ID(), subject.SystemSeminars); got != want {
		t.Fatalf("seminar in %q, want %q", got, want)
	}

	// 3. Marked for deletion, the recording is in no folder.
	rec, err := recordings.Get(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.MarkDeleting(libScenarioT0, nil); err != nil {
		t.Fatal(err)
	}
	if err := recordings.Save(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if got := placed("deleting"); got != "" {
		t.Fatalf("a recording marked for deletion is in %q", got)
	}

	if after, err := db.SchemaVersion(ctx); err != nil || after != version {
		t.Fatalf("SchemaVersion = %d, %v; want %d", after, err, version)
	}
}
