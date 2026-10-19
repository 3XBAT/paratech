package wails

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"paratech/desktop/internal/cases/activity"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/domain/shared"
)

// Errors of the recordings and jobs page. The page sees only these messages (frontend/src/bindings/activity.ts
// matches them); causes are logged, they may name local paths or database details.
var (
	ErrActivityUnavailable = errors.New("recordings and jobs are unavailable")
	ErrInvalidID           = errors.New("invalid id")
	ErrItemNotFound        = errors.New("not found")
	ErrRecordingNotRunning = errors.New("the recording is not running in this session")
	ErrCannotRunNow        = errors.New("the job cannot be run now")
	ErrActionFailed        = errors.New("the action failed")
)

// ActivityUseCase is the read side the recordings and jobs page needs (ARCH-42).
type ActivityUseCase interface {
	ListRecordings(ctx context.Context) ([]activity.Recording, error)
	ListJobs(ctx context.Context) ([]activity.Job, error)
}

// RecordingStopper stops a recording; satisfied by recordingsession.Service.
type RecordingStopper interface {
	Stop(ctx context.Context, id shared.RecordingID) (recordingsession.Info, error)
}

// JobForcer is the "Run now" action; satisfied by jobrunner.Service.
type JobForcer interface {
	ForceRun(ctx context.Context, id shared.JobID) error
}

// RecordingItem is the JSON form of one recording with the status of its whole chain (TASK-076). Times are RFC 3339
// in UTC, empty when unknown. The codes are the activity constants; the page maps them to its texts.
type RecordingItem struct {
	ID       string `json:"id"`
	LessonID string `json:"lessonId"`
	// Title is the lesson title (imported text: render as plain text only); "" without a lesson, and the page then
	// titles the row «Запись <дата, время>» from startedAt.
	Title     string `json:"title"`
	Status    string `json:"status"`
	StartedAt string `json:"startedAt"`
	StoppedAt string `json:"stoppedAt"`
	// DurationSeconds is the final duration; hasDuration is false until the recording is COMPLETED.
	DurationSeconds int64  `json:"durationSeconds"`
	HasDuration     bool   `json:"hasDuration"`
	Problem         string `json:"problem"`
	Stoppable       bool   `json:"stoppable"`

	// Stage: recording, saving, queued, transcribing, summarizing, done or failed.
	Stage string `json:"stage"`
	// Step of a queued or failed chain: "", "transcription" or "summary".
	Step string `json:"step"`
	// ProgressPercent of the transcription, 0..100.
	ProgressPercent int `json:"progressPercent"`
	// QueueReason of a queued chain: inLine, retry, power, network, quota, serviceNotConfigured (the Hub has no address
	// or key; action openConnections) or serviceUnavailable (the Hub does not answer; retried automatically).
	QueueReason string `json:"queueReason"`
	// ErrorCode of a failed chain: recordingFailed, transcriptionFailed, summaryFailed or homeworkFailed.
	ErrorCode string `json:"errorCode"`
	// JobError is the last error of the job the stage refers to (failed or retrying); "" otherwise.
	JobError string `json:"jobError"`
	// Action the row offers besides Stop: "", runNow (RunJobNow with actionJobId), openModels, openConnections or
	// checkSound.
	Action      string `json:"action"`
	ActionJobID string `json:"actionJobId"`
}

