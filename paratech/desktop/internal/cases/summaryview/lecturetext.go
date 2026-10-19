package summaryview

import (
	"context"
	"fmt"

	"paratech/desktop/internal/domain/coursework/services/summaryexport"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// File names of the lecture text (Russian UI).
const (
	lectureLessonFileName = "%s — текст лекции %s" // the lesson title and the date
	lectureAdHocFileName  = "Текст лекции %s"      // the start date and time
)

// lectureOf derives the state of the lecture text from its newest job; v holds the stored text, if any. No job is a
// transcript older than the lecture text (TASK-090): with a text it is done, without one it can be started.
func lectureOf(j *job.Job, v LectureText) LectureText {
	if j == nil {
		v.Step = Step{State: StateNone}
		if v.Ready {
			v.Step = Step{State: StateDone}
		}
		v.CanStart = !v.Ready
		return v
	}
	v.Step = stepOf(j, v.Ready)
	if v.State == StateDone && !v.Ready {
		// Finished without a text: it was removed since, nothing is being made.
		v.State = StateNone
	}
	if v.State == StateWaiting || v.State == StateRunning {
		if done, parts, ok := values.ParseLectureTextCheckpoint(j.Progress().Checkpoint()); ok {
			v.PartsDone, v.Parts = done, parts
		}
	}
	return v
}

// LectureText renders the lecture text as plain text for the clipboard; times adds the start of each block
// ("[12:05]"). No text is ErrNoLectureText.
func (s *Service) LectureText(ctx context.Context, id shared.RecordingID, times bool) (string, error) {
	return s.renderLecture(ctx, id, summaryexport.FormatText, times)
}

// LectureSuggestedName returns the file name the save dialog proposes for the lecture text: the lesson title, "текст
// лекции" and the date, or "Текст лекции" with the date and time of an ad-hoc recording, safe for Windows, with the
// extension of f.
func (s *Service) LectureSuggestedName(ctx context.Context, id shared.RecordingID, f summaryexport.Format) (string, error) {
	if err := f.Validate(); err != nil {
		return "", ErrInvalidRequest
	}
	rec, err := s.recording(ctx, id)
	if err != nil {
		return "", err
	}
	at := rec.StartedAt().In(s.clock.Now().Location())
	if l := s.liveLesson(ctx, rec); l != nil && l.Content().Title != "" {
		return summaryexport.FileName(fmt.Sprintf(lectureLessonFileName, l.Content().Title, at.Format(fileDateLayout)), f), nil
	}
	return summaryexport.FileName(fmt.Sprintf(lectureAdHocFileName, at.Format(fileTimeLayout)), f), nil
}

// ExportLectureText writes the lecture text to path, chosen by the user in the save dialog, in format f; times adds
// the start of each block. The file is written atomically; on a failure nothing remains under that name.
func (s *Service) ExportLectureText(ctx context.Context, id shared.RecordingID, f summaryexport.Format, times bool, path string) error {
	if err := f.Validate(); err != nil || path == "" {
		return ErrInvalidRequest
	}
	text, err := s.renderLecture(ctx, id, f, times)
	if err != nil {
		return err
	}
	if err := s.writer.WriteFile(ctx, path, []byte(text)); err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return nil
}

// renderLecture renders the lecture text of the recording in format f; no text is ErrNoLectureText.
func (s *Service) renderLecture(ctx context.Context, id shared.RecordingID, f summaryexport.Format, times bool) (string, error) {
	if err := f.Validate(); err != nil {
		return "", ErrInvalidRequest
	}
	r, err := s.load(ctx, id)
	if err != nil {
		return "", err
	}
	if r.lecture == nil || !r.lecture.Ready() {
		return "", ErrNoLectureText
	}

	doc := summaryexport.LectureDocument{
		Title: s.title(r), Start: r.rec.StartedAt().In(s.clock.Now().Location()), Blocks: r.lecture.Blocks(), Times: times,
	}
	if d, ok := r.rec.Duration(); ok {
		doc.Duration = d
	}
	text, err := summaryexport.RenderLectureText(doc, f)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return text, nil
}
