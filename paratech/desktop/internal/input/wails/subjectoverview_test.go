package wails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/subjectview"
	"paratech/desktop/internal/domain/library/services/overview"
	"paratech/desktop/internal/domain/shared"
)

// fakeOverview is the subject page use case; it records the ids it was asked for.
type fakeOverview struct {
	view    subjectview.Overview
	err     error
	asked   []shared.SubjectID
	openErr error
	opened  []shared.SubjectID
}

func (f *fakeOverview) Overview(_ context.Context, id shared.SubjectID) (subjectview.Overview, error) {
	f.asked = append(f.asked, id)
	return f.view, f.err
}

func (f *fakeOverview) MarkOpened(_ context.Context, id shared.SubjectID) error {
	if f.openErr != nil {
		return f.openErr
	}
	f.opened = append(f.opened, id)
	return nil
}

func TestSubjectOverviewDTO(t *testing.T) {
	msk := time.FixedZone("MSK", 3*60*60)
	uc := &fakeOverview{view: subjectview.Overview{
		Subject: "s1", Name: "Линейная алгебра", OpenedAt: time.Date(2026, 10, 20, 9, 0, 0, 0, msk),
		Deadlines: []overview.Deadline{
			{Assignment: "a1", Title: "Задачи 3, 5, 8", Due: time.Date(2026, 10, 27, 10, 30, 0, 0, msk), DaysLeft: 6},
		},
		Schedule: []overview.Slot{
			{Kind: "seminar", Weekday: time.Monday, StartMinute: 12*60 + 10, EndMinute: 13*60 + 40},
			{Kind: "lab", Weekday: time.Sunday, StartMinute: 9 * 60, EndMinute: 10*60 + 5},
		},
		Lessons: []subjectview.Lesson{{
			Recording: "r1", Title: "Смена базиса", Kind: "lecture", StartedAt: time.Date(2026, 10, 6, 10, 30, 0, 0, msk),
			Duration: 89*time.Minute + 40*time.Second, Status: overview.StatusSummaryReady, SummaryNew: true,
		}},
		Fresh: []subjectview.FreshMaterial{{Material: "m1", Folder: "f-tests"}},
	}}
	s := NewLibraryService(&fakeLibrary{}, nil, nil, discard()).withOverview(uc)

	got, err := s.SubjectOverview(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	want := SubjectOverview{
		SubjectID: "s1", Name: "Линейная алгебра", OpenedAt: "2026-10-20T06:00:00Z",
		Deadlines: []SubjectDeadline{{AssignmentID: "a1", Title: "Задачи 3, 5, 8", Due: "2026-10-27T07:30:00Z", DaysLeft: 6}},
		Schedule: []SubjectSlot{
			{Kind: "seminar", Weekday: 1, Start: "12:10", End: "13:40"},
			{Kind: "lab", Weekday: 7, Start: "09:00", End: "10:05"},
		},
		Lessons: []SubjectLesson{{
			RecordingID: "r1", Title: "Смена базиса", Kind: "lecture", StartedAt: "2026-10-06T07:30:00Z",
			DurationSeconds: 89*60 + 40, Status: "summaryReady", SummaryNew: true,
		}},
		NewMaterials: []SubjectNewMaterial{{MaterialID: "m1", FolderID: "f-tests"}},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("overview =\n%+v\nwant\n%+v", got, want)
	}
	if len(uc.asked) != 1 || uc.asked[0] != "s1" || len(uc.opened) != 0 {
		t.Fatalf("asked %v, opened %v: the overview must not record an opening", uc.asked, uc.opened)
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"subjectId"`, `"openedAt"`, `"deadlines"`, `"daysLeft"`, `"schedule"`, `"weekday"`,
		`"lessons"`, `"durationSeconds"`, `"summaryNew"`, `"newMaterials"`, `"folderId"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("JSON lacks %s: %s", key, raw)
		}
	}
	if strings.Contains(strings.ToLower(string(raw)), "teacher") {
		t.Errorf("JSON has a teacher: %s", raw)
	}
}

// An empty page is empty lists for the page, never null.
func TestSubjectOverviewEmptyListsAreArrays(t *testing.T) {
	s := NewLibraryService(&fakeLibrary{}, nil, nil, discard()).withOverview(&fakeOverview{view: subjectview.Overview{Subject: "s1"}})
	got, err := s.SubjectOverview(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "null") {
		t.Fatalf("JSON has null: %s", raw)
	}
	if got.OpenedAt != "" {
		t.Fatalf("openedAt = %q, want \"\" for a page never opened", got.OpenedAt)
	}
}

func TestMarkSubjectOpened(t *testing.T) {
	uc := &fakeOverview{}
	s := NewLibraryService(&fakeLibrary{}, nil, nil, discard()).withOverview(uc)
	if err := s.MarkSubjectOpened(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	if len(uc.opened) != 1 || uc.opened[0] != "s1" || len(uc.asked) != 0 {
		t.Fatalf("opened %v, asked %v", uc.opened, uc.asked)
	}
}

func TestSubjectOverviewErrorsAreFixedTexts(t *testing.T) {
	cause := errors.New("visitfile: disk I/O error at C:/Users/x/subject-visits.json")
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"invalid id", subjectview.ErrInvalidRequest, ErrLibraryInvalidID},
		{"subject gone", subjectview.ErrSubjectNotFound, ErrLibrarySubjectNotFound},
		{"storage", fmt.Errorf("%w: %w", subjectview.ErrUnavailable, cause), ErrLibraryUnavailable},
		{"unknown", cause, ErrLibraryUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewLibraryService(&fakeLibrary{}, nil, nil, discard()).withOverview(&fakeOverview{err: tt.err, openErr: tt.err})
			if _, err := s.SubjectOverview(context.Background(), "s1"); !errors.Is(err, tt.want) {
				t.Errorf("SubjectOverview = %v, want %v", err, tt.want)
			}
			if err := s.MarkSubjectOpened(context.Background(), "s1"); !errors.Is(err, tt.want) {
				t.Errorf("MarkSubjectOpened = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestSubjectOverviewNotWired(t *testing.T) {
	s := NewLibraryService(&fakeLibrary{}, nil, nil, discard())
	if _, err := s.SubjectOverview(context.Background(), "s1"); !errors.Is(err, ErrLibraryUnavailable) {
		t.Errorf("SubjectOverview = %v", err)
	}
	if err := s.MarkSubjectOpened(context.Background(), "s1"); !errors.Is(err, ErrLibraryUnavailable) {
		t.Errorf("MarkSubjectOpened = %v", err)
	}
}