// JobItem is the JSON form of one job. Times are RFC 3339 in UTC, empty when unknown.
type JobItem struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	Status          string `json:"status"`
	Waiting         bool   `json:"waiting"`
	Attempts        int    `json:"attempts"`
	MaxAttempts     int    `json:"maxAttempts"`
	ProgressPercent int    `json:"progressPercent"`
	LastError       string `json:"lastError"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
	NextRunAt       string `json:"nextRunAt"`
	RunNowOffered   bool   `json:"runNowOffered"`
}

// ActivityService is bound to the UI like SettingsService ("<package path>.ActivityService.<Method>").
type ActivityService struct {
	uc      ActivityUseCase
	stopper RecordingStopper
	forcer  JobForcer
	log     *slog.Logger
}

// NewActivityService returns the binding of the recordings and jobs page. All arguments are required; OpenServices
// checks the core before calling it.
func NewActivityService(uc ActivityUseCase, stopper RecordingStopper, forcer JobForcer, log *slog.Logger) *ActivityService {
	return &ActivityService{uc: uc, stopper: stopper, forcer: forcer, log: log}
}

// ListRecordings returns the newest recordings, newest first, one row per recording with its chain status.
func (s *ActivityService) ListRecordings(ctx context.Context) ([]RecordingItem, error) {
	recs, err := s.uc.ListRecordings(ctx)
	if err != nil {
		s.log.Error("ui: list recordings", "err", err)
		return nil, ErrActivityUnavailable
	}
	out := make([]RecordingItem, 0, len(recs))
	for _, r := range recs {
		c := r.Chain
		out = append(out, RecordingItem{
			ID: string(r.ID), LessonID: string(r.LessonID), Title: r.LessonTitle, Status: string(r.Status),
			StartedAt: timeText(r.StartedAt), StoppedAt: timeText(r.StoppedAt),
			DurationSeconds: int64(r.Duration.Round(time.Second) / time.Second), HasDuration: r.HasDuration,
			Problem: r.Problem, Stoppable: r.Stoppable,
			Stage: string(c.Stage), Step: string(c.Step), ProgressPercent: c.Percent, QueueReason: string(c.QueueReason),
			ErrorCode: string(c.ErrorCode), JobError: c.JobError, Action: string(c.Action), ActionJobID: string(c.ActionJob),
		})
	}
	return out, nil
}

// ListJobs returns the newest jobs, newest first, without the service jobs and the jobs of the recording chain
// (their recording rows show them).
func (s *ActivityService) ListJobs(ctx context.Context) ([]JobItem, error) {
	jobs, err := s.uc.ListJobs(ctx)
	if err != nil {
		s.log.Error("ui: list jobs", "err", err)
		return nil, ErrActivityUnavailable
	}
	out := make([]JobItem, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, JobItem{
			ID: string(j.ID), Type: string(j.Type), Status: string(j.Status), Waiting: j.Waiting,
			Attempts: j.Attempts, MaxAttempts: j.MaxAttempts, ProgressPercent: j.Percent, LastError: j.LastError,
			CreatedAt: timeText(j.CreatedAt), UpdatedAt: timeText(j.UpdatedAt), NextRunAt: timeText(j.NextRunAt),
			RunNowOffered: j.RunNowOffered,
		})
	}
	return out, nil
}

// StopRecording stops the recording id and returns once it is finished.
func (s *ActivityService) StopRecording(ctx context.Context, id string) error {
	rid := shared.RecordingID(id)
	if rid.Validate() != nil {
		return ErrInvalidID
	}
	_, err := s.stopper.Stop(ctx, rid)
	if errors.Is(err, recordingsession.ErrNotRunning) {
		return ErrRecordingNotRunning
	}
	return s.actionError("stop recording", err)
}

// RunJobNow starts a job waiting for AC power at once (PRD section 7.4, AC-22).
func (s *ActivityService) RunJobNow(ctx context.Context, id string) error {
	jid := shared.JobID(id)
	if jid.Validate() != nil {
		return ErrInvalidID
	}
	err := s.forcer.ForceRun(ctx, jid)
	if errors.Is(err, jobrunner.ErrCannotRunNow) {
		return ErrCannotRunNow
	}
	return s.actionError("run job now", err)
}

func (s *ActivityService) actionError(op string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, shared.ErrNotFound):
		return ErrItemNotFound
	}
	s.log.Error("ui: "+op, "err", err)
	return ErrActionFailed
}

// timeText formats t for the page; the zero time is empty.
func timeText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
