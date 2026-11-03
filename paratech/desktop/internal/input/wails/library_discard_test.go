package wails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

func (f *fakeLibrary) DiscardSubject(_ context.Context, id shared.SubjectID, version int64) (library.Discarded, error) {
	f.note("DiscardSubject %s %d", id, version)
	if f.err != nil {
		return library.Discarded{}, f.err
	}
	return f.discarded, nil
}

func (f *fakeLibrary) RestoreSubject(_ context.Context, id shared.SubjectID, version int64) (*subject.Subject, error) {
	f.note("RestoreSubject %s %d", id, version)
	if f.err != nil {
		return nil, f.err
	}
	return f.subjects[0], nil
}

func (f *fakeLibrary) RemovedSubject(_ context.Context, id shared.SubjectID) (*subject.Subject, error) {
	if f.removedErr != nil {
		return nil, f.removedErr
	}
	if sub, ok := f.removed[id]; ok {
		return sub, nil
	}
	return nil, library.ErrSubjectNotFound
}

// removedSubject is the test subject removed and stored at version.
func removedSubject(t *testing.T, version int64) *subject.Subject {
	t.Helper()
	s := testSubject(t)
	if _, err := s.Remove(libT0); err != nil {
		t.Fatal(err)
	}
	s.Persisted(version)
	return s
}

func TestLibraryDiscardAndRestoreSubject(t *testing.T) {
	s, uc, logs := newLibraryBinding(t, "", &memFiles{})
	uc.discarded = library.Discarded{Subject: removedSubject(t, 8), Notes: 2, Files: 1}
	ctx := context.Background()

	got := s.DiscardSubject(ctx, "s1", 7)
	if got != (LibraryDiscard{Notes: 2, Files: 1, Version: 8}) {
		t.Fatalf("DiscardSubject = %+v", got)
	}
	raw, err := json.Marshal(got)
	if err != nil || string(raw) != `{"problem":"","notes":2,"files":1,"version":8}` {
		t.Fatalf("json %s, %v", raw, err)
	}

	back := s.RestoreSubject(ctx, "s1", 8)
	if back.Problem != "" || back.Subject == nil || back.Subject.ID != "s1" || back.Subject.Name != "Физика" {
		t.Fatalf("RestoreSubject = %+v", back)
	}
	if want := []string{"DiscardSubject s1 7", "RestoreSubject s1 8"}; !reflect.DeepEqual(uc.calls, want) {
		t.Fatalf("calls = %q", uc.calls)
	}
	if logs.Len() != 0 {
		t.Fatalf("a success logs nothing here: %s", logs.String())
	}
}

func TestLibraryDiscardAndRestoreProblems(t *testing.T) {
	cause := errors.New("disk")
	tests := []struct {
		name    string
		err     error
		discard string
		restore string
		level   string
	}{
		{"changed", library.ErrSubjectChanged, "changed", "changed", "INFO"},
		{"not found", library.ErrSubjectNotFound, "not_found", "not_found", "INFO"},
		{"invalid id", library.ErrInvalidRequest, "not_found", "not_found", "INFO"},
		// DiscardSubject never answers it; were it to, it is still a code the page knows.
		{"expired", library.ErrRestoreExpired, "expired", "expired", "INFO"},
		{"storage", fmt.Errorf("%w: %w", library.ErrUnavailable, cause), "failed", "failed", "ERROR"},
		{"unexpected", cause, "failed", "failed", "ERROR"},
	}
	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, uc, logs := newLibraryBinding(t, "", &memFiles{})
			uc.err = fmt.Errorf("wrapped: %w", tt.err)

			if got := s.DiscardSubject(ctx, "s1", 3); got != (LibraryDiscard{Problem: tt.discard}) {
				t.Errorf("DiscardSubject = %+v, want problem %q", got, tt.discard)
			}
			if got := s.RestoreSubject(ctx, "s1", 4); got.Problem != tt.restore || got.Subject != nil {
				t.Errorf("RestoreSubject = %+v, want problem %q", got, tt.restore)
			}

			out := logs.String()
			for _, op := range []string{opLibraryDiscard, opLibraryRestoreSub} {
				if !strings.Contains(out, "level="+tt.level+` msg="`+op+`"`) {
					t.Errorf("no %s record of %q: %s", tt.level, op, out)
				}
			}
		})
	}
}

