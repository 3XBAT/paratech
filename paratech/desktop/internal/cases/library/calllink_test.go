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

// A fake call link: a reserved domain, a made-up meeting and password.
const (
	callLink    = "https://meet.example.org/j/1234567890?pwd=FakePwd42"
	callLinkPwd = "FakePwd42"
)

func TestSetAndClearCallLink(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика")
	e.clock.now = t0.Add(time.Hour)

	got, err := e.svc.SetCallLink(ctx, phys.ID(), " "+callLink+" ")
	if err != nil || got.CallLink().String() != callLink || got.Version() != 2 {
		t.Fatalf("SetCallLink = %v, %v", got, err)
	}
	stored, err := e.subjects.Get(ctx, phys.ID())
	if err != nil || stored.CallLink() != got.CallLink() || !stored.UpdatedAt().Equal(e.clock.now) {
		t.Fatalf("stored link %v at %v, %v", stored.CallLink(), stored.UpdatedAt(), err)
	}

	// The same link again keeps the version.
	if again, err := e.svc.SetCallLink(ctx, phys.ID(), callLink); err != nil || again.Version() != 2 {
		t.Fatalf("same link = %v, %v", again, err)
	}
	if link, err := e.svc.CallLink(ctx, phys.ID()); err != nil || link.String() != callLink {
		t.Fatalf("CallLink = %v, %v", link, err)
	}

	cleared, err := e.svc.ClearCallLink(ctx, phys.ID())
	if err != nil || !cleared.CallLink().IsZero() || cleared.Version() != 3 {
		t.Fatalf("ClearCallLink = %v, %v", cleared, err)
	}
	if again, err := e.svc.ClearCallLink(ctx, phys.ID()); err != nil || again.Version() != 3 {
		t.Fatalf("ClearCallLink without a link = %v, %v", again, err)
	}
	if link, err := e.svc.CallLink(ctx, phys.ID()); err != nil || !link.IsZero() {
		t.Fatalf("CallLink after the clear = %v, %v", link, err)
	}
}

func TestSetCallLinkRejects(t *testing.T) {
	e, buf := newLoggingEnv(t)
	ctx := context.Background()
	phys := createSubject(t, e, "Физика")

	tests := []struct {
		label string
		id    shared.SubjectID
		link  string
		want  error
	}{
		{"empty", phys.ID(), "  ", ErrInvalidCallLink},
		{"javascript", phys.ID(), "javascript:alert(1)", ErrInvalidCallLink},
		{"file", phys.ID(), "file://a.example.org/c$/x", ErrInvalidCallLink},
		{"user info", phys.ID(), "https://u:" + callLinkPwd + "@meet.example.org/", ErrInvalidCallLink},
		{"too long", phys.ID(), "https://meet.example.org/" + strings.Repeat("x", 2048), ErrInvalidCallLink},
		{"bad link on an unknown subject", "nope", "nope", ErrInvalidCallLink},
		{"unknown subject", "nope", callLink, ErrSubjectNotFound},
		{"bad id", "a b", callLink, ErrInvalidRequest},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			_, err := e.svc.SetCallLink(ctx, tt.id, tt.link)
			if !errors.Is(err, tt.want) {
				t.Fatalf("SetCallLink = %v, want %v", err, tt.want)
			}
			if strings.Contains(err.Error(), callLinkPwd) || strings.Contains(err.Error(), "example") {
				t.Fatalf("error %q holds the typed text", err)
			}
		})
	}
	if stored, err := e.subjects.Get(ctx, phys.ID()); err != nil || !stored.CallLink().IsZero() || stored.Version() != 1 {
		t.Fatalf("a refused change was stored: %v, %v", stored, err)
	}
	if _, err := e.svc.ClearCallLink(ctx, "nope"); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("ClearCallLink of an unknown subject = %v", err)
	}
	if _, err := e.svc.CallLink(ctx, "nope"); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("CallLink of an unknown subject = %v", err)
	}
	if strings.Contains(buf.String(), callLinkPwd) {
		t.Fatalf("the log holds the link: %s", buf)
	}
}

