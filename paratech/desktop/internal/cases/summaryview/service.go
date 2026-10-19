package summaryview

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/services/summaryexport"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

// Titles and file names (Russian UI).
const (
	adHocTitle      = "Запись %s"      // the start date and time
	adHocFileName   = "Конспект %s"    // the start date and time
	lessonFileName  = "Конспект %s %s" // the lesson title and the date
	titleTimeLayout = "02.01.2006, 15:04"
	fileDateLayout  = "2006-01-02"
	fileTimeLayout  = "2006-01-02 15-04" // no ":" in Windows file names
)

// Operation names of the log.
const (
	opLesson = "summary viewer: lesson not read"
)

// Job types of the post-processing (the jobs context keeps types open, ARCH-11).
const (
	summarizeJobType = job.Type(values.SummarizeJobType)
	extractJobType   = job.Type(values.ExtractHomeworkJobType)
)

// Service is the summary viewer (TASK-087): it shows the summary of a recording with its key points and the homework
// found in the lesson, says where the summary and the homework search stand, renders the summary for the clipboard and
// saves it to a file the user chose. Everything is local: no port talks to the network, so the summary is there
// while the Hub or the internet is down.
type Service struct {
	recordings  RecordingReader
	lessons     LessonReader
	transcripts TranscriptFinder
	summaries   SummaryReader
	assignments AssignmentLister
	jobs        JobFinder
	writer      TextWriter
	clock       Clock
	log         *slog.Logger
}

