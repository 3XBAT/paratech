package tests

// Scenario test of the soft removal of a subject (TASK-288, ADR-0027 «Removing a subject that has notes») on the real
// stack: SQLite, the artifact store, the library, the notes and the knowledge base. The student removes a subject with
// a note and files (one in an own folder): everything under it is hidden from every reader, a file of it that is a
// part of a note of another subject stays in that note, the name stays taken; «Вернуть» brings it all back as it was.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/doctext"
	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/random"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/adapters/textfile"
	"paratech/desktop/internal/cases/knowledgebase"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/cases/notes"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

type removalStack struct {
	lib   *library.Service
	notes *notes.Service
	kb    *knowledgebase.Service
	repo  *sqlite.NoteRepository
	clock *testenv.FakeClock
}

func newRemovalStack(t *testing.T) removalStack {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := openSQLite(ctx, t, sqlite.Options{Path: filepath.Join(dir, "removal.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := filestore.New(filepath.Join(dir, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

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
	own, err := notes.New(notes.Deps{
		Notes: noteRepo, Materials: lib, Recordings: recordings, Lessons: lessons, Clock: clock, IDs: random.IDs{},
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
	return removalStack{lib: lib, notes: own, kb: base, repo: noteRepo, clock: clock}
}

// file uploads text and files it in folder of sub ("" is «Материалы»).
func (s removalStack) file(t *testing.T, sub shared.SubjectID, folder shared.FolderID, name, text string) shared.MaterialID {
	t.Helper()
	im, err := s.lib.Import(context.Background(), library.Upload{Name: name, Data: []byte(text), Subject: sub, Folder: folder})
	if err != nil || im.NotPlaced {
		t.Fatalf("Import(%s) = %+v, %v", name, im, err)
	}
	return im.Material.ID()
}

// dayNote stores a note of sub for 12 September with parts.
func (s removalStack) dayNote(t *testing.T, id shared.NoteID, sub shared.SubjectID, parts ...shared.MaterialID) {
	t.Helper()
	date, err := note.NewDate(2026, time.September, 12)
	if err != nil {
		t.Fatal(err)
	}
	a, err := note.NewAnchor(note.AnchorSpec{Kind: note.AnchorDay, Date: date})
	if err != nil {
		t.Fatal(err)
	}
	n, err := note.New(note.Spec{ID: id, Subject: sub, Anchor: a, Title: "Конспект", Parts: parts}, libScenarioT0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.repo.Save(context.Background(), n); err != nil {
		t.Fatal(err)
	}
}

// seen is what the readers show of the removed subject phys and of chem.
type seen struct {
	subjects, physFiles, physNotes, physDayNotes, kbGroups, taylorHits int
	nested                                                             bool
	chemPartShown                                                      bool
}

func (s removalStack) look(t *testing.T, phys, chem shared.SubjectID, nested shared.MaterialID) seen {
	t.Helper()
	ctx := context.Background()
	var out seen
	list, err := s.lib.Subjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	out.subjects = len(list)
	files, err := s.lib.Materials(ctx, phys)
	if err != nil {
		t.Fatal(err)
	}
	out.physFiles = len(files)
	_, err = s.lib.Material(ctx, nested)
	switch {
	case err == nil:
		out.nested = true
	case !errors.Is(err, library.ErrMaterialNotFound):
		t.Fatal(err)
	}
	views, err := s.notes.ForSubject(ctx, phys)
	if err != nil {
		t.Fatal(err)
	}
	out.physNotes = len(views)
	day, err := note.NewDate(2026, time.September, 12)
	if err != nil {
		t.Fatal(err)
	}
	if views, err = s.notes.ForLesson(ctx, notes.Lecture{Subject: phys, Date: day}); err != nil {
		t.Fatal(err)
	}
	out.physDayNotes = len(views)
	chemViews, err := s.notes.ForSubject(ctx, chem)
	if err != nil || len(chemViews) != 1 || len(chemViews[0].Parts) != 1 {
		t.Fatalf("ForSubject(chem) = %+v, %v", chemViews, err)
	}
	out.chemPartShown = chemViews[0].Parts[0] != nil
	groups, err := s.kb.Subjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	out.kbGroups = len(groups)
	found, err := s.kb.Search(ctx, "тейлор")
	if err != nil {
		t.Fatal(err)
	}
	out.taylorHits = len(found.Hits)
	return out
}

func TestSubjectRemovalHidesEverythingAndRestoreBringsItBack(t *testing.T) {
	ctx := context.Background()
	s := newRemovalStack(t)

	phys, err := s.lib.CreateSubject(ctx, library.NewSubject{Name: "Физика", Aliases: []string{"Физ"}})
	if err != nil {
		t.Fatal(err)
	}
	chem, err := s.lib.CreateSubject(ctx, library.NewSubject{Name: "Химия"})
	if err != nil {
		t.Fatal(err)
	}
	own, err := s.lib.CreateFolder(ctx, phys.ID(), "Задачники")
	if err != nil {
		t.Fatal(err)
	}
	part := s.file(t, phys.ID(), "", "taylor.txt", "Ряд Тейлора сходится")
	nested := s.file(t, phys.ID(), own.ID(), "tasks.txt", "Задачи по механике")
	s.file(t, phys.ID(), "", "plan.txt", "План семестра")
	both := s.file(t, phys.ID(), "", "both.txt", "Общая часть")
	s.dayNote(t, "n-phys", phys.ID(), part)
	s.dayNote(t, "n-chem", chem.ID(), both) // a file of «Физика» in a note of «Химия» (moved there after it was added)

	before := s.look(t, phys.ID(), chem.ID(), nested)
	if before.subjects != 2 || before.physFiles != 4 || !before.nested || before.physNotes != 1 || before.physDayNotes != 1 ||
		before.kbGroups != 2 || before.taylorHits != 1 || !before.chemPartShown {
		t.Fatalf("before the removal: %+v", before)
	}

	list, err := s.lib.Subjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var cur *subject.Subject
	for _, x := range list {
		if x.ID() == phys.ID() {
			cur = x
		}
	}
	got, err := s.lib.DiscardSubject(ctx, phys.ID(), cur.Version())
	if err != nil {
		t.Fatal(err)
	}
	// The note, and the files not in it: the nested one, the plan and the one in the note of «Химия».
	if got.Notes != 1 || got.Files != 3 {
		t.Fatalf("DiscardSubject = notes %d, files %d", got.Notes, got.Files)
	}

	after := s.look(t, phys.ID(), chem.ID(), nested)
	want := seen{subjects: 1, kbGroups: 1, chemPartShown: true}
	if after != want {
		t.Fatalf("after the removal: %+v, want %+v", after, want)
	}
	if m, err := s.lib.ResolveSubjects(ctx, []string{"физ"}); err != nil || len(m) != 0 {
		t.Fatalf("ResolveSubjects after the removal = %v, %v", m, err)
	}
	var taken *library.RemovedNameError
	if _, err := s.lib.CreateSubject(ctx, library.NewSubject{Name: "физика"}); !errors.As(err, &taken) || taken.Subject != phys.ID() {
		t.Fatalf("CreateSubject(removed name) = %v, want RemovedNameError of %s", err, phys.ID())
	}

	s.clock.Advance(subject.RestoreWindow - time.Minute)
	back, err := s.lib.RestoreSubject(ctx, phys.ID(), got.Subject.Version())
	if err != nil || !back.Active() {
		t.Fatalf("RestoreSubject = %v, %v", back, err)
	}
	if again := s.look(t, phys.ID(), chem.ID(), nested); again != before {
		t.Fatalf("after «Вернуть»: %+v, want %+v", again, before)
	}

	// Removed again, it cannot come back once the window is over.
	if got, err = s.lib.DiscardSubject(ctx, phys.ID(), back.Version()); err != nil {
		t.Fatal(err)
	}
	s.clock.Advance(subject.RestoreWindow)
	if _, err := s.lib.RestoreSubject(ctx, phys.ID(), got.Subject.Version()); !errors.Is(err, library.ErrRestoreExpired) {
		t.Fatalf("RestoreSubject after the window = %v, want ErrRestoreExpired", err)
	}
}
