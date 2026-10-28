package wails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

const (
	fakeTeacherName  = "Петров Пётр Петрович"
	fakeTeacherEmail = "petrov@example.invalid"
	fakeLabName      = "Сидоров С. С."
	fakeLabEmail     = "sidorov@example.invalid"
)

func (f *fakeLibrary) SetTeacher(_ context.Context, id shared.SubjectID, role subject.TeacherRole, name, email string, version int64) (*subject.Subject, error) {
	f.note("SetTeacher %s %d %s %s %s", id, version, role, name, email)
	if f.err != nil {
		return nil, f.err
	}
	return f.subjects[0], nil
}

func (f *fakeLibrary) ClearTeacher(_ context.Context, id shared.SubjectID, role subject.TeacherRole, version int64) (*subject.Subject, error) {
	f.note("ClearTeacher %s %d %s", id, version, role)
	if f.err != nil {
		return nil, f.err
	}
	return f.subjects[0], nil
}

// subjectWithTeachers has a lecturer and a lab teacher, no seminar teacher.
func subjectWithTeachers(t *testing.T) *subject.Subject {
	t.Helper()
	s := testSubject(t)
	if _, err := s.SetTeacher(subject.TeacherLecture, fakeTeacherName, fakeTeacherEmail, libT0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetTeacher(subject.TeacherLab, fakeLabName, fakeLabEmail, libT0); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLibrarySubjectCarriesTeachersAndVersion(t *testing.T) {
	s, uc, _ := newLibraryBinding(t, "", &memFiles{})
	ctx := context.Background()

	list, err := s.Subjects(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("Subjects = %+v, %v", list, err)
	}
	if list[0].Teachers != (LibraryTeachers{}) || list[0].Version != uc.subjects[0].Version() {
		t.Fatalf("no teacher: teachers %+v, version %d", list[0].Teachers, list[0].Version)
	}
	raw, err := json.Marshal(list[0])
	if err != nil || !strings.Contains(string(raw), `"teachers":{"lecture":null,"practice":null,"lab":null}`) ||
		!strings.Contains(string(raw), `"version":`) || strings.Contains(string(raw), `"teacher":`) {
		t.Fatalf("json %s, %v", raw, err)
	}

	uc.subjects = []*subject.Subject{subjectWithTeachers(t)}
	list, err = s.Subjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := list[0].Teachers
	if got.Lecture == nil || *got.Lecture != (LibraryTeacher{Name: fakeTeacherName, Email: fakeTeacherEmail}) ||
		got.Practice != nil || got.Lab == nil || *got.Lab != (LibraryTeacher{Name: fakeLabName, Email: fakeLabEmail}) {
		t.Fatalf("teachers %+v", got)
	}
	raw, err = json.Marshal(list[0])
	want := `"teachers":{"lecture":{"name":"` + fakeTeacherName + `","email":"` + fakeTeacherEmail + `"},"practice":null,` +
		`"lab":{"name":"` + fakeLabName + `","email":"` + fakeLabEmail + `"}}`
	if err != nil || !strings.Contains(string(raw), want) {
		t.Fatalf("json %s, %v", raw, err)
	}
}

func TestLibrarySetAndClearTeacher(t *testing.T) {
	s, uc, logs := newLibraryBinding(t, "", &memFiles{})
	uc.subjects = []*subject.Subject{subjectWithTeachers(t)}
	ctx := context.Background()

	res := s.SetSubjectTeacher(ctx, "s1", 3, "lab", fakeLabName, fakeLabEmail)
	if res.Problem != "" || res.Field != "" || res.Subject == nil || res.Subject.Teachers.Lab == nil ||
		res.Subject.Teachers.Lab.Email != fakeLabEmail {
		t.Fatalf("SetSubjectTeacher = %+v", res)
	}

	uc.subjects = []*subject.Subject{testSubject(t)}
	res = s.ClearSubjectTeacher(ctx, "s1", 4, "lecture")
	if res.Problem != "" || res.Subject == nil || res.Subject.Teachers != (LibraryTeachers{}) || res.Subject.ID != "s1" {
		t.Fatalf("ClearSubjectTeacher = %+v", res)
	}

	want := []string{"SetTeacher s1 3 lab " + fakeLabName + " " + fakeLabEmail, "ClearTeacher s1 4 lecture"}
	if fmt.Sprint(uc.calls) != fmt.Sprint(want) {
		t.Fatalf("calls = %q", uc.calls)
	}
	if logs.Len() != 0 {
		t.Fatalf("a success logs nothing: %s", logs.String())
	}
}

func TestLibraryTeacherProblems(t *testing.T) {
	// A cause that carries the typed texts, as a careless storage error might: it must never reach the log.
	leaky := errors.New("row: " + fakeTeacherName + " " + fakeTeacherEmail)
	tests := []struct {
		name    string
		err     error
		problem string
		field   string
		logged  bool
	}{
		{"invalid name", fmt.Errorf("%w: %w", library.ErrInvalidTeacherName, leaky), "invalid", "name", false},
		{"invalid email", fmt.Errorf("%w: %w", library.ErrInvalidTeacherEmail, leaky), "invalid", "email", false},
		{"invalid role", fmt.Errorf("%w: %w", library.ErrInvalidTeacherRole, leaky), "invalid", "role", false},
		{"invalid teacher", library.ErrInvalidTeacher, "invalid", "", false},
		{"changed", library.ErrSubjectChanged, "changed", "", true},
		{"not found", library.ErrSubjectNotFound, "not_found", "", true},
		{"invalid id", library.ErrInvalidRequest, "not_found", "", true},
		{"storage", fmt.Errorf("%w: %w", library.ErrUnavailable, leaky), "failed", "", true},
		{"unexpected", leaky, "failed", "", true},
	}
	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, uc, logs := newLibraryBinding(t, "", &memFiles{})
			uc.err = fmt.Errorf("wrapped: %w", tt.err)

			results := map[string]LibraryTeacherResult{
				"set":   s.SetSubjectTeacher(ctx, "s1", 1, "practice", fakeTeacherName, fakeTeacherEmail),
				"clear": s.ClearSubjectTeacher(ctx, "s1", 1, "practice"),
			}
			for call, res := range results {
				if res.Problem != tt.problem || res.Field != tt.field || res.Subject != nil {
					t.Errorf("%s = %+v, want problem %q field %q", call, res, tt.problem, tt.field)
				}
			}

			out := logs.String()
			if tt.logged != (out != "") {
				t.Errorf("logged %v, want %v: %s", out != "", tt.logged, out)
			}
			for _, secret := range []string{"Петров", "petrov", "example.invalid"} {
				if strings.Contains(out, secret) {
					t.Fatalf("the log has the teacher (%q): %s", secret, out)
				}
			}
		})
	}
}
