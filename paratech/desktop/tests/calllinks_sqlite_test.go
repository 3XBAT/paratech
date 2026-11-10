package tests

// Scenario test of the call links per lesson kind (TASK-431, ADR-0032 addendum 2026-11-10) on SQLite and the library:
// the student keeps a general link and a lecture link; a lecture opens the lecture link, a seminar the general one, and
// after the general link is cleared a seminar opens nothing (it never takes the lecture link).

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/random"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// Fake links: reserved domains, made-up meetings and passwords.
const (
	scenarioGeneralLink = "https://general.example.org/j/111?pwd=FakeGen1"
	scenarioLectureLink = "https://lecture.example.org/j/222?pwd=FakeLec2"
)

// recordingOpener records the links it was asked to open; nothing is opened for real.
type recordingOpener struct{ opened []string }

func (o *recordingOpener) OpenSubjectCallLink(_ context.Context, l subject.CallLink) error {
	o.opened = append(o.opened, l.String())
	return nil
}

func TestCallLinksByKindOnSQLite(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := openSQLite(ctx, t, sqlite.Options{Path: filepath.Join(dir, "calllinks.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := filestore.New(filepath.Join(dir, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	opener := &recordingOpener{}
	lib, err := library.New(library.Deps{
		Subjects: sqlite.NewSubjectRepository(db), Materials: sqlite.NewMaterialRepository(db), Blobs: store,
		Lessons: sqlite.NewLessonRepository(db), Strings: sqlite.NewSubjectStrings(db), Clock: testenv.NewFakeClock(libScenarioT0),
		IDs: random.IDs{}, Calls: opener,
	})
	if err != nil {
		t.Fatal(err)
	}
	phys, err := lib.CreateSubject(ctx, library.NewSubject{Name: "Физика"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lib.SetCallLinkOf(ctx, phys.ID(), subject.CallGeneral, scenarioGeneralLink); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.SetCallLinkOf(ctx, phys.ID(), subject.CallLecture, scenarioLectureLink); err != nil {
		t.Fatal(err)
	}

	for _, lessonKind := range []string{"lecture", "seminar"} {
		if err := lib.OpenJoinLink(ctx, phys.ID(), lessonKind); err != nil {
			t.Fatalf("OpenJoinLink(%s) = %v", lessonKind, err)
		}
	}
	if len(opener.opened) != 2 || opener.opened[0] != scenarioLectureLink || opener.opened[1] != scenarioGeneralLink {
		t.Fatalf("opened %q", opener.opened)
	}

	if _, err := lib.ClearCallLinkOf(ctx, phys.ID(), subject.CallGeneral); err != nil {
		t.Fatal(err)
	}
	if err := lib.OpenJoinLink(ctx, phys.ID(), "seminar"); !errors.Is(err, library.ErrNoCallLink) || len(opener.opened) != 2 {
		t.Fatalf("seminar without a general link: %v, opened %q", err, opener.opened)
	}
	sets, err := lib.CallLinkSets(ctx, []shared.SubjectID{phys.ID()})
	if err != nil || sets[phys.ID()] != (subject.CallLinks{Lecture: sets[phys.ID()].Lecture}) ||
		sets[phys.ID()].Lecture.String() != scenarioLectureLink {
		t.Fatalf("CallLinkSets = %v, %v", sets, err)
	}
}
