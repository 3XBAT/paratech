package tests

// Scenario tests of the call links per lesson kind (TASK-431, TASK-432, ADR-0032 addendum 2026-11-10) on SQLite and
// the library: the student keeps a general link and a lecture link; a lecture opens the lecture link, a seminar the
// general one, and after the general link is cleared a seminar opens nothing (it never takes the lecture link). The
// «Сегодня» cards of a lecture and a seminar of one subject show the host of the link their kind opens, and the click
// with the card's kind opens exactly that link.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/random"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/cases/todayfeed"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/schedule/lesson"
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

// callLinkEnv is the library on SQLite with a recording opener and the subject «Физика» that keeps a general and a
// lecture link.
type callLinkEnv struct {
	db     *sqlite.DB
	lib    *library.Service
	opener *recordingOpener
	clock  *testenv.FakeClock
	phys   *subject.Subject
}

func newCallLinkEnv(ctx context.Context, t *testing.T) *callLinkEnv {
	t.Helper()
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

	e := &callLinkEnv{db: db, opener: &recordingOpener{}, clock: testenv.NewFakeClock(libScenarioT0)}
	e.lib, err = library.New(library.Deps{
		Subjects: sqlite.NewSubjectRepository(db), Materials: sqlite.NewMaterialRepository(db), Blobs: store,
		Lessons: sqlite.NewLessonRepository(db), Strings: sqlite.NewSubjectStrings(db), Clock: e.clock,
		IDs: random.IDs{}, Calls: e.opener,
	})
	if err != nil {
		t.Fatal(err)
	}
	if e.phys, err = e.lib.CreateSubject(ctx, library.NewSubject{Name: "Физика"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.SetCallLinkOf(ctx, e.phys.ID(), subject.CallGeneral, scenarioGeneralLink); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.SetCallLinkOf(ctx, e.phys.ID(), subject.CallLecture, scenarioLectureLink); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestCallLinksByKindOnSQLite(t *testing.T) {
	ctx := context.Background()
	e := newCallLinkEnv(ctx, t)
	lib, phys, opener := e.lib, e.phys, e.opener

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

// «Сегодня» on SQLite with the library as its subjects and call links (as the app wires it): the lecture card shows
// the lecture host, the seminar card the general one, and «Подключиться» with each card's kind opens that link.
func TestTodayLessonsOpenTheLinkOfTheirKindOnSQLite(t *testing.T) {
	ctx := context.Background()
	e := newCallLinkEnv(ctx, t)
	lessons := sqlite.NewLessonRepository(e.db)
	for i, k := range []lesson.Kind{lesson.KindLecture, lesson.KindSeminar} {
		start := libScenarioT0.Add(time.Duration(2+2*i) * time.Hour)
		l, err := lesson.New(lesson.Spec{ID: shared.LessonID("les-" + k.String()), Content: lesson.Content{
			Title: "Физика", Subject: "Физика", Kind: k, Start: start, End: start.Add(90 * time.Minute),
			TimeZone: "Europe/Moscow",
		}}, libScenarioT0.Add(-24*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if err := lessons.Save(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	screen, err := todayfeed.NewScreen(todayfeed.ScreenDeps{
		Posts: sqlite.NewTodayPostRepository(e.db), Recordings: sqlite.NewRecordingRepository(e.db), Lessons: lessons,
		Homework: sqlite.NewAssignmentRepository(e.db), Assignments: noAssignments{},
		Proposals: sqlite.NewProposalRepository(e.db), Subjects: e.lib, CallLinks: e.lib, Clock: e.clock,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := screen.Today(ctx)
	if err != nil || len(got.Lessons) != 2 {
		t.Fatalf("Today = %+v, %v", got.Lessons, err)
	}
	want := []struct {
		kind, host string
		callKind   subject.CallKind
		opens      string
	}{
		{"lecture", "lecture.example.org", subject.CallLecture, scenarioLectureLink},
		{"seminar", "general.example.org", subject.CallGeneral, scenarioGeneralLink},
	}
	for i, row := range got.Lessons {
		w := want[i]
		if row.SubjectID != e.phys.ID() || row.Kind != w.kind || row.CallLink.Host() != w.host || row.CallLinkKind != w.callKind {
			t.Fatalf("card %d = %q %q %q %q, want %q %q %q", i, row.SubjectID, row.Kind, row.CallLink.Host(),
				row.CallLinkKind, w.kind, w.host, w.callKind)
		}
		if err := e.lib.OpenJoinLink(ctx, row.SubjectID, row.Kind); err != nil {
			t.Fatalf("open %s = %v", row.Kind, err)
		}
		if last := e.opener.opened[len(e.opener.opened)-1]; last != w.opens {
			t.Fatalf("%s opened %q, want %q", row.Kind, last, w.opens)
		}
	}
	if len(e.opener.opened) != 2 {
		t.Fatalf("opened %q", e.opener.opened)
	}
}
