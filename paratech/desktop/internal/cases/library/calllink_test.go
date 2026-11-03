package library

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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
