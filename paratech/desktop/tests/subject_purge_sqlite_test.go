package tests

// Scenario test of the purge of a removed subject (TASK-308, ADR-0027 «Purge after 3 days») on the real stack: SQLite,
// the artifact store, the library and the subject purge. Three days after the removal the subject is erased with its
// notes, files, folders, aliases and ticket sets; its assignments, recordings, lessons and works stay, a file of it in
// a note of another subject or that a work was written from stays in «Входящие», and the name can be created again.

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
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/cases/subjectpurge"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

type purgeStack struct {
	db     *sqlite.DB
	store  *filestore.Store
	lib    *library.Service
	purger *subjectpurge.Purger
	notes  *sqlite.NoteRepository
	clock  *testenv.FakeClock
}

func newPurgeStack(t *testing.T) purgeStack {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := openSQLite(ctx, t, sqlite.Options{Path: filepath.Join(dir, "purge.db"), Migrate: true})
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
	noteRepo := sqlite.NewNoteRepository(db)
	purger, err := subjectpurge.New(subjectpurge.Deps{
		Subjects: sqlite.NewSubjectRepository(db), Notes: noteRepo, TicketSets: sqlite.NewSubjectTicketSets(db),
		Materials: sqlite.NewMaterialRepository(db), Blobs: store, Clock: clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	lib, err := library.New(library.Deps{
		Subjects: sqlite.NewSubjectRepository(db), Materials: sqlite.NewMaterialRepository(db), Blobs: store,
		Lessons: sqlite.NewLessonRepository(db), Strings: sqlite.NewSubjectStrings(db), Clock: clock, IDs: random.IDs{},
		Texts: doctext.New(), Notes: noteRepo, Expired: purger,
	})
	if err != nil {
		t.Fatal(err)
	}
	return purgeStack{db: db, store: store, lib: lib, purger: purger, notes: noteRepo, clock: clock}
}

func (s purgeStack) file(t *testing.T, sub shared.SubjectID, folder shared.FolderID, name, text string) shared.MaterialID {
	t.Helper()
	im, err := s.lib.Import(context.Background(), library.Upload{Name: name, Data: []byte(text), Subject: sub, Folder: folder})
	if err != nil || im.NotPlaced {
		t.Fatalf("Import(%s) = %+v, %v", name, im, err)
	}
	return im.Material.ID()
}

func (s purgeStack) dayNote(t *testing.T, id shared.NoteID, sub shared.SubjectID, parts ...shared.MaterialID) {
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
	if err := s.notes.Save(context.Background(), n); err != nil {
		t.Fatal(err)
	}
}

func (s purgeStack) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.db.DBTX(ctx).ExecContext(ctx, q, args...); err != nil {
		t.Fatal(err)
	}
}

func (s purgeStack) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	ctx := context.Background()
	var n int
	if err := s.db.DBTX(ctx).QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// stored reports whether the bytes of text are in the artifact store.
func (s purgeStack) stored(t *testing.T, text string) bool {
	t.Helper()
	ref, err := material.BlobRef(material.DigestOf([]byte(text)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.store.Size(context.Background(), ref)
	if err != nil && !errors.Is(err, shared.ErrNotFound) {
		t.Fatal(err)
	}
	return err == nil
}

// discard removes sub at the clock.
func (s purgeStack) discard(t *testing.T, sub shared.SubjectID) {
	t.Helper()
	ctx := context.Background()
	list, err := s.lib.Subjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range list {
		if x.ID() == sub {
			if _, err := s.lib.DiscardSubject(ctx, sub, x.Version()); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("no subject %s to remove", sub)
}

func TestRemovedSubjectIsPurgedAfterThreeDays(t *testing.T) {
	ctx := context.Background()
	s := newPurgeStack(t)

	phys, err := s.lib.CreateSubject(ctx, library.NewSubject{Name: "Физика", Aliases: []string{"Физ"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.lib.SetTeacher(ctx, phys.ID(), subject.TeacherLecture, "Иванов И. И.", "teacher@example.test", phys.Version()); err != nil {
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
	s.file(t, phys.ID(), own.ID(), "tasks.txt", "Задачи по механике")
	plan := s.file(t, phys.ID(), "", "plan.txt", "План семестра")
	both := s.file(t, phys.ID(), "", "both.txt", "Общая часть")
	source := s.file(t, phys.ID(), "", "essay-task.txt", "Тема реферата")
	s.dayNote(t, "n-phys", phys.ID(), part)
	s.dayNote(t, "n-chem", chem.ID(), both) // a file of «Физика» in a note of «Химия»

	// A work the student is writing from a file of «Физика», with its first round.
	s.exec(t, `INSERT INTO works (id, material_id, status, round, attempt, version, created_at, updated_at)
		VALUES ('w1', ?, 'questions', 1, 1, 1, 1, 1)`, string(source))
	s.exec(t, `INSERT INTO work_rounds (work_id, round, answer) VALUES ('w1', 1, '{}')`)

	// Other contexts that name the subject: a lesson and a recording by its name, an assignment made from its file, a
	// ticket set by its id.
	s.exec(t, `INSERT INTO schedule_entries (id, title, starts_at, ends_at, timezone, subject, created_at, updated_at)
		VALUES ('l1', 'Лекция', 1000, 2000, 'Europe/Moscow', 'Физика', 1, 1)`)
	s.exec(t, `INSERT INTO recordings (id, lesson_id, status, started_at, max_duration_ms, chunks_ref, lesson_subject, created_at, updated_at)
		VALUES ('r1', 'l1', 'FAILED', 1000, 10800000, 'recordings/r1', 'Физика', 1, 1)`)
	s.exec(t, `INSERT INTO assignments (id, lesson_id, title, material_id, created_at, updated_at)
		VALUES ('a1', 'l1', 'Задачи 1-5', ?, 1, 1)`, string(plan))
	s.exec(t, `INSERT INTO ticket_sets (id, subject_id, status, content, version, created_at, updated_at)
		VALUES ('ts1', ?, 'done', '{}', 1, 1, 1)`, string(phys.ID()))

	s.discard(t, phys.ID())

	// A second before the end: nothing is erased, the name is still the removed subject's.
	s.clock.Advance(subject.RestoreWindow - time.Second)
	if n, err := s.purger.Purge(ctx); err != nil || n != 0 {
		t.Fatalf("Purge inside the window = %d, %v", n, err)
	}
	var taken *library.RemovedNameError
	if _, err := s.lib.CreateSubject(ctx, library.NewSubject{Name: "физика"}); !errors.As(err, &taken) || taken.Subject != phys.ID() {
		t.Fatalf("CreateSubject inside the window = %v, want RemovedNameError of %s", err, phys.ID())
	}

	// At the end the start (or the daily run) erases it.
	s.clock.Advance(time.Second)
	if n, err := s.purger.Purge(ctx); err != nil || n != 1 {
		t.Fatalf("Purge = %d, %v; want 1", n, err)
	}
	if n, err := s.purger.Purge(ctx); err != nil || n != 0 {
		t.Fatalf("Purge again = %d, %v; want 0", n, err)
	}

	id := string(phys.ID())
	for q, want := range map[string]int{
		"SELECT count(*) FROM subjects WHERE id = ?":                 0,
		"SELECT count(*) FROM subject_aliases WHERE subject_id = ?":  0,
		"SELECT count(*) FROM subject_folders WHERE subject_id = ?":  0,
		"SELECT count(*) FROM subject_teachers WHERE subject_id = ?": 0,
		"SELECT count(*) FROM notes WHERE subject_id = ?":            0,
		"SELECT count(*) FROM materials WHERE subject_id = ?":        0,
		"SELECT count(*) FROM ticket_sets WHERE subject_id = ?":      0,
	} {
		if n := s.count(t, q, id); n != want {
			t.Errorf("%s = %d, want %d", q, n, want)
		}
	}
	for q, want := range map[string]int{
		"SELECT count(*) FROM assignments WHERE id = 'a1' AND material_id IS NULL": 1,
		"SELECT count(*) FROM recordings WHERE id = 'r1'":                          1,
		"SELECT count(*) FROM schedule_entries WHERE id = 'l1'":                    1,
		"SELECT count(*) FROM materials":                                           2,
		"SELECT count(*) FROM works WHERE id = 'w1'":                               1,
		"SELECT count(*) FROM work_rounds WHERE work_id = 'w1'":                    1,
	} {
		if n := s.count(t, q); n != want {
			t.Errorf("%s = %d, want %d", q, n, want)
		}
	}

	// The file in the note of «Химия» and the source of the work are in «Входящие» with their bytes; the others' bytes
	// are gone.
	for name, id := range map[string]shared.MaterialID{"the file of the other note": both, "the source of the work": source} {
		m, err := s.lib.Material(ctx, id)
		if err != nil || !m.InInbox() || !m.Active() {
			t.Fatalf("%s = %v, %v; want it active in the inbox", name, m, err)
		}
	}
	for text, kept := range map[string]bool{
		"Общая часть": true, "Тема реферата": true, "Ряд Тейлора сходится": false, "Задачи по механике": false,
		"План семестра": false,
	} {
		if got := s.stored(t, text); got != kept {
			t.Errorf("bytes of %q stored = %v, want %v", text, got, kept)
		}
	}
	if chemNote, err := s.notes.Get(ctx, "n-chem"); err != nil || len(chemNote.Parts()) != 1 {
		t.Fatalf("the note of «Химия» = %v, %v", chemNote, err)
	}

	// The name is free.
	if _, err := s.lib.CreateSubject(ctx, library.NewSubject{Name: "Физика", Aliases: []string{"Физ"}}); err != nil {
		t.Fatalf("CreateSubject after the purge = %v", err)
	}
}

// The name is free at the end of the window even before the daily purge ran: the check erases the subject itself.
func TestNameOfARemovedSubjectIsFreeAtTheEndOfTheWindow(t *testing.T) {
	ctx := context.Background()
	s := newPurgeStack(t)
	phys, err := s.lib.CreateSubject(ctx, library.NewSubject{Name: "Физика"})
	if err != nil {
		t.Fatal(err)
	}
	s.file(t, phys.ID(), "", "plan.txt", "План семестра")
	s.discard(t, phys.ID())

	s.clock.Advance(subject.RestoreWindow)
	again, err := s.lib.CreateSubject(ctx, library.NewSubject{Name: "физика"})
	if err != nil || again.ID() == phys.ID() {
		t.Fatalf("CreateSubject at the end of the window = %v, %v", again, err)
	}
	if s.count(t, "SELECT count(*) FROM subjects WHERE id = ?", string(phys.ID())) != 0 || s.stored(t, "План семестра") {
		t.Fatal("the expired subject or its file is still stored")
	}
}
