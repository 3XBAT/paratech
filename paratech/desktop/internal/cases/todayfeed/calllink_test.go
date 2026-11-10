package todayfeed

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/schedule/lesson"
)

// fakeLink is a made-up call link with a meeting password; it must never reach the log.
const (
	fakeLink    = "https://meet.example.org/j/1?pwd=FakePwd42"
	fakeLinkPwd = "FakePwd42"
)

func mustLink(t *testing.T) subject.CallLink {
	t.Helper()
	link, err := subject.ParseCallLink(fakeLink)
	if err != nil {
		t.Fatal(err)
	}
	return link
}

// loggingScreen is the screen of w with a log the test reads.
func loggingScreen(t *testing.T, w *world) (*Screen, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	s := w.screen()
	s.log = slog.New(slog.NewTextHandler(buf, nil))
	return s, buf
}

// The lesson cards carry the library subject of the lesson and the call link kept on it (ADR-0032, TASK-364); a
// lesson whose string names no subject has none and no place to keep a link. The library is read once for the posts
// and the lessons.
func TestTodayLessonsCarrySubjectAndCallLink(t *testing.T) {
	w := day(t)
	w.addLesson("l-chem", "Химия", "Химия", at(21, 16, 0))
	w.subjects["Химия"] = subjectmatch.Match{ID: "subj-chem", Name: "Химия"}
	link := mustLink(t)
	w.callLinks["subj-math"] = subject.CallLinks{General: link}

	got, err := w.screen().Today(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Lessons) != 3 {
		t.Fatalf("lessons = %+v", got.Lessons)
	}
	math, phys, chem := got.Lessons[0], got.Lessons[1], got.Lessons[2]
	if math.SubjectState != SubjectKnown || math.SubjectID != "subj-math" || math.SubjectName != "Математический анализ" ||
		math.CallLink != link || math.CallLinkKind != subject.CallGeneral {
		t.Errorf("math = %q %q %q %v %q", math.SubjectState, math.SubjectID, math.SubjectName, math.CallLink, math.CallLinkKind)
	}
	if phys.SubjectState != SubjectUnknown || phys.SubjectID != "" || phys.SubjectName != "" || !phys.CallLink.IsZero() {
		t.Errorf("physics, no library subject = %q %q %q %v", phys.SubjectState, phys.SubjectID, phys.SubjectName, phys.CallLink)
	}
	if chem.SubjectState != SubjectKnown || chem.SubjectID != "subj-chem" || !chem.CallLink.IsZero() || chem.CallLinkKind != "" {
		t.Errorf("chemistry, no link = %q %q %v", chem.SubjectState, chem.SubjectID, chem.CallLink)
	}
	if w.subjectReads != 1 || w.linkReads != 1 {
		t.Errorf("LookupSubjects read %d times, CallLinks %d; want once each", w.subjectReads, w.linkReads)
	}
}

// A lesson card carries the link its kind opens (ADR-0032 addendum 2026-11-10): the kind's link, else the general
// one, else none; practice takes the seminar link, an unknown kind the general one, and a seminar never the lecture's.
func TestTodayLessonCallLinkByKind(t *testing.T) {
	general := parseLink(t, "https://general.example.org/j/1")
	lecture := parseLink(t, "https://lecture.example.org/j/2")
	seminar := parseLink(t, "https://seminar.example.org/j/3")
	tests := []struct {
		name     string
		kind     lesson.Kind
		links    subject.CallLinks
		wantLink subject.CallLink
		wantKind subject.CallKind
	}{
		{"lecture with its own link", lesson.KindLecture, subject.CallLinks{General: general, Lecture: lecture},
			lecture, subject.CallLecture},
		{"seminar with only the general", lesson.KindSeminar, subject.CallLinks{General: general, Lecture: lecture},
			general, subject.CallGeneral},
		{"seminar never takes the lecture", lesson.KindSeminar, subject.CallLinks{Lecture: lecture},
			subject.CallLink{}, ""},
		{"lab without links", lesson.KindLab, subject.CallLinks{}, subject.CallLink{}, ""},
		{"practice takes the seminar", lesson.KindPractice, subject.CallLinks{General: general, Seminar: seminar},
			seminar, subject.CallSeminar},
		{"unknown kind takes the general", lesson.KindUnknown,
			subject.CallLinks{General: general, Lecture: lecture, Seminar: seminar}, general, subject.CallGeneral},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			w.addLessonOf("l-chem", "Химия", "Химия", tt.kind, at(21, 16, 0))
			w.subjects["Химия"] = subjectmatch.Match{ID: "subj-chem", Name: "Химия"}
			if !tt.links.IsZero() {
				w.callLinks["subj-chem"] = tt.links
			}

			got, err := w.screen().Today(context.Background())
			if err != nil || len(got.Lessons) != 1 {
				t.Fatalf("Today = %+v, %v", got.Lessons, err)
			}
			l := got.Lessons[0]
			if l.SubjectID != "subj-chem" || l.Kind != tt.kind.String() || l.CallLink != tt.wantLink ||
				l.CallLinkKind != tt.wantKind {
				t.Errorf("lesson = %q %q %v %q; want %v %q", l.SubjectID, l.Kind, l.CallLink, l.CallLinkKind,
					tt.wantLink, tt.wantKind)
			}
		})
	}
}