func TestLibraryCreateSubjectWithTheNameOfARemovedOne(t *testing.T) {
	ctx := context.Background()
	gone := fmt.Errorf("wrapped: %w", &library.RemovedNameError{Subject: "s1"})

	t.Run("removed holder", func(t *testing.T) {
		s, uc, _ := newLibraryBinding(t, "", &memFiles{})
		uc.err, uc.removed = gone, map[shared.SubjectID]*subject.Subject{"s1": removedSubject(t, 5)}

		_, err := s.CreateSubject(ctx, "физика", nil, "")
		var removed *LibraryRemovedNameError
		if !errors.As(err, &removed) || *removed != (LibraryRemovedNameError{Problem: "removed_name_taken", SubjectID: "s1", Version: 5}) {
			t.Fatalf("CreateSubject = %#v, want a LibraryRemovedNameError of s1 at 5", err)
		}
		// A page that does not know the code still shows the text of a taken name.
		if err.Error() != ErrLibraryNameTaken.Error() || !errors.Is(err, ErrLibraryNameTaken) {
			t.Fatalf("text %q", err)
		}
		// Wails passes json.Marshal(&err) to the page as the cause of the rejection.
		raw, jerr := json.Marshal(&err)
		if jerr != nil || string(raw) != `{"problem":"removed_name_taken","subjectId":"s1","version":5}` {
			t.Fatalf("cause %s, %v", raw, jerr)
		}
	})

	tests := []struct {
		name       string
		err        error
		removed    map[shared.SubjectID]*subject.Subject
		removedErr error
		want       error
	}{
		{"active holder", fmt.Errorf("w: %w", library.ErrNameTaken), nil, nil, ErrLibraryNameTaken},
		{"removed holder gone meanwhile", gone, nil, nil, ErrLibraryBusy},
		{"removed holder unreadable", gone, nil, fmt.Errorf("%w: disk", library.ErrUnavailable), ErrLibraryUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, uc, _ := newLibraryBinding(t, "", &memFiles{})
			uc.err, uc.removed, uc.removedErr = tt.err, tt.removed, tt.removedErr

			_, err := s.CreateSubject(ctx, "физика", nil, "")
			var removed *LibraryRemovedNameError
			if !errors.Is(err, tt.want) || errors.As(err, &removed) {
				t.Fatalf("CreateSubject = %#v, want %q", err, tt.want)
			}
		})
	}
}

func TestCreateFromScheduleOffersToRestoreARemovedSubject(t *testing.T) {
	uc := &fakeScheduleSubjects{res: library.ScheduleResult{Skipped: []library.ScheduleSkipped{
		{Key: "физика", Removed: "s1", Reason: library.ReasonNameTaken},
		{Subject: "s-hist", Removed: "s-purged", Reason: library.ReasonNameTaken},
		{Key: "химия", Reason: library.ReasonNameTaken},
		{Subject: "s-bio", Removed: "s1", Reason: library.ReasonTooManyAliases}, // Removed only goes with name_taken
	}}}
	lib := &fakeLibrary{removed: map[shared.SubjectID]*subject.Subject{"s1": removedSubject(t, 9)}}
	s := NewLibraryService(lib, nil, nil, discard()).withSchedule(uc)

	got := s.CreateFromSchedule(context.Background(), tokA, nil)
	want := []ScheduleSkippedRow{
		{Key: "физика", Reason: "name_taken", RemovedSubjectID: "s1", RemovedSubjectVersion: 9},
		{SubjectID: "s-hist", Reason: "name_taken"}, // purged meanwhile: a plain row, no «Вернуть» that cannot work
		{Key: "химия", Reason: "name_taken"},
		{SubjectID: "s-bio", Reason: "too_many_aliases"},
	}
	if got.Problem != "" || !reflect.DeepEqual(got.Skipped, want) {
		t.Fatalf("Skipped = %+v, problem %q\nwant %+v", got.Skipped, got.Problem, want)
	}
	raw, err := json.Marshal(got.Skipped[0])
	if err != nil || string(raw) != `{"key":"физика","subjectId":"","reason":"name_taken","removedSubjectId":"s1","removedSubjectVersion":9}` {
		t.Fatalf("json %s, %v", raw, err)
	}

	// The removed subject cannot be read: the row stays, without the button.
	lib.removedErr = fmt.Errorf("%w: disk", library.ErrUnavailable)
	got = s.CreateFromSchedule(context.Background(), tokB, nil)
	if got.Skipped[0] != (ScheduleSkippedRow{Key: "физика", Reason: "name_taken"}) {
		t.Fatalf("unreadable removed subject: row %+v", got.Skipped[0])
	}
}
