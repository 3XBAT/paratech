package library

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// Fake links of a lecture and a seminar: reserved domains, made-up meetings and passwords.
const (
	lectureCallLink = "https://lecture.example.org/j/222?pwd=FakeLec2"
	seminarCallLink = "https://seminar.example.org/j/333?pwd=FakeSem3"
)

// Set and clear per kind: each kind is stored on its own, the same value keeps the version, a change bumps it.
func TestSetAndClearCallLinkOf(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика")
	e.clock.now = t0.Add(time.Hour)

	got, err := e.svc.SetCallLinkOf(ctx, phys.ID(), subject.CallLecture, " "+lectureCallLink+" ")
	if err != nil || got.CallLinks().Lecture.String() != lectureCallLink || !got.CallLink().IsZero() || got.Version() != 2 {
		t.Fatalf("SetCallLinkOf(lecture) = %v, %v", got.CallLinks(), err)
	}
	if again, err := e.svc.SetCallLinkOf(ctx, phys.ID(), subject.CallLecture, lectureCallLink); err != nil || again.Version() != 2 {
		t.Fatalf("same link = %v, %v", again, err)
	}
	if got, err = e.svc.SetCallLinkOf(ctx, phys.ID(), subject.CallGeneral, callLink); err != nil ||
		got.CallLink().String() != callLink || got.Version() != 3 {
		t.Fatalf("SetCallLinkOf(general) = %v, %v", got.CallLinks(), err)
	}
	stored, err := e.subjects.Get(ctx, phys.ID())
	if err != nil || stored.CallLinks() != got.CallLinks() || !stored.UpdatedAt().Equal(e.clock.now) {
		t.Fatalf("stored %v, %v", stored.CallLinks(), err)
	}

	cleared, err := e.svc.ClearCallLinkOf(ctx, phys.ID(), subject.CallLecture)
	if err != nil || !cleared.CallLinks().Lecture.IsZero() || cleared.CallLink().String() != callLink || cleared.Version() != 4 {
		t.Fatalf("ClearCallLinkOf = %v, %v", cleared.CallLinks(), err)
	}
	if again, err := e.svc.ClearCallLinkOf(ctx, phys.ID(), subject.CallLab); err != nil || again.Version() != 4 {
		t.Fatalf("ClearCallLinkOf without a link = %v, %v", again, err)
	}
}

func TestSetCallLinkOfRejects(t *testing.T) {
	e, buf := newLoggingEnv(t)
	ctx := context.Background()
	phys := createSubject(t, e, "Физика")

	tests := []struct {
		label string
		id    shared.SubjectID
		kind  subject.CallKind
		link  string
		want  error
	}{
		{"bad link", phys.ID(), subject.CallSeminar, "javascript:alert(1)", ErrInvalidCallLink},
		{"user info", phys.ID(), subject.CallLab, "https://u:" + callLinkPwd + "@meet.example.org/", ErrInvalidCallLink},
		{"bad link on an unknown subject", "nope", subject.CallLecture, "nope", ErrInvalidCallLink},
		{"bad kind", phys.ID(), "practice", seminarCallLink, ErrInvalidCallKind},
		{"empty kind", phys.ID(), "", seminarCallLink, ErrInvalidCallKind},
		{"bad kind on an unknown subject", "nope", "x", seminarCallLink, ErrInvalidCallKind},
		{"unknown subject", "nope", subject.CallSeminar, seminarCallLink, ErrSubjectNotFound},
		{"bad id", "a b", subject.CallSeminar, seminarCallLink, ErrInvalidRequest},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			_, err := e.svc.SetCallLinkOf(ctx, tt.id, tt.kind, tt.link)
			if !errors.Is(err, tt.want) {
				t.Fatalf("SetCallLinkOf = %v, want %v", err, tt.want)
			}
			if strings.Contains(err.Error(), "Fake") || strings.Contains(err.Error(), "example") {
				t.Fatalf("error %q holds the typed text", err)
			}
		})
	}
	if stored, err := e.subjects.Get(ctx, phys.ID()); err != nil || !stored.CallLinks().IsZero() || stored.Version() != 1 {
		t.Fatalf("a refused change was stored: %v, %v", stored, err)
	}
	if _, err := e.svc.ClearCallLinkOf(ctx, phys.ID(), "seminars"); !errors.Is(err, ErrInvalidCallKind) {
		t.Fatalf("ClearCallLinkOf of a bad kind = %v", err)
	}
	if _, err := e.svc.ClearCallLinkOf(ctx, "nope", subject.CallLab); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("ClearCallLinkOf of an unknown subject = %v", err)
	}
	if strings.Contains(buf.String(), "Fake") || strings.Contains(buf.String(), "example") {
		t.Fatalf("the log holds a link: %s", buf)
	}
}

// The card saves without a version: a save that loses is tried again on the fresh subject.
func TestSetCallLinkOfConcurrentChange(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика")
	e.subjects.beforeSave = func() {
		e.subjects.beforeSave = nil
		if _, err := e.svc.SetCallLinkOf(ctx, phys.ID(), subject.CallLecture, lectureCallLink); err != nil {
			t.Error(err)
		}
	}
	got, err := e.svc.SetCallLinkOf(ctx, phys.ID(), subject.CallSeminar, seminarCallLink)
	if err != nil || got.CallLinks().Lecture.String() != lectureCallLink || got.CallLinks().Seminar.String() != seminarCallLink ||
		got.Version() != 3 {
		t.Fatalf("SetCallLinkOf after a concurrent change = %v, %v", got.CallLinks(), err)
	}
}

