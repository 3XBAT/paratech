package wails

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"paratech/desktop/internal/cases/activity"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
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

// RecordingStopper controls the running recording: stop, the silent-source advice and the switch of its source
// (TASK-082); satisfied by recordingsession.Service.
type RecordingStopper interface {
	Stop(ctx context.Context, id shared.RecordingID) (recordingsession.Info, error)
	Active() (recordingsession.Info, bool)
	SwitchSource(ctx context.Context, id shared.RecordingID, source values.Source) error
	DismissAdvice(ctx context.Context, id shared.RecordingID) error
}

// Problems of SourceSwitch.
const (
	switchSourceGone = "source_gone" // the chosen app does not run
	switchRefused    = "refused"     // the recording cannot switch now (stopping, or another switch runs)
	switchFailed     = "failed"      // the capture of the new source did not start
)

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
	// titles the row «Запись <дата, время>» from startedAt. A recording keeps it from its start (TASK-088).
	Title string `json:"title"`
	// Subject, Kind and Teacher of the lesson as kept at the start (TASK-088), "" if unknown: «Матанализ · лекция».
	// Kind is a short code (lecture, seminar, ...); Subject and Teacher are imported text: plain text only.
	Subject   string `json:"subject"`
	Kind      string `json:"kind"`
	Teacher   string `json:"teacher"`
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
	// ErrorCode of a failed chain: recordingFailed, transcriptionFailed, summaryFailed, homeworkFailed or
	// silentRecording (the track is silent: «Запись без звука», TASK-082).
	ErrorCode string `json:"errorCode"`
	// JobError is the last error of the job the stage refers to (failed or retrying); "" otherwise.
	JobError string `json:"jobError"`
	// Action the row offers besides Stop: "", runNow (RunJobNow with actionJobId), openModels, openConnections or
	// checkSound.
	Action      string `json:"action"`
	ActionJobID string `json:"actionJobId"`

	// RemainingSeconds is the approximate time left of a queued or running transcription («≈ N мин осталось»,
	// TASK-084); hasRemaining is false when there is none or no estimate.
	RemainingSeconds int64 `json:"remainingSeconds"`
	HasRemaining     bool  `json:"hasRemaining"`

	// Advice is the silent-source advice of the running recording (TASK-082); null when none.
	Advice *AdviceView `json:"advice"`
}

// AdviceView is the silent-source advice (TASK-082): «В <silentName> нет звука, звук идёт из <suggestName>.
// Переключиться?»; for the whole computer «…звук идёт из других приложений… Записывать весь звук компьютера?». The
// names are untrusted text: render as plain text only; the exe names belong in a tooltip.
type AdviceView struct {
	RecordingID string `json:"recordingId"`
	SilentApp   string `json:"silentApp"`   // exe name of the chosen app
	SilentName  string `json:"silentName"`  // its readable name
	SuggestApp  string `json:"suggestApp"`  // exe name to switch to; "" = the whole computer
	SuggestName string `json:"suggestName"` // its readable name; "" for the whole computer
}

// SourceSwitch is the answer of SwitchSource.
type SourceSwitch struct {
	Switched bool `json:"switched"`
	// Problem says why not: "source_gone" (the app does not run any more), "refused" (the recording cannot switch
	// now) or "failed" (the capture of the new source did not start); "" once switched. The recording goes on with its
	// old source either way.
	Problem string `json:"problem"`
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
	active, running := s.stopper.Active()
	out := make([]RecordingItem, 0, len(recs))
	for _, r := range recs {
		c := r.Chain
		out = append(out, RecordingItem{
			ID: string(r.ID), LessonID: string(r.LessonID), Title: r.LessonTitle, Subject: r.LessonSubject,
			Kind: r.LessonKind, Teacher: r.LessonTeacher, Status: string(r.Status),
			StartedAt: timeText(r.StartedAt), StoppedAt: timeText(r.StoppedAt),
			DurationSeconds: int64(r.Duration.Round(time.Second) / time.Second), HasDuration: r.HasDuration,
			Problem: r.Problem, Stoppable: r.Stoppable,
			Stage: string(c.Stage), Step: string(c.Step), ProgressPercent: c.Percent, QueueReason: string(c.QueueReason),
			ErrorCode: string(c.ErrorCode), JobError: c.JobError, Action: string(c.Action), ActionJobID: string(c.ActionJob),
			RemainingSeconds: int64(r.Remaining.Round(time.Second) / time.Second), HasRemaining: r.HasRemaining,
		})
		if running && active.ID == r.ID {
			out[len(out)-1].Advice = adviceView(active.ID, active.Advice)
		}
	}
	return out, nil
}

// adviceView is the JSON form of an advice; nil for none.
func adviceView(id shared.RecordingID, a values.SourceAdvice) *AdviceView {
	if a.IsZero() {
		return nil
	}
	return &AdviceView{
		RecordingID: string(id), SilentApp: a.Silent.String(), SilentName: a.SilentName,
		SuggestApp: a.Suggest.App().String(), SuggestName: a.SuggestName,
	}
}

// SwitchSource switches the running recording id to app (an exe name; "" = the whole computer): the advice
// «Переключиться» (TASK-082). The same recording goes on with the new source. A switch that cannot be done is no
// error: the answer carries its problem and the recording keeps its source.
func (s *ActivityService) SwitchSource(ctx context.Context, id, app string) (SourceSwitch, error) {
	rid := shared.RecordingID(id)
	if rid.Validate() != nil {
		return SourceSwitch{}, ErrInvalidID
	}
	source, err := recordingSourceOf(app)
	if err != nil {
		return SourceSwitch{}, err
	}
	err = s.stopper.SwitchSource(ctx, rid, source)
	switch {
	case err == nil:
		return SourceSwitch{Switched: true}, nil
	case errors.Is(err, recording.ErrSourceNotRunning): // the use case logged it
		return SourceSwitch{Problem: switchSourceGone}, nil
	case errors.Is(err, recordingsession.ErrSwitchFailed):
		return SourceSwitch{Problem: switchFailed}, nil
	case errors.Is(err, recordingsession.ErrSwitchRefused):
		return SourceSwitch{Problem: switchRefused}, nil
	case errors.Is(err, recordingsession.ErrNotRunning):
		return SourceSwitch{}, ErrRecordingNotRunning
	}
	return SourceSwitch{}, s.actionError("switch the recording source", err)
}

// DismissAdvice hides the advice of the running recording id until it changes («Оставить», TASK-082).
func (s *ActivityService) DismissAdvice(ctx context.Context, id string) error {
	rid := shared.RecordingID(id)
	if rid.Validate() != nil {
		return ErrInvalidID
	}
	err := s.stopper.DismissAdvice(ctx, rid)
	if errors.Is(err, recordingsession.ErrNotRunning) {
		return ErrRecordingNotRunning
	}
	return s.actionError("dismiss the source advice", err)
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
