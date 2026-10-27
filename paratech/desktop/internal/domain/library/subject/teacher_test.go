package subject

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
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

func TestTeacherRoles(t *testing.T) {
	if got := TeacherRoles(); !reflect.DeepEqual(got, []TeacherRole{TeacherLecture, TeacherPractice, TeacherLab}) {
		t.Fatalf("TeacherRoles = %v", got)
	}
	for _, r := range []TeacherRole{"", "seminar", "Lecture", "lab "} {
		if r.Valid() {
			t.Errorf("%q is valid", r)
		}
	}
}

// fullSubject is a subject with a teacher in every role, each with the fake data.
func fullSubject(t *testing.T) *Subject {
	t.Helper()
	s := newSubject(t)
	for _, r := range TeacherRoles() {
		if _, err := s.SetTeacher(r, teacherName, teacherEmail, t0); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// A subject, its teachers, a teacher and the stored state never print a name or an address: not with any fmt verb
// and not through slog (text or JSON). A bare []TeacherState is redacted by fmt and the text log only: JSON marshals
// its fields, so it is never logged.
func TestPrintingHidesTeachers(t *testing.T) {
	s := fullSubject(t)
	values := map[string]any{
		"subject": s, "teachers": s.Teachers(), "teacher": s.Teacher(TeacherLab), "state": s.State(),
		"state teachers": s.State().Teachers, "subjects": []*Subject{s},
	}
	leaks := func(out string) bool {
		return strings.Contains(out, "petrov") || strings.Contains(out, "Петров") || strings.Contains(out, "example.invalid")
	}
	for label, v := range values {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if out := fmt.Sprintf(verb, v); leaks(out) {
				t.Errorf("%s %s: %s", label, verb, out)
			}
		}
		var text, js bytes.Buffer
		slog.New(slog.NewTextHandler(&text, nil)).Info("x", "v", v)
		if label != "state teachers" {
			slog.New(slog.NewJSONHandler(&js, nil)).Info("x", "v", v)
		}
		if leaks(text.String()) || leaks(js.String()) {
			t.Errorf("%s through slog: %s %s", label, text.String(), js.String())
		}
	}

	if out := fmt.Sprint(s); !strings.Contains(out, "s1") || !strings.Contains(out, "Физика") {
		t.Fatalf("subject prints %q, want its id and name", out)
	}
	var text bytes.Buffer
	slog.New(slog.NewTextHandler(&text, nil)).Info("x", "subject", s)
	if !strings.Contains(text.String(), "subject.id=s1") || !strings.Contains(text.String(), "subject.name=Физика") {
		t.Fatalf("slog of a subject = %s", text.String())
	}
	if got := fmt.Sprint(s.Teachers()); got != "teachers: lecture, practice, lab" {
		t.Fatalf("teachers print %q", got)
	}
	if got := fmt.Sprint((*Subject)(nil)); got != "<nil subject>" {
		t.Fatalf("nil subject prints %q", got)
	}
}

func TestSetAndClearTeacher(t *testing.T) {
	s := newSubject(t)
	if !s.Teachers().IsZero() || s.Teachers().Roles() != nil {
		t.Fatalf("new subject has teachers: %v", s.Teachers())
	}
	t1 := t0.Add(time.Hour)

	changed, err := s.SetTeacher(TeacherPractice, " "+teacherName, teacherEmail+" ", t1)
	got := s.Teacher(TeacherPractice)
	if err != nil || !changed || got.Name() != teacherName || got.Email() != teacherEmail || !s.UpdatedAt().Equal(t1) {
		t.Fatalf("SetTeacher = %v, %v; teacher %q %q at %v", changed, err, got.Name(), got.Email(), s.UpdatedAt())
	}
	if !s.Teacher(TeacherLecture).IsZero() || !s.Teacher(TeacherLab).IsZero() || !s.Teacher("seminar").IsZero() {
		t.Fatal("another role got the teacher")
	}

	// The same teacher again changes nothing; the same teacher in another role is a change.
	if changed, err := s.SetTeacher(TeacherPractice, teacherName, teacherEmail, t1.Add(time.Hour)); err != nil || changed || !s.UpdatedAt().Equal(t1) {
		t.Fatalf("same teacher again = %v, %v; updated %v", changed, err, s.UpdatedAt())
	}
	t2 := t1.Add(2 * time.Hour)
	if changed, err := s.SetTeacher(TeacherLab, "Иванова А. С.", "ivanova@example.invalid", t2); err != nil || !changed || !s.UpdatedAt().Equal(t2) {
		t.Fatalf("lab teacher = %v, %v", changed, err)
	}
	if roles := s.Teachers().Roles(); !reflect.DeepEqual(roles, []TeacherRole{TeacherPractice, TeacherLab}) {
		t.Fatalf("roles = %v", roles)
	}

	// A bad address or an unknown role keeps every teacher that is set.
	before := s.Teachers()
	refused := []struct {
		label       string
		role        TeacherRole
		name, email string
		want        error
	}{
		{"bad address", TeacherPractice, "Иванов", "nope", ErrInvalidTeacherEmail},
		{"bad name", TeacherLab, "", teacherEmail, ErrInvalidTeacherName},
		{"unknown role", "seminar", teacherName, teacherEmail, ErrInvalidTeacherRole},
		{"empty role", "", teacherName, teacherEmail, ErrInvalidTeacherRole},
	}
	for _, tt := range refused {
		changed, err := s.SetTeacher(tt.role, tt.name, tt.email, t2.Add(time.Hour))
		if !errors.Is(err, tt.want) || !errors.Is(err, ErrInvalidTeacher) || changed {
			t.Errorf("%s: SetTeacher = %v, %v", tt.label, changed, err)
		}
	}
	if s.Teachers() != before || !s.UpdatedAt().Equal(t2) {
		t.Fatalf("teachers after refused changes = %v at %v", s.Teachers(), s.UpdatedAt())
	}
	if changed, err := s.ClearTeacher("seminar", t2.Add(time.Hour)); !errors.Is(err, ErrInvalidTeacherRole) || changed {
		t.Fatalf("ClearTeacher(unknown role) = %v, %v", changed, err)
	}

	t3 := t2.Add(time.Hour)
	if changed, err := s.ClearTeacher(TeacherPractice, t3); err != nil || !changed || !s.Teacher(TeacherPractice).IsZero() ||
		s.Teacher(TeacherLab).IsZero() || !s.UpdatedAt().Equal(t3) {
		t.Fatalf("ClearTeacher = %v, %v; teachers %v, updated %v", changed, err, s.Teachers(), s.UpdatedAt())
	}
	if changed, err := s.ClearTeacher(TeacherPractice, t3.Add(time.Hour)); err != nil || changed || !s.UpdatedAt().Equal(t3) {
		t.Fatalf("ClearTeacher without a teacher = %v, %v", changed, err)
	}
}

func TestRestoreTeachers(t *testing.T) {
	s := newSubject(t)
	if _, err := s.SetTeacher(TeacherLab, teacherName, teacherEmail, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetTeacher(TeacherLecture, "Иванова А. С.", "ivanova@example.invalid", t0); err != nil {
		t.Fatal(err)
	}
	st := s.State()
	st.Version = 2
	want := []TeacherState{
		{Role: TeacherLecture, Name: "Иванова А. С.", Email: "ivanova@example.invalid"},
		{Role: TeacherLab, Name: teacherName, Email: teacherEmail},
	}
	if !reflect.DeepEqual(st.Teachers, want) {
		t.Fatal("state teachers are not the set roles in role order")
	}
	back, err := Restore(st)
	if err != nil || back.Teachers() != s.Teachers() {
		t.Fatalf("Restore = %v, %v", back, err)
	}

	// The stored order does not matter; the state comes back in role order.
	reversed := st
	reversed.Teachers = []TeacherState{want[1], want[0]}
	if back, err := Restore(reversed); err != nil || !reflect.DeepEqual(back.State().Teachers, want) {
		t.Fatalf("Restore of another order = %v, %v", back, err)
	}

	tests := []struct {
		label    string
		teachers []TeacherState
	}{
		{"name only", []TeacherState{{Role: TeacherLab, Name: teacherName}}},
		{"email only", []TeacherState{{Role: TeacherLab, Email: teacherEmail}}},
		{"bad email", []TeacherState{{Role: TeacherLab, Name: teacherName, Email: "nope"}}},
		{"untrimmed name", []TeacherState{{Role: TeacherLab, Name: " " + teacherName, Email: teacherEmail}}},
		{"bidi in name", []TeacherState{{Role: TeacherLab, Name: "‮" + teacherName, Email: teacherEmail}}},
		{"unknown role", []TeacherState{{Role: "seminar", Name: teacherName, Email: teacherEmail}}},
		{"empty role", []TeacherState{{Name: teacherName, Email: teacherEmail}}},
		{"role twice", []TeacherState{want[1], want[1]}},
		{"bad second", []TeacherState{want[0], {Role: TeacherPractice, Name: teacherName, Email: "x"}}},
	}
	for _, tt := range tests {
		bad := st
		bad.Teachers = tt.teachers
		if _, err := Restore(bad); !errors.Is(err, ErrInvalidSubject) {
			t.Errorf("%s: Restore = %v, want ErrInvalidSubject", tt.label, err)
		}
	}
}
