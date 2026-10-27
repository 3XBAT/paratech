package library

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// Fake teacher data: an obviously invented name and a reserved domain.
const (
	teacherName  = "Петров П. П."
	teacherEmail = "petrov@example.invalid"
)

// newLoggingEnv is newEnv with a logger that writes to the returned buffer.
func newLoggingEnv(t *testing.T) (*env, *bytes.Buffer) {
	t.Helper()
	e, buf := newEnv(t), &bytes.Buffer{}
	svc, err := New(Deps{
		Subjects: e.subjects, Materials: e.materials, Blobs: e.blobs, Lessons: e.lessons, Strings: e.strings,
		Clock: e.clock, IDs: &seqIDs{}, Log: slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	return e, buf
}

func TestSetAndClearTeacher(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика")
	e.clock.now = t0.Add(time.Hour)

	got, err := e.svc.SetTeacher(ctx, phys.ID(), teacherName, " "+teacherEmail, phys.Version())
	if err != nil || got.Teacher().Name() != teacherName || got.Teacher().Email() != teacherEmail || got.Version() != 2 {
		t.Fatalf("SetTeacher = %v, %v", got, err)
	}
	stored, err := e.subjects.Get(ctx, phys.ID())
	if err != nil || stored.Teacher() != got.Teacher() || !stored.UpdatedAt().Equal(e.clock.now) {
		t.Fatalf("stored teacher %v at %v, %v", stored.Teacher(), stored.UpdatedAt(), err)
	}

	// The same teacher again keeps the version.
	if again, err := e.svc.SetTeacher(ctx, phys.ID(), teacherName, teacherEmail, 2); err != nil || again.Version() != 2 {
		t.Fatalf("same teacher = %v, %v", again, err)
	}

	// Another teacher replaces it.
	other, err := e.svc.SetTeacher(ctx, phys.ID(), "Иванова А. С.", "ivanova@example.invalid", 2)
	if err != nil || other.Teacher().Email() != "ivanova@example.invalid" || other.Version() != 3 {
		t.Fatalf("other teacher = %v, %v", other, err)
	}

	cleared, err := e.svc.ClearTeacher(ctx, phys.ID(), 3)
	if err != nil || !cleared.Teacher().IsZero() || cleared.Version() != 4 {
		t.Fatalf("ClearTeacher = %v, %v", cleared, err)
	}
	if again, err := e.svc.ClearTeacher(ctx, phys.ID(), 4); err != nil || again.Version() != 4 {
		t.Fatalf("ClearTeacher without a teacher = %v, %v", again, err)
	}
	if list, err := e.svc.Subjects(ctx); err != nil || len(list) != 1 || !list[0].Teacher().IsZero() {
		t.Fatalf("Subjects = %v, %v", list, err)
	}
}

func TestSetTeacherRejects(t *testing.T) {
	e, buf := newLoggingEnv(t)
	ctx := context.Background()
	phys := createSubject(t, e, "Физика")

	tests := []struct {
		label       string
		id          shared.SubjectID
		name, email string
		version     int64
		want        error
	}{
		{"empty name", phys.ID(), " ", teacherEmail, 1, ErrInvalidTeacherName},
		{"bidi name", phys.ID(), "\u202e" + teacherName, teacherEmail, 1, ErrInvalidTeacherName},
		{"control in name", phys.ID(), "Петров\nП.", teacherEmail, 1, ErrInvalidTeacherName},
		{"long name", phys.ID(), strings.Repeat("я", 201), teacherEmail, 1, ErrInvalidTeacherName},
		{"bad email", phys.ID(), teacherName, "petrov@example", 1, ErrInvalidTeacherEmail},
		{"email list", phys.ID(), teacherName, teacherEmail + ",x@example.invalid", 1, ErrInvalidTeacherEmail},
		{"stale version", phys.ID(), teacherName, teacherEmail, 0, ErrSubjectChanged},
		{"newer version", phys.ID(), teacherName, teacherEmail, 2, ErrSubjectChanged},
		{"unknown subject", "nope", teacherName, teacherEmail, 1, ErrSubjectNotFound},
		{"bad id", "a b", teacherName, teacherEmail, 1, ErrInvalidRequest},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			_, err := e.svc.SetTeacher(ctx, tt.id, tt.name, tt.email, tt.version)
			if !errors.Is(err, tt.want) {
				t.Fatalf("SetTeacher = %v, want %v", err, tt.want)
			}
			if strings.Contains(err.Error(), "petrov") || strings.Contains(err.Error(), "Петров") {
				t.Fatalf("error %q holds the typed text", err)
			}
		})
	}
	if !errors.Is(ErrInvalidTeacherName, ErrInvalidTeacher) || !errors.Is(ErrInvalidTeacherEmail, ErrInvalidTeacher) {
		t.Fatal("the field errors do not wrap ErrInvalidTeacher")
	}
	if stored, err := e.subjects.Get(ctx, phys.ID()); err != nil || !stored.Teacher().IsZero() || stored.Version() != 1 {
		t.Fatalf("a refused change was stored: %v, %v", stored, err)
	}

	if _, err := e.svc.ClearTeacher(ctx, phys.ID(), 7); !errors.Is(err, ErrSubjectChanged) {
		t.Fatalf("ClearTeacher at another version = %v", err)
	}
	if strings.Contains(buf.String(), "petrov") || strings.Contains(buf.String(), "Петров") {
		t.Fatalf("the log holds the teacher: %s", buf)
	}
}

// A save that loses against a change made after the subject was read is ErrSubjectChanged, never tried again over a
// change the student has not seen; a failing store is ErrUnavailable without the teacher in the error.
func TestSetTeacherSaveFailures(t *testing.T) {
	e, buf := newLoggingEnv(t)
	ctx := context.Background()
	phys := createSubject(t, e, "Физика")

	e.subjects.beforeSave = func() {
		if _, err := e.svc.AddAlias(ctx, phys.ID(), "Физ-ра"); err != nil {
			t.Error(err)
		}
	}
	if _, err := e.svc.SetTeacher(ctx, phys.ID(), teacherName, teacherEmail, 1); !errors.Is(err, ErrSubjectChanged) {
		t.Fatalf("SetTeacher after a concurrent change = %v", err)
	}
	stored, err := e.subjects.Get(ctx, phys.ID())
	if err != nil || !stored.Teacher().IsZero() || stored.Version() != 2 {
		t.Fatalf("stored after the lost save: %v, %v", stored, err)
	}

	e.subjects.beforeSave = func() { e.subjects.err = errors.New("disk full") }
	_, err = e.svc.SetTeacher(ctx, phys.ID(), teacherName, teacherEmail, 2)
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "petrov") {
		t.Fatalf("SetTeacher on a failing store = %v", err)
	}
	if strings.Contains(buf.String(), "petrov") || strings.Contains(buf.String(), "Петров") {
		t.Fatalf("the log holds the teacher: %s", buf)
	}
}