func parseLink(t *testing.T, s string) subject.CallLink {
	t.Helper()
	l, err := subject.ParseCallLink(s)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// Each state of the subject string is told apart: none, unknown, several subjects, one.
func TestTodayLessonSubjectStates(t *testing.T) {
	w := newWorld(t)
	w.addLesson("l1", "Без предмета", "", at(21, 8, 50))
	w.addLesson("l2", "Прочерк", "–", at(21, 10, 30))
	w.addLesson("l3", "Физика", "Физика", at(21, 12, 20))
	w.addLesson("l4", "Матан", "Матан", at(21, 14, 0))
	w.addLesson("l5", "Химия", "Химия", at(21, 15, 40))
	w.ambiguous["Матан"] = true
	w.subjects["Химия"] = subjectmatch.Match{ID: "subj-chem", Name: "Химия"}

	got, err := w.screen().Today(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []SubjectState{SubjectNone, SubjectNone, SubjectUnknown, SubjectAmbiguous, SubjectKnown}
	if len(got.Lessons) != len(want) {
		t.Fatalf("lessons = %+v", got.Lessons)
	}
	for i, l := range got.Lessons {
		if l.SubjectState != want[i] || (l.SubjectID != "") != (want[i] == SubjectKnown) {
			t.Errorf("%s: state %q, subject %q; want %q", l.Lesson, l.SubjectState, l.SubjectID, want[i])
		}
	}
}

// A failed read of the library costs only the subjects and the links: the lessons, the posts and the rest of the day
// are still shown, every lesson that names a subject is «unavailable» without a subject (no link control), and the
// log has a code, never the cause or the link.
func TestTodayLibraryUnreadKeepsTheDay(t *testing.T) {
	leaky := errors.New("row: " + fakeLink)
	tests := []struct {
		name  string
		setUp func(w *world)
		phys  SubjectState // «Физика», which no subject has
	}{
		{"subjects unread", func(w *world) { w.failSubjects = leaky }, SubjectUnavailable},
		// The lookup was read: a string no subject has is still told; only the known subject loses its place.
		{"links unread", func(w *world) { w.failLinks = leaky }, SubjectUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := day(t)
			w.addLesson("l-free", "Окно", "", at(21, 18, 0))
			w.callLinks["subj-math"] = subject.CallLinks{General: mustLink(t)}
			tt.setUp(w)
			s, logs := loggingScreen(t, w)

			got, err := s.Today(context.Background())
			if err != nil {
				t.Fatalf("Today = %v, want the day without subjects", err)
			}
			if len(got.Lessons) != 3 || len(got.Posts) != 1 || len(got.Waiting) == 0 || got.Lessons[0].Post == nil {
				t.Fatalf("the day is incomplete: lessons %+v, posts %+v, waiting %+v", got.Lessons, got.Posts, got.Waiting)
			}
			for _, l := range got.Lessons {
				wantState := map[string]SubjectState{"Матанализ": SubjectUnavailable, "Физика": tt.phys, "": SubjectNone}[l.Subject]
				if l.SubjectState != wantState || l.SubjectID != "" || l.SubjectName != "" || !l.CallLink.IsZero() {
					t.Errorf("%s: %q %q %q %v, want %q without a subject", l.Lesson, l.SubjectState, l.SubjectID,
						l.SubjectName, l.CallLink, wantState)
				}
			}
			out := logs.String()
			if out == "" || strings.Contains(out, fakeLinkPwd) || strings.Contains(out, "meet.example.org") {
				t.Fatalf("log %q: want a code without the link", out)
			}
		})
	}
}

// Without the library the post keeps the string of its lesson; the thread, which is keyed by the subject, still fails.
func TestLibraryUnreadPostsAndThread(t *testing.T) {
	w := day(t)
	w.failSubjects = errDisk

	got, err := w.screen().Today(context.Background())
	if err != nil || len(got.Posts) != 1 {
		t.Fatalf("Today = %+v, %v", got.Posts, err)
	}
	if l := got.Posts[0].Lesson; l.SubjectID != "" || l.SubjectName != "Матанализ" {
		t.Errorf("post lesson = %+v, want the string of the lesson", l)
	}
	if _, err := w.screen().Thread(context.Background(), "subj-math"); !errors.Is(err, errDisk) {
		t.Fatalf("Thread = %v, want the storage error", err)
	}
}

// No lesson resolves to a subject: the links are not read, so a failing link store changes nothing.
func TestTodayCallLinksNotReadWithoutSubjects(t *testing.T) {
	w := newWorld(t)
	w.addLesson("l1", "Физика", "Физика", at(21, 8, 50))
	w.failLinks = errDisk
	got, err := w.screen().Today(context.Background())
	if err != nil || got.Lessons[0].SubjectID != "" || got.Lessons[0].SubjectState != SubjectUnknown || w.linkReads != 0 {
		t.Fatalf("Today without subjects = %+v, %v, %d link reads", got.Lessons, err, w.linkReads)
	}
}