func TestOpenJoinLink(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	opener := &fakeCallOpener{}
	e.svc.calls = opener
	phys := createSubject(t, e, "Физика")

	if err := e.svc.OpenJoinLink(ctx, phys.ID(), "lecture"); !errors.Is(err, ErrNoCallLink) || len(opener.opened) != 0 {
		t.Fatalf("no link: %v, opened %q", err, opener.opened)
	}
	if _, err := e.svc.SetCallLinkOf(ctx, phys.ID(), subject.CallLecture, lectureCallLink); err != nil {
		t.Fatal(err)
	}
	// A seminar never takes the lecture link.
	if err := e.svc.OpenJoinLink(ctx, phys.ID(), "seminar"); !errors.Is(err, ErrNoCallLink) || len(opener.opened) != 0 {
		t.Fatalf("seminar with a lecture link only: %v, opened %q", err, opener.opened)
	}
	if _, err := e.svc.SetCallLinkOf(ctx, phys.ID(), subject.CallGeneral, callLink); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct{ lessonKind, want string }{
		{"lecture", lectureCallLink}, {"seminar", callLink}, {"practice", callLink}, {"", callLink}, {"unknown", callLink},
	} {
		opener.opened = nil
		if err := e.svc.OpenJoinLink(ctx, phys.ID(), step.lessonKind); err != nil || len(opener.opened) != 1 || opener.opened[0] != step.want {
			t.Fatalf("OpenJoinLink(%q) = %v, opened %q", step.lessonKind, err, opener.opened)
		}
	}

	// The link is read again at each click.
	if _, err := e.svc.ClearCallLinkOf(ctx, phys.ID(), subject.CallLecture); err != nil {
		t.Fatal(err)
	}
	opener.opened = nil
	if err := e.svc.OpenJoinLink(ctx, phys.ID(), "lecture"); err != nil || len(opener.opened) != 1 || opener.opened[0] != callLink {
		t.Fatalf("after the clear: %v, opened %q", err, opener.opened)
	}
}

func TestOpenJoinLinkFailures(t *testing.T) {
	ctx := context.Background()
	refused := errors.New("weblink: refused")
	tests := []struct {
		name   string
		id     shared.SubjectID
		setUp  func(e *env, o *fakeCallOpener)
		want   error
		opened int
	}{
		{"unknown subject", "nope", nil, ErrSubjectNotFound, 0},
		{"bad id", "a b", nil, ErrInvalidRequest, 0},
		{"storage", "", func(e *env, _ *fakeCallOpener) { e.subjects.err = errors.New("disk full") }, ErrUnavailable, 0},
		{"opener refuses", "", func(_ *env, o *fakeCallOpener) { o.err = refused }, ErrCallLinkNotOpened, 1},
		{"no opener", "", func(e *env, _ *fakeCallOpener) { e.svc.calls = nil }, ErrCallLinkNotOpened, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			opener := &fakeCallOpener{}
			e.svc.calls = opener
			phys := createSubject(t, e, "Физика")
			if _, err := e.svc.SetCallLinkOf(ctx, phys.ID(), subject.CallSeminar, seminarCallLink); err != nil {
				t.Fatal(err)
			}
			id := tt.id
			if id == "" {
				id = phys.ID()
			}
			if tt.setUp != nil {
				tt.setUp(e, opener)
			}
			err := e.svc.OpenJoinLink(ctx, id, "practice")
			if !errors.Is(err, tt.want) || len(opener.opened) != tt.opened {
				t.Fatalf("OpenJoinLink = %v, opened %q; want %v", err, opener.opened, tt.want)
			}
			if strings.Contains(err.Error(), "Fake") || strings.Contains(err.Error(), "example") {
				t.Fatalf("error %q holds the link", err)
			}
		})
	}
}

func TestCallLinkSets(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика")
	chem := createSubject(t, e, "Химия")
	bio := createSubject(t, e, "Биология")
	if _, err := e.svc.SetCallLinkOf(ctx, phys.ID(), subject.CallLecture, lectureCallLink); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SetCallLink(ctx, bio.ID(), callLink); err != nil {
		t.Fatal(err)
	}

	got, err := e.svc.CallLinkSets(ctx, []shared.SubjectID{phys.ID(), chem.ID(), "nope"})
	if err != nil || len(got) != 1 || got[phys.ID()].Lecture.String() != lectureCallLink || !got[phys.ID()].General.IsZero() {
		t.Fatalf("CallLinkSets = %v, %v", got, err)
	}
	if got, err := e.svc.CallLinkSets(ctx, nil); err != nil || len(got) != 0 {
		t.Fatalf("CallLinkSets(none) = %v, %v", got, err)
	}
	// A subject with only the general link is in the sets too.
	if got, err := e.svc.CallLinkSets(ctx, []shared.SubjectID{bio.ID()}); err != nil || len(got) != 1 ||
		got[bio.ID()] != (subject.CallLinks{General: got[bio.ID()].General}) || got[bio.ID()].General.String() != callLink {
		t.Fatalf("CallLinkSets(general only) = %v, %v", got, err)
	}

	e.subjects.err = errors.New("disk full")
	if _, err := e.svc.CallLinkSets(ctx, []shared.SubjectID{phys.ID()}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("CallLinkSets on a failing store = %v", err)
	}
}