// New checks that every port is given.
func New(d Deps) (*Service, error) {
	if d.Recordings == nil || d.Lessons == nil || d.Transcripts == nil || d.Summaries == nil || d.Assignments == nil ||
		d.Jobs == nil || d.Writer == nil || d.Clock == nil {
		return nil, fmt.Errorf("%w: every port except the logger is required", ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		recordings: d.Recordings, lessons: d.Lessons, transcripts: d.Transcripts, summaries: d.Summaries,
		assignments: d.Assignments, jobs: d.Jobs, writer: d.Writer, clock: d.Clock, log: log,
	}, nil
}

// results is what is stored for a recording: its newest transcript and what was made from it.
type results struct {
	rec         *recording.Recording
	lessonTitle string
	transcript  shared.TranscriptID // empty: not transcribed
	summary     *summary.Summary    // nil: none
	assignments []*assignment.Assignment
}

// View returns the summary, the homework and where both stand.
func (s *Service) View(ctx context.Context, id shared.RecordingID) (View, error) {
	r, err := s.load(ctx, id)
	if err != nil {
		return View{}, err
	}
	d, has := r.rec.Duration()
	v := View{
		Recording: id, Lesson: r.rec.LessonID(), Title: s.title(r), StartedAt: r.rec.StartedAt(),
		Duration: d, HasDuration: has, Transcribed: r.transcript != "",
		Summary:  Summary{Step: Step{State: StateNone}},
		Homework: Homework{Step: Step{State: StateNone}, Assignments: assignmentsOf(r.assignments)},
	}
	if r.summary != nil {
		v.Summary.Ready, v.Summary.Content, v.Summary.UpdatedAt = true, r.summary.Content(), r.summary.UpdatedAt()
	}
	if !v.Transcribed {
		return v, nil
	}

	sj, err := s.job(ctx, summarizeJobType, r.transcript)
	if err != nil {
		return View{}, err
	}
	hj, err := s.job(ctx, extractJobType, r.transcript)
	if err != nil {
		return View{}, err
	}
	v.Summary.Step = stepOf(sj, v.Summary.Ready)
	if v.Summary.State == StateDone && !v.Summary.Ready {
		// Finished without a summary: it was removed since, nothing is being made.
		v.Summary.State = StateNone
	}
	v.Homework.Step = stepOf(hj, len(r.assignments) > 0)
	return v, nil
}

// Text renders the summary with its key points and the homework as plain text for the clipboard.
func (s *Service) Text(ctx context.Context, id shared.RecordingID) (string, error) {
	return s.render(ctx, id, summaryexport.FormatText)
}

// SuggestedName returns the file name the save dialog proposes: "Конспект" with the lesson title and date, or with
// the date and time of an ad-hoc recording, safe for Windows, with the extension of f.
func (s *Service) SuggestedName(ctx context.Context, id shared.RecordingID, f summaryexport.Format) (string, error) {
	if err := f.Validate(); err != nil {
		return "", ErrInvalidRequest
	}
	rec, err := s.recording(ctx, id)
	if err != nil {
		return "", err
	}
	at := rec.StartedAt().In(s.clock.Now().Location())
	if title := s.lessonTitle(ctx, rec); title != "" {
		return summaryexport.FileName(fmt.Sprintf(lessonFileName, title, at.Format(fileDateLayout)), f), nil
	}
	return summaryexport.FileName(fmt.Sprintf(adHocFileName, at.Format(fileTimeLayout)), f), nil
}

// Export writes the summary to path, chosen by the user in the save dialog, in format f. The file is written
// atomically; on a failure nothing remains under that name.
func (s *Service) Export(ctx context.Context, id shared.RecordingID, f summaryexport.Format, path string) error {
	if err := f.Validate(); err != nil || path == "" {
		return ErrInvalidRequest
	}
	text, err := s.render(ctx, id, f)
	if err != nil {
		return err
	}
	if err := s.writer.WriteFile(ctx, path, []byte(text)); err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return nil
}

// render renders the summary of the recording in format f; no summary is ErrNoSummary.
func (s *Service) render(ctx context.Context, id shared.RecordingID, f summaryexport.Format) (string, error) {
	if err := f.Validate(); err != nil {
		return "", ErrInvalidRequest
	}
	r, err := s.load(ctx, id)
	if err != nil {
		return "", err
	}
	if r.summary == nil {
		return "", ErrNoSummary
	}

	loc := s.clock.Now().Location()
	doc := summaryexport.Document{Title: s.title(r), Start: r.rec.StartedAt().In(loc), Content: r.summary.Content()}
	if d, ok := r.rec.Duration(); ok {
		doc.Duration = d
	}
	for _, a := range r.assignments {
		h := summaryexport.Homework{Title: a.Details().Title(), Description: a.Details().Description()}
		if due := a.Details().Due(); !due.IsZero() {
			h.Due = due.In(loc)
		}
		doc.Homework = append(doc.Homework, h)
	}
	text, err := summaryexport.Render(doc, f)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return text, nil
}

// load reads the recording, its newest transcript and the summary and assignments made from it.
func (s *Service) load(ctx context.Context, id shared.RecordingID) (results, error) {
	rec, err := s.recording(ctx, id)
	if err != nil {
		return results{}, err
	}
	r := results{rec: rec, lessonTitle: s.lessonTitle(ctx, rec)}

	t, err := s.transcripts.LatestByRecording(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return r, nil
	case err != nil:
		return results{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	r.transcript = t.ID()

	r.summary, err = s.summaries.Get(ctx, summary.IDOf(t.ID()))
	switch {
	case errors.Is(err, shared.ErrNotFound):
		r.summary = nil
	case err != nil:
		return results{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if r.assignments, err = s.assignments.ListByTranscript(ctx, t.ID()); err != nil {
		return results{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return r, nil
}

// recording loads the recording; a missing one is ErrRecordingNotFound.
func (s *Service) recording(ctx context.Context, id shared.RecordingID) (*recording.Recording, error) {
	if err := id.Validate(); err != nil {
		return nil, ErrInvalidRequest
	}
	rec, err := s.recordings.Get(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, ErrRecordingNotFound
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return rec, nil
}

// job returns the newest job of typ for transcript t, nil when there is none.
func (s *Service) job(ctx context.Context, typ job.Type, t shared.TranscriptID) (*job.Job, error) {
	j, err := s.jobs.LatestByKeyPrefix(ctx, typ, values.PostProcessJobKeyPrefix(string(typ), t))
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return j, nil
}

// title is the lesson title or "Запись <date>, <time>" in the clock's location.
func (s *Service) title(r results) string {
	if r.lessonTitle != "" {
		return r.lessonTitle
	}
	return fmt.Sprintf(adHocTitle, r.rec.StartedAt().In(s.clock.Now().Location()).Format(titleTimeLayout))
}

// lessonTitle is "" for an ad-hoc recording or a lesson that is gone; a failed read only costs the title.
func (s *Service) lessonTitle(ctx context.Context, rec *recording.Recording) string {
	if rec.LessonID() == "" {
		return ""
	}
	l, err := s.lessons.Get(ctx, rec.LessonID())
	if err != nil {
		if !errors.Is(err, shared.ErrNotFound) {
			s.log.Warn(opLesson, slog.String("recording", string(rec.ID())), slog.Any("cause", err))
		}
		return ""
	}
	return l.Content().Title
}

// stepOf derives the state of a step from its newest job; has tells whether a result of the step is stored. No job
// with no result is a step about to be enqueued: the jobs are enqueued right after the transcript is stored.
func stepOf(j *job.Job, has bool) Step {
	switch {
	case j == nil && has:
		return Step{State: StateDone}
	case j == nil:
		return Step{State: StateWaiting, Wait: WaitInLine}
	}

	switch j.Status() {
	case job.StatusRunning:
		return Step{State: StateRunning}
	case job.StatusCompleted:
		return Step{State: StateDone}
	case job.StatusFailed, job.StatusCancelled:
		return Step{State: StateFailed, JobError: j.LastError()}
	}
	return Step{State: StateWaiting, Wait: waitOf(j), JobError: j.LastError()}
}

// waitOf is why a waiting job waits: the cause it recorded first (a Hub that is not set up parks the job in
// WAITING_FOR_NETWORK too), then its status.
func waitOf(j *job.Job) WaitReason {
	switch j.Cause() {
	case job.CauseServiceNotConfigured:
		return WaitServiceNotConfigured
	case job.CauseServiceUnavailable:
		return WaitServiceUnavailable
	}
	switch j.Status() {
	case job.StatusRetryWait:
		return WaitRetry
	case job.StatusWaitingForPower:
		return WaitPower
	case job.StatusWaitingForNetwork:
		return WaitNetwork
	case job.StatusWaitingForQuota:
		return WaitQuota
	}
	return WaitInLine
}

func assignmentsOf(list []*assignment.Assignment) []Assignment {
	out := make([]Assignment, len(list))
	for i, a := range list {
		d := a.Details()
		out[i] = Assignment{ID: a.ID(), Title: d.Title(), Description: d.Description(), Due: d.Due(), UserEdited: a.UserEdited()}
	}
	return out
}
