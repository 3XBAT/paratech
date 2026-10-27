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
)

func (f *fakeLibrary) SetTeacher(_ context.Context, id shared.SubjectID, name, email string, version int64) (*subject.Subject, error) {
	f.note("SetTeacher %s %d %s %s", id, version, name, email)
	if f.err != nil {
		return nil, f.err
	}
	return f.subjects[0], nil
}

func (f *fakeLibrary) ClearTeacher(_ context.Context, id shared.SubjectID, version int64) (*subject.Subject, error) {
	f.note("ClearTeacher %s %d", id, version)
	if f.err != nil {
		return nil, f.err
	}
	return f.subjects[0], nil
}

func subjectWithTeacher(t *testing.T) *subject.Subject {
	t.Helper()
	s := testSubject(t)
	if _, err := s.SetTeacher(fakeTeacherName, fakeTeacherEmail, libT0); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLibrarySubjectCarriesTeacherAndVersion(t *testing.T) {
	s, uc, _ := newLibraryBinding(t, "", &memFiles{})
	ctx := context.Background()

	list, err := s.Subjects(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("Subjects = %+v, %v", list, err)
	}
	if list[0].Teacher != nil || list[0].Version != uc.subjects[0].Version() {
		t.Fatalf("no teacher: teacher %+v, version %d", list[0].Teacher, list[0].Version)
	}
	raw, err := json.Marshal(list[0])
	if err != nil || !strings.Contains(string(raw), `"teacher":null`) || !strings.Contains(string(raw), `"version":`) {
		t.Fatalf("json %s, %v", raw, err)
	}

	uc.subjects = []*subject.Subject{subjectWithTeacher(t)}
	list, err = s.Subjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := LibraryTeacher{Name: fakeTeacherName, Email: fakeTeacherEmail}
	if list[0].Teacher == nil || *list[0].Teacher != want || list[0].Version != uc.subjects[0].Version() {
		t.Fatalf("teacher %+v, version %d", list[0].Teacher, list[0].Version)
	}
	raw, err = json.Marshal(list[0])
	if err != nil || !strings.Contains(string(raw), `"teacher":{"name":"`+fakeTeacherName+`","email":"`+fakeTeacherEmail+`"}`) {
		t.Fatalf("json %s, %v", raw, err)
	}
}

func TestLibrarySetAndClearTeacher(t *testing.T) {
	s, uc, logs := newLibraryBinding(t, "", &memFiles{})
	uc.subjects = []*subject.Subject{subjectWithTeacher(t)}
	ctx := context.Background()

	res := s.SetSubjectTeacher(ctx, "s1", 3, fakeTeacherName, fakeTeacherEmail)
	if res.Problem != "" || res.Field != "" || res.Subject == nil || res.Subject.Teacher == nil ||
		res.Subject.Teacher.Email != fakeTeacherEmail {
		t.Fatalf("SetSubjectTeacher = %+v", res)
	}

	uc.subjects = []*subject.Subject{testSubject(t)}
	res = s.ClearSubjectTeacher(ctx, "s1", 4)
	if res.Problem != "" || res.Subject == nil || res.Subject.Teacher != nil || res.Subject.ID != "s1" {
		t.Fatalf("ClearSubjectTeacher = %+v", res)
	}

	want := []string{"SetTeacher s1 3 " + fakeTeacherName + " " + fakeTeacherEmail, "ClearTeacher s1 4"}
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
				"set":   s.SetSubjectTeacher(ctx, "s1", 1, fakeTeacherName, fakeTeacherEmail),
				"clear": s.ClearSubjectTeacher(ctx, "s1", 1),
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