// A save that loses against a concurrent change is tried again on the fresh subject (the card has no version); a
// failing store is ErrUnavailable without the link in the error or the log.
func TestSetCallLinkSaveFailures(t *testing.T) {
	e, buf := newLoggingEnv(t)
	ctx := context.Background()
	phys := createSubject(t, e, "Физика")

	e.subjects.beforeSave = func() {
		if _, err := e.svc.AddAlias(ctx, phys.ID(), "Физ-ра"); err != nil {
			t.Error(err)
		}
	}
	got, err := e.svc.SetCallLink(ctx, phys.ID(), callLink)
	if err != nil || got.CallLink().String() != callLink || len(got.Aliases()) != 1 || got.Version() != 3 {
		t.Fatalf("SetCallLink after a concurrent change = %v, %v", got, err)
	}

	e.subjects.beforeSave = func() { e.subjects.err = errors.New("disk full") }
	_, err = e.svc.SetCallLink(ctx, phys.ID(), "https://bbb.example.edu/b/room")
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "example") {
		t.Fatalf("SetCallLink on a failing store = %v", err)
	}
	if strings.Contains(buf.String(), callLinkPwd) || strings.Contains(buf.String(), "bbb.example") {
		t.Fatalf("the log holds the link: %s", buf)
	}
}

// The subject is stored with the link in its one and only save; the name rules are those of CreateSubject.
func TestCreateSubjectWithCallLink(t *testing.T) {
	e, buf := newLoggingEnv(t)
	ctx := context.Background()

	got, err := e.svc.CreateSubjectWithCallLink(ctx, NewSubject{Name: "Физика"}, " "+callLink+" ")
	if err != nil || got.CallLink().String() != callLink || got.Version() != 1 || got.Name().String() != "Физика" {
		t.Fatalf("CreateSubjectWithCallLink = %v, %v", got, err)
	}
	stored, err := e.subjects.Get(ctx, got.ID())
	if err != nil || stored.CallLink() != got.CallLink() || stored.Version() != 1 || len(stored.Folders()) != len(got.Folders()) {
		t.Fatalf("stored %v, %v", stored, err)
	}

	// A taken name is refused as by CreateSubject; the link changes nothing about that.
	if _, err := e.svc.CreateSubjectWithCallLink(ctx, NewSubject{Name: "физика"}, callLink); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("taken name = %v, want ErrNameTaken", err)
	}
	if _, err := e.svc.CreateSubjectWithCallLink(ctx, NewSubject{Name: "  "}, callLink); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("empty name = %v, want ErrInvalidName", err)
	}
	if strings.Contains(buf.String(), callLinkPwd) {
		t.Fatalf("the log holds the link: %s", buf)
	}
}

// A bad link is refused before anything is read, erased or stored.
func TestCreateSubjectWithCallLinkRejectsTheLinkFirst(t *testing.T) {
	ctx := context.Background()
	for _, link := range []string{"", "javascript:alert(1)", "https://u:" + callLinkPwd + "@meet.example.org/"} {
		t.Run(link, func(t *testing.T) {
			e := newEnv(t)
			purge := withExpired(e)
			e.subjects.beforeSave = func() { t.Error("a subject was saved") }

			_, err := e.svc.CreateSubjectWithCallLink(ctx, NewSubject{Name: "Физика"}, link)
			if !errors.Is(err, ErrInvalidCallLink) || strings.Contains(err.Error(), callLinkPwd) {
				t.Fatalf("CreateSubjectWithCallLink = %v, want ErrInvalidCallLink without the text", err)
			}
			// Also an invalid name: the link is told first.
			if _, err := e.svc.CreateSubjectWithCallLink(ctx, NewSubject{Name: ""}, link); !errors.Is(err, ErrInvalidCallLink) {
				t.Fatalf("bad name and link = %v, want ErrInvalidCallLink", err)
			}
			if list, _ := e.svc.Subjects(ctx); len(list) != 0 || purge.calls != 0 {
				t.Fatalf("subjects %v, purges %d: something happened", list, purge.calls)
			}
		})
	}
}

