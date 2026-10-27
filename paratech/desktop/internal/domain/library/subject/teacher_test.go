package subject

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Fake teacher data: an obviously invented name and a reserved domain.
const (
	teacherName  = "Петров П. П."
	teacherEmail = "petrov@example.invalid"
)

func TestNewTeacherAccepts(t *testing.T) {
	tests := []struct {
		name, email         string
		wantName, wantEmail string
	}{
		{teacherName, teacherEmail, teacherName, teacherEmail},
		{"  Анна Сергеевна  ", " a.b+c@mail.example.org ", "Анна Сергеевна", "a.b+c@mail.example.org"},
		{strings.Repeat("я", MaxTeacherNameRunes), "x@y.z", strings.Repeat("я", MaxTeacherNameRunes), "x@y.z"},
		{"O'Brien", strings.Repeat("a", MaxTeacherEmailLen-len("@b.cd")) + "@b.cd", "O'Brien",
			strings.Repeat("a", MaxTeacherEmailLen-len("@b.cd")) + "@b.cd"},
	}
	for _, tt := range tests {
		got, err := NewTeacher(tt.name, tt.email)
		if err != nil || got.Name() != tt.wantName || got.Email() != tt.wantEmail || got.IsZero() {
			t.Errorf("NewTeacher(%q, %q) = %q %q, %v", tt.name, tt.email, got.Name(), got.Email(), err)
		}
	}
}

func TestNewTeacherRejects(t *testing.T) {
	tests := []struct {
		label, name, email string
		want               error
	}{
		{"empty name", "  ", teacherEmail, ErrInvalidTeacherName},
		{"long name", strings.Repeat("я", MaxTeacherNameRunes+1), teacherEmail, ErrInvalidTeacherName},
		{"newline", "Петров\nП.", teacherEmail, ErrInvalidTeacherName},
		{"tab", "Петров\tП.", teacherEmail, ErrInvalidTeacherName},
		{"line separator", "Петров П.", teacherEmail, ErrInvalidTeacherName},
		{"bidi override", "Петров‮П.", teacherEmail, ErrInvalidTeacherName},
		{"bidi isolate", "⁦Петров", teacherEmail, ErrInvalidTeacherName},
		{"zero width", "Пет​ров", teacherEmail, ErrInvalidTeacherName},
		{"invalid utf-8", "Петров\xff", teacherEmail, ErrInvalidTeacherName},
		{"empty email", teacherName, "", ErrInvalidTeacherEmail},
		{"no at", teacherName, "petrov.example.invalid", ErrInvalidTeacherEmail},
		{"two ats", teacherName, "a@b@example.invalid", ErrInvalidTeacherEmail},
		{"display name", teacherName, "Петров <petrov@example.invalid>", ErrInvalidTeacherEmail},
		{"inner space", teacherName, "pet rov@example.invalid", ErrInvalidTeacherEmail},
		{"non ascii", teacherName, "петров@example.invalid", ErrInvalidTeacherEmail},
		{"header injection", teacherName, "a@example.invalid\r\nBcc: b@example.invalid", ErrInvalidTeacherEmail},
		{"list", teacherName, "a@example.invalid,b@example.invalid", ErrInvalidTeacherEmail},
		{"no dot in domain", teacherName, "a@localhost", ErrInvalidTeacherEmail},
		{"double dot", teacherName, "a..b@example.invalid", ErrInvalidTeacherEmail},
		{"leading dot", teacherName, ".a@example.invalid", ErrInvalidTeacherEmail},
		{"trailing dot domain", teacherName, "a@example.invalid.", ErrInvalidTeacherEmail},
		{"too long", teacherName, strings.Repeat("a", MaxTeacherEmailLen) + "@b.cd", ErrInvalidTeacherEmail},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			_, err := NewTeacher(tt.name, tt.email)
			if !errors.Is(err, tt.want) || !errors.Is(err, ErrInvalidTeacher) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			// Personal data never gets into an error.
			msg := err.Error()
			for _, part := range []string{"Петров", "petrov", "example"} {
				if strings.Contains(msg, part) {
					t.Fatalf("error %q holds the typed text", msg)
				}
			}
		})
	}
}

func TestTeacherStringHidesData(t *testing.T) {
	tch, err := NewTeacher(teacherName, teacherEmail)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{fmt.Sprint(tch), fmt.Sprintf("%v %s", tch, tch)} {
		if strings.Contains(s, "petrov") || strings.Contains(s, "Петров") {
			t.Fatalf("printed teacher %q shows its data", s)
		}
	}
}

func TestSetAndClearTeacher(t *testing.T) {
	s := newSubject(t)
	if !s.Teacher().IsZero() {
		t.Fatalf("new subject has a teacher")
	}
	t1 := t0.Add(time.Hour)

	changed, err := s.SetTeacher(" "+teacherName, teacherEmail+" ", t1)
	if err != nil || !changed || s.Teacher().Name() != teacherName || s.Teacher().Email() != teacherEmail ||
		!s.UpdatedAt().Equal(t1) {
		t.Fatalf("SetTeacher = %v, %v; teacher %q %q at %v", changed, err, s.Teacher().Name(), s.Teacher().Email(), s.UpdatedAt())
	}
	if changed, err := s.SetTeacher(teacherName, teacherEmail, t1.Add(time.Hour)); err != nil || changed || !s.UpdatedAt().Equal(t1) {
		t.Fatalf("same teacher again = %v, %v; updated %v", changed, err, s.UpdatedAt())
	}

	// A bad address keeps the teacher that is set.
	if changed, err := s.SetTeacher("Иванов", "nope", t1.Add(time.Hour)); !errors.Is(err, ErrInvalidTeacherEmail) || changed {
		t.Fatalf("bad address = %v, %v", changed, err)
	}
	if s.Teacher().Email() != teacherEmail {
		t.Fatalf("teacher after a refused change = %q", s.Teacher().Email())
	}

	t2 := t1.Add(2 * time.Hour)
	if !s.ClearTeacher(t2) || !s.Teacher().IsZero() || !s.UpdatedAt().Equal(t2) {
		t.Fatalf("ClearTeacher: teacher %v, updated %v", s.Teacher(), s.UpdatedAt())
	}
	if s.ClearTeacher(t2.Add(time.Hour)) || !s.UpdatedAt().Equal(t2) {
		t.Fatal("ClearTeacher without a teacher changed the subject")
	}
}

func TestRestoreTeacher(t *testing.T) {
	s := newSubject(t)
	if _, err := s.SetTeacher(teacherName, teacherEmail, t0); err != nil {
		t.Fatal(err)
	}
	st := s.State()
	st.Version = 2
	if st.TeacherName != teacherName || st.TeacherEmail != teacherEmail {
		t.Fatalf("state teacher = %q %q", st.TeacherName, st.TeacherEmail)
	}
	back, err := Restore(st)
	if err != nil || back.Teacher() != s.Teacher() {
		t.Fatalf("Restore = %v, %v", back, err)
	}

	tests := []struct {
		label, name, email string
	}{
		{"name only", teacherName, ""},
		{"email only", "", teacherEmail},
		{"bad email", teacherName, "nope"},
		{"untrimmed name", " " + teacherName, teacherEmail},
		{"bidi in name", "‮" + teacherName, teacherEmail},
	}
	for _, tt := range tests {
		bad := st
		bad.TeacherName, bad.TeacherEmail = tt.name, tt.email
		if _, err := Restore(bad); !errors.Is(err, ErrInvalidSubject) {
			t.Errorf("%s: Restore = %v, want ErrInvalidSubject", tt.label, err)
		}
	}
}
