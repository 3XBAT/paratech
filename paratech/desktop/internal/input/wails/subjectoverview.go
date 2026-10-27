package wails

import (
	"context"
	"errors"
	"fmt"

	"paratech/desktop/internal/cases/subjectview"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the log.
const (
	opSubjectOverview = "ui: subject overview"
	opSubjectOpened   = "ui: subject page opened"
)

// SubjectOverviewUseCase is the page of a subject (ARCH-42); satisfied by subjectview.Service.
type SubjectOverviewUseCase interface {
	Overview(ctx context.Context, id shared.SubjectID) (subjectview.Overview, error)
	MarkOpened(ctx context.Context, id shared.SubjectID) error
}

// SubjectDeadline is a line of «Ближайшее». Title is model output or the student's text: plain text only. Due is
// RFC 3339 in UTC; DaysLeft counts calendar days from today (0 is today).
type SubjectDeadline struct {
	AssignmentID string `json:"assignmentId"`
	Title        string `json:"title"`
	Due          string `json:"due"`
	DaysLeft     int    `json:"daysLeft"`
}

// SubjectSlot is a weekly place of the subject's lessons in the imported schedule: «Лекции — вт, 10:30». Kind is a
// lesson kind code ("lecture", "seminar", "practice", "lab", "unknown"); Weekday is 1 (Monday) to 7 (Sunday); Start
// and End are "HH:MM" in the student's zone.
type SubjectSlot struct {
	Kind    string `json:"kind"`
	Weekday int    `json:"weekday"`
	Start   string `json:"start"`
	End     string `json:"end"`
}

// SubjectLesson is a recorded lesson of the subject. Title is "" for an ad-hoc recording (the page names it by its
// date) and imported text otherwise: plain text only. Kind is a lesson kind code, "" if unknown. Status is
// "recorded", "transcribed", "summaryReady" or "failed". SummaryNew marks a summary that appeared after the previous
// opening. DurationSeconds is 0 when unknown.
type SubjectLesson struct {
	RecordingID     string `json:"recordingId"`
	Title           string `json:"title"`
	Kind            string `json:"kind"`
	StartedAt       string `json:"startedAt"`
	DurationSeconds int64  `json:"durationSeconds"`
	Status          string `json:"status"`
	SummaryNew      bool   `json:"summaryNew"`
}

// SubjectNewMaterial is a file of the subject marked «новое», with its folder (the page marks the folder too).
type SubjectNewMaterial struct {
	MaterialID string `json:"materialId"`
	FolderID   string `json:"folderId"`
}

// SubjectOverview is the data of the page of a subject. Name is typed text: plain text only. OpenedAt is the previous
// opening the «новое» marks are relative to (RFC 3339 in UTC; "" when the page was never opened, and nothing is
// marked). No teacher: it waits for the owner's decision.
type SubjectOverview struct {
	SubjectID    string               `json:"subjectId"`
	Name         string               `json:"name"`
	OpenedAt     string               `json:"openedAt"`
	Deadlines    []SubjectDeadline    `json:"deadlines"`
	Schedule     []SubjectSlot        `json:"schedule"`
	Lessons      []SubjectLesson      `json:"lessons"`
	NewMaterials []SubjectNewMaterial `json:"newMaterials"`
}

// withOverview adds the page of a subject.
func (s *LibraryService) withOverview(uc SubjectOverviewUseCase) *LibraryService {
	s.overview = uc
	return s
}

// SubjectOverview returns the data of the page of a subject. It changes nothing: the page calls MarkSubjectOpened
// once it has shown the overview, so the marks of this visit stay on screen and the next visit marks only what
// appears after it.
func (s *LibraryService) SubjectOverview(ctx context.Context, subjectID string) (SubjectOverview, error) {
	if s.overview == nil {
		s.log.Error(opSubjectOverview, "err", "not wired")
		return SubjectOverview{}, ErrLibraryUnavailable
	}
	v, err := s.overview.Overview(ctx, shared.SubjectID(subjectID))
	if err != nil {
		return SubjectOverview{}, s.overviewFail(opSubjectOverview, err)
	}
	return subjectOverview(v), nil
}

// MarkSubjectOpened records that the student opened the page of a subject now.
func (s *LibraryService) MarkSubjectOpened(ctx context.Context, subjectID string) error {
	if s.overview == nil {
		s.log.Error(opSubjectOpened, "err", "not wired")
		return ErrLibraryUnavailable
	}
	if err := s.overview.MarkOpened(ctx, shared.SubjectID(subjectID)); err != nil {
		return s.overviewFail(opSubjectOpened, err)
	}
	return nil
}

// overviewFail maps an error of the subject page to the page's text and logs the cause.
func (s *LibraryService) overviewFail(op string, err error) error {
	switch {
	case errors.Is(err, subjectview.ErrInvalidRequest):
		s.log.Info(op, "result", err.Error())
		return ErrLibraryInvalidID
	case errors.Is(err, subjectview.ErrSubjectNotFound):
		s.log.Info(op, "result", err.Error())
		return ErrLibrarySubjectNotFound
	}
	s.log.Error(op, "err", err)
	return ErrLibraryUnavailable
}

func subjectOverview(v subjectview.Overview) SubjectOverview {
	out := SubjectOverview{
		SubjectID: v.Subject.String(), Name: v.Name, OpenedAt: timeText(v.OpenedAt),
		Deadlines: make([]SubjectDeadline, 0, len(v.Deadlines)), Schedule: make([]SubjectSlot, 0, len(v.Schedule)),
		Lessons: make([]SubjectLesson, 0, len(v.Lessons)), NewMaterials: make([]SubjectNewMaterial, 0, len(v.Fresh)),
	}
	for _, d := range v.Deadlines {
		out.Deadlines = append(out.Deadlines, SubjectDeadline{
			AssignmentID: d.Assignment.String(), Title: d.Title, Due: timeText(d.Due), DaysLeft: d.DaysLeft,
		})
	}
	for _, sl := range v.Schedule {
		out.Schedule = append(out.Schedule, SubjectSlot{
			Kind: sl.Kind, Weekday: (int(sl.Weekday)+6)%7 + 1, Start: minuteText(sl.StartMinute), End: minuteText(sl.EndMinute),
		})
	}
	for _, l := range v.Lessons {
		out.Lessons = append(out.Lessons, SubjectLesson{
			RecordingID: l.Recording.String(), Title: l.Title, Kind: l.Kind, StartedAt: timeText(l.StartedAt),
			DurationSeconds: int64(l.Duration.Seconds()), Status: string(l.Status), SummaryNew: l.SummaryNew,
		})
	}
	for _, m := range v.Fresh {
		out.NewMaterials = append(out.NewMaterials, SubjectNewMaterial{MaterialID: string(m.Material), FolderID: string(m.Folder)})
	}
	return out
}

// minuteText writes minutes from midnight as "HH:MM".
func minuteText(minute int) string { return fmt.Sprintf("%02d:%02d", minute/60, minute%60) }