// A store that fails on the save leaves no subject: there is no state with the subject but without its link.
func TestCreateSubjectWithCallLinkSaveFails(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	e.subjects.beforeSave = func() { e.subjects.err = errors.New("disk full") }

	if _, err := e.svc.CreateSubjectWithCallLink(ctx, NewSubject{Name: "Физика"}, callLink); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("CreateSubjectWithCallLink on a failing store = %v", err)
	}
	e.subjects.err = nil
	if list, err := e.svc.Subjects(ctx); err != nil || len(list) != 0 {
		t.Fatalf("subjects after the failed save = %v, %v", list, err)
	}
}

// fakeCallOpener records the links it was asked to open.
type fakeCallOpener struct {
	opened []string
	err    error
}

func (f *fakeCallOpener) OpenSubjectCallLink(_ context.Context, l subject.CallLink) error {
	f.opened = append(f.opened, l.String())
	return f.err
}

func TestOpenCallLink(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	opener := &fakeCallOpener{}
	e.svc.calls = opener
	phys := createSubject(t, e, "Физика")

	if err := e.svc.OpenCallLink(ctx, phys.ID()); !errors.Is(err, ErrNoCallLink) || len(opener.opened) != 0 {
		t.Fatalf("no link: %v, opened %q", err, opener.opened)
	}
	if _, err := e.svc.SetCallLink(ctx, phys.ID(), callLink); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.OpenCallLink(ctx, phys.ID()); err != nil || len(opener.opened) != 1 || opener.opened[0] != callLink {
		t.Fatalf("open = %v, opened %q", err, opener.opened)
	}

	// The link is read again at each click: one cleared meanwhile opens nothing.
	if _, err := e.svc.ClearCallLink(ctx, phys.ID()); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.OpenCallLink(ctx, phys.ID()); !errors.Is(err, ErrNoCallLink) || len(opener.opened) != 1 {
		t.Fatalf("after the clear: %v, opened %q", err, opener.opened)
	}
}

func TestOpenCallLinkFailures(t *testing.T) {
	ctx := context.Background()
	refused := errors.New("weblink: the call link names an environment variable")
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
			if _, err := e.svc.SetCallLink(ctx, phys.ID(), callLink); err != nil {
				t.Fatal(err)
			}
			id := tt.id
			if id == "" {
				id = phys.ID()
			}
			if tt.setUp != nil {
				tt.setUp(e, opener)
			}
			err := e.svc.OpenCallLink(ctx, id)
			if !errors.Is(err, tt.want) || len(opener.opened) != tt.opened {
				t.Fatalf("OpenCallLink = %v, opened %q; want %v", err, opener.opened, tt.want)
			}
			if strings.Contains(err.Error(), callLinkPwd) || strings.Contains(err.Error(), "example") {
				t.Fatalf("error %q holds the link", err)
			}
			if tt.name == "opener refuses" && !errors.Is(err, refused) {
				t.Fatalf("the opener's error is not kept: %v", err)
			}
		})
	}
}

func TestCallLinks(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика")
	chem := createSubject(t, e, "Химия")
	bio := createSubject(t, e, "Биология")
	for _, id := range []shared.SubjectID{phys.ID(), bio.ID()} {
		if _, err := e.svc.SetCallLink(ctx, id, callLink); err != nil {
			t.Fatal(err)
		}
	}

	got, err := e.svc.CallLinks(ctx, []shared.SubjectID{phys.ID(), chem.ID(), "nope"})
	if err != nil || len(got) != 1 || got[phys.ID()].String() != callLink {
		t.Fatalf("CallLinks = %v, %v", got, err)
	}
	if got, err := e.svc.CallLinks(ctx, nil); err != nil || len(got) != 0 {
		t.Fatalf("CallLinks(none) = %v, %v", got, err)
	}

	e.subjects.err = errors.New("disk full")
	if _, err := e.svc.CallLinks(ctx, []shared.SubjectID{phys.ID()}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("CallLinks on a failing store = %v", err)
	}
}
