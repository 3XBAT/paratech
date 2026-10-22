package tests

// Scenario test of the library (TASK-121, ADR-0024) on the real stack: SQLite, the artifact store and the knowledge
// base. Subjects are suggested from the schedule but never created by themselves; once the student creates one with
// the schedule string as an alias, the knowledge base groups the recorded lesson by its id. A file is uploaded twice
// (one material), filed, linked, removed and restored, and the subject can be removed only when it is empty.

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/random"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/adapters/textfile"
	"paratech/desktop/internal/cases/knowledgebase"
	"paratech/desktop/internal/cases/library"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/recording/recording"
	recvalues "paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

var (
	_ library.SubjectRepository     = (*sqlite.SubjectRepository)(nil)
	_ library.MaterialRepository    = (*sqlite.MaterialRepository)(nil)
	_ library.SubjectStrings        = (*sqlite.SubjectStrings)(nil)
	_ library.LessonReader          = (*sqlite.LessonRepository)(nil)
	_ library.BlobStore             = (*filestore.Store)(nil)
	_ library.IDs                   = random.IDs{}
	_ knowledgebase.SubjectResolver = (*library.Service)(nil)
)

var libScenarioT0 = time.Date(2026, time.October, 20, 7, 0, 0, 0, time.UTC)

func libraryDocx(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range []string{"[Content_Types].xml", "word/document.xml"} {
		if _, err := zw.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func saveScheduleLesson(t *testing.T, repo *sqlite.LessonRepository, id shared.LessonID, title, subj string, at time.Time) {
	t.Helper()
	l, err := lesson.New(lesson.Spec{ID: id, Content: lesson.Content{
		Title: title, Subject: subj, Kind: lesson.KindLecture, Start: at, End: at.Add(90 * time.Minute), TimeZone: "Europe/Moscow",
	}}, libScenarioT0)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background(), l); err != nil {
		t.Fatal(err)
	}
}

// saveRecordedLesson stores a completed recording of lesson that kept a snapshot naming subj.
func saveRecordedLesson(t *testing.T, repo *sqlite.RecordingRepository, id shared.RecordingID, l shared.LessonID, subj string, at time.Time) {
	t.Helper()
	r, err := recording.New(recording.Spec{
		ID: id, MaxDuration: 3 * time.Hour, LessonID: l,
		Lesson: recvalues.NewLessonSnapshot(recvalues.LessonSnapshotSpec{Title: "Лекция", Subject: subj, Kind: "lecture"}),
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	end := at.Add(90 * time.Minute)
	track, err := r.TrackRef("wav")
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(r.Begin(at), r.Stop(end), r.Finalize(end), r.Complete(end, track, 90*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background(), r); err != nil {
		t.Fatal(err)
	}
}

func TestLibraryScenario(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := openSQLite(ctx, t, sqlite.Options{Path: filepath.Join(dir, "library.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := filestore.New(filepath.Join(dir, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	lessons, recordings := sqlite.NewLessonRepository(db), sqlite.NewRecordingRepository(db)
	saveScheduleLesson(t, lessons, "l1", "Тёплые сети — лекция", "Тёплые сети", libScenarioT0)
	saveScheduleLesson(t, lessons, "l2", "Механика", "Физика", libScenarioT0.Add(24*time.Hour))
	saveRecordedLesson(t, recordings, "r1", "l1", "Тёплые сети", libScenarioT0)

	clock := testenv.NewFakeClock(libScenarioT0)
	lib, err := library.New(library.Deps{
		Subjects: sqlite.NewSubjectRepository(db), Materials: sqlite.NewMaterialRepository(db), Blobs: store,
		Lessons: lessons, Strings: sqlite.NewSubjectStrings(db), Clock: clock, IDs: random.IDs{},
	})
	if err != nil {
		t.Fatal(err)
	}
	base, err := knowledgebase.New(knowledgebase.Deps{
		Library: lib, Lessons: sqlite.NewKnowledgeBase(db), Recordings: recordings, Schedule: lessons,
		Transcripts: sqlite.NewTranscriptRepository(db), Artifacts: store, Summaries: sqlite.NewSummaryRepository(db),
		LectureTexts: sqlite.NewLectureTextRepository(db), Writer: textfile.New(), Clock: clock,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Suggestions from the schedule and the recording; nothing is created.
	got, err := lib.SuggestSubjects(ctx)
	want := []subjectmatch.Suggestion{{Name: "Тёплые сети", Lessons: 1, Recordings: 1}, {Name: "Физика", Lessons: 1}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("SuggestSubjects = %+v, %v; want %+v", got, err, want)
	}
	if subjects, err := lib.Subjects(ctx); err != nil || len(subjects) != 0 {
		t.Fatalf("subjects created by themselves: %d, %v", len(subjects), err)
	}
	if groups, err := base.Subjects(ctx); err != nil || len(groups) != 1 || groups[0].Key != "теплые сети" {
		t.Fatalf("knowledge base before = %+v, %v", groups, err)
	}

	// 2. The student creates the subject with the schedule string as an alias: the lesson joins it by id, the stored
	// strings stay as they were.
	heat, err := lib.CreateSubject(ctx, library.NewSubject{Name: "Теплоснабжение", Aliases: []string{"теплые сети"}})
	if err != nil {
		t.Fatal(err)
	}
	if heat.Semester().Code() != "2026-autumn" {
		t.Fatalf("semester %q", heat.Semester().Code())
	}
	if got, err := lib.SuggestSubjects(ctx); err != nil || len(got) != 1 || got[0].Name != "Физика" {
		t.Fatalf("SuggestSubjects after = %+v, %v", got, err)
	}
	groups, err := base.Subjects(ctx)
	if err != nil || len(groups) != 1 || groups[0].Key != kb.LibraryKey(heat.ID()) || groups[0].Name != "Теплоснабжение" {
		t.Fatalf("knowledge base after = %+v, %v", groups, err)
	}
	if ls, err := base.Lessons(ctx, groups[0].Key); err != nil || len(ls) != 1 || ls[0].Subject != "Тёплые сети" {
		t.Fatalf("lessons of the subject = %+v, %v", ls, err)
	}

	// 3. Upload: one material and one blob per sha256.
	doc := libraryDocx(t)
	first, err := lib.Import(ctx, library.Upload{Name: "Лекция 1.docx", Data: doc})
	if err != nil || first.Existing || !first.Material.InInbox() {
		t.Fatalf("Import = %+v, %v", first, err)
	}
	blob := filepath.Join(dir, "artifacts", "materials", first.Material.Digest().String())
	if data, err := os.ReadFile(blob); err != nil || !bytes.Equal(data, doc) {
		t.Fatalf("blob %s: %v", blob, err)
	}
	again, err := lib.Import(ctx, library.Upload{Name: "copy.docx", Data: doc})
	if err != nil || !again.Existing || again.Material.ID() != first.Material.ID() {
		t.Fatalf("Import again = %+v, %v", again, err)
	}
	id := first.Material.ID()

	// 4. File it, link it to the lesson.
	placed, err := lib.Place(ctx, id, heat.ID(), "")
	if err != nil || placed.Folder() != subject.SystemFolderID(heat.ID(), subject.SystemMaterials) {
		t.Fatalf("Place = %+v, %v", placed, err)
	}
	if linked, err := lib.Link(ctx, id, "l1"); err != nil || !reflect.DeepEqual(linked.Lessons(), []shared.LessonID{"l1"}) {
		t.Fatalf("Link = %+v, %v", linked, err)
	}

	// 5. Removed softly: the subject still holds it; restored where it was.
	if _, err := lib.RemoveMaterial(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := lib.RemoveSubject(ctx, heat.ID()); !errors.Is(err, library.ErrNotEmpty) {
		t.Fatalf("RemoveSubject with a removed file = %v", err)
	}
	restored, err := lib.RestoreMaterial(ctx, id)
	if err != nil || !restored.Active() || restored.Subject() != heat.ID() || len(restored.Lessons()) != 1 {
		t.Fatalf("RestoreMaterial = %+v, %v", restored, err)
	}

	// 6. Back to «Входящие», then the empty subject goes; the lesson is grouped by its string again.
	if _, err := lib.MoveToInbox(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := lib.RemoveSubject(ctx, heat.ID()); err != nil {
		t.Fatalf("RemoveSubject(empty) = %v", err)
	}
	if groups, err := base.Subjects(ctx); err != nil || len(groups) != 1 || groups[0].Key != "теплые сети" {
		t.Fatalf("knowledge base after the removal = %+v, %v", groups, err)
	}
	if inbox, err := lib.Materials(ctx, ""); err != nil || len(inbox) != 1 || inbox[0].ID() != id {
		t.Fatalf("inbox = %v, %v", inbox, err)
	}
}
