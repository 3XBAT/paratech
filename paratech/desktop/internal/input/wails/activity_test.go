package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/activity"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

type fakeActivity struct {
	recs []activity.Recording
	jobs []activity.Job
	err  error
}

func (f *fakeActivity) ListRecordings(context.Context) ([]activity.Recording, error) {
	return f.recs, f.err
}

func (f *fakeActivity) ListJobs(context.Context) ([]activity.Job, error) { return f.jobs, f.err }

type fakeStopper struct {
	got       shared.RecordingID
	err       error
	active    recordingsession.Info
	running   bool
	gotSource values.Source
	calls     []string
}

func (f *fakeStopper) Stop(_ context.Context, id shared.RecordingID) (recordingsession.Info, error) {
	f.got = id
	return recordingsession.Info{ID: id}, f.err
}

func (f *fakeStopper) Active() (recordingsession.Info, bool) { return f.active, f.running }

func (f *fakeStopper) SwitchSource(_ context.Context, id shared.RecordingID, source values.Source) error {
	f.got, f.gotSource = id, source
	f.calls = append(f.calls, "switch")
	return f.err
}

func (f *fakeStopper) DismissAdvice(_ context.Context, id shared.RecordingID) error {
	f.got = id
	f.calls = append(f.calls, "dismiss")
	return f.err
}

type fakeForcer struct {
	got shared.JobID
	err error
}

func (f *fakeForcer) ForceRun(_ context.Context, id shared.JobID) error {
	f.got = id
	return f.err
}

type fakeRetrier struct {
	calls []string
	err   error
}

func (f *fakeRetrier) Retry(_ context.Context, rec shared.RecordingID, work homeworkflow.Work) (shared.JobID, error) {
	f.calls = append(f.calls, "retry "+string(rec)+" "+string(work))
	return "j9", f.err
}

func (f *fakeRetrier) RedoSummary(_ context.Context, rec shared.RecordingID) (shared.JobID, error) {
	f.calls = append(f.calls, "redo "+string(rec))
	return "j9", f.err
}

// A non-UTC zone checks that the page gets UTC times.
var at = time.Date(2026, 10, 15, 9, 30, 0, 0, time.FixedZone("MSK", 3*3600))

// The page reads these names (frontend/src/bindings/activity.ts). TASK-076 added the lesson title and the chain
// status fields to every row, TASK-088 the subject, kind and teacher of the lesson.
func TestActivityServiceListsRecordings(t *testing.T) {
	uc := &fakeActivity{recs: []activity.Recording{
		{
			ID: "r1", LessonID: "l1", LessonTitle: "История, лекция", LessonSubject: "История", LessonKind: "lecture",
			LessonTeacher: "Петров", Status: recording.StatusRecording, StartedAt: at,
			Stoppable: true, Chain: activity.Chain{Stage: activity.StageRecording},
		},
		{
			ID: "r2", Status: recording.StatusCompleted, StartedAt: at, StoppedAt: at.Add(time.Hour),
			Duration: 59*time.Minute + 1500*time.Millisecond, HasDuration: true, Problem: "limit reached",
			Chain: activity.Chain{
				Stage: activity.StageQueued, Step: activity.StepTranscription, Percent: 40, QueueReason: activity.QueuePower,
				JobError: "on battery", Action: activity.ActionRunNow, ActionJob: "j1",
			},
		},
		{
			ID: "r3", Status: recording.StatusCompleted, StartedAt: at, Chain: activity.Chain{
				Stage: activity.StageFailed, Step: activity.StepSummary, ErrorCode: activity.ErrorSummaryFailed,
				Action: activity.ActionOpenConnections, Retry: activity.WorkSummary, CanRedoSummary: true,
			},
		},
	}}
	got, err := NewActivityService(uc, &fakeStopper{}, &fakeForcer{}, &fakeRetrier{}, discard()).ListRecordings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `[` +
		`{"id":"r1","lessonId":"l1","title":"История, лекция","subject":"История","kind":"lecture","teacher":"Петров",` +
		`"status":"RECORDING",` +
		`"startedAt":"2026-10-15T06:30:00Z","stoppedAt":"","durationSeconds":0,"hasDuration":false,"problem":"",` +
		`"stoppable":true,"stage":"recording","step":"","progressPercent":0,"queueReason":"","errorCode":"",` +
		`"jobError":"","action":"","actionJobId":"","retryWork":"","canRedoSummary":false,"remainingSeconds":0,"hasRemaining":false,"advice":null},` +
		`{"id":"r2","lessonId":"","title":"","subject":"","kind":"","teacher":"","status":"COMPLETED","startedAt":"2026-10-15T06:30:00Z",` +
		`"stoppedAt":"2026-10-15T07:30:00Z","durationSeconds":3542,"hasDuration":true,"problem":"limit reached",` +
		`"stoppable":false,"stage":"queued","step":"transcription","progressPercent":40,"queueReason":"power",` +
		`"errorCode":"","jobError":"on battery","action":"runNow","actionJobId":"j1","retryWork":"","canRedoSummary":false,"remainingSeconds":0,"hasRemaining":false,"advice":null},` +
		`{"id":"r3","lessonId":"","title":"","subject":"","kind":"","teacher":"","status":"COMPLETED","startedAt":"2026-10-15T06:30:00Z","stoppedAt":"",` +
		`"durationSeconds":0,"hasDuration":false,"problem":"","stoppable":false,"stage":"failed","step":"summary",` +
		`"progressPercent":0,"queueReason":"","errorCode":"summaryFailed","jobError":"","action":"openConnections",` +
		`"actionJobId":"","retryWork":"summary","canRedoSummary":true,"remainingSeconds":0,"hasRemaining":false,"advice":null}]`
	if string(b) != want {
		t.Fatalf("JSON =\n%s\nwant\n%s", b, want)
	}
}

func TestActivityServiceListsJobs(t *testing.T) {
	uc := &fakeActivity{jobs: []activity.Job{{
		ID: "j1", Type: "stt", Status: job.StatusWaitingForPower, Waiting: true, Attempts: 1, MaxAttempts: 3, Percent: 40,
		LastError: "net", CreatedAt: at, UpdatedAt: at.Add(time.Minute), NextRunAt: at.Add(2 * time.Minute),
		RunNowOffered: true,
	}}}
	got, err := NewActivityService(uc, &fakeStopper{}, &fakeForcer{}, &fakeRetrier{}, discard()).ListJobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"id":"j1","type":"stt","status":"WAITING_FOR_POWER","waiting":true,"attempts":1,"maxAttempts":3,` +
		`"progressPercent":40,"lastError":"net","createdAt":"2026-10-15T06:30:00Z","updatedAt":"2026-10-15T06:31:00Z",` +
		`"nextRunAt":"2026-10-15T06:32:00Z","runNowOffered":true}]`
	if string(b) != want {
		t.Fatalf("JSON =\n%s\nwant\n%s", b, want)
	}
}

// An empty history is an empty list on the page, not null.
func TestActivityServiceEmptyListsAreArrays(t *testing.T) {
	s := NewActivityService(&fakeActivity{}, &fakeStopper{}, &fakeForcer{}, &fakeRetrier{}, discard())
	recs, err := s.ListRecordings(context.Background())
	if b, _ := json.Marshal(recs); err != nil || string(b) != "[]" {
		t.Errorf("ListRecordings = %s, %v", b, err)
	}
	jobs, err := s.ListJobs(context.Background())
	if b, _ := json.Marshal(jobs); err != nil || string(b) != "[]" {
		t.Errorf("ListJobs = %s, %v", b, err)
	}
}

func TestActivityServiceHidesListErrors(t *testing.T) {
	var logs bytes.Buffer
	secret := errors.New(`open C:\Users\student\db: locked`)
	s := NewActivityService(&fakeActivity{err: secret}, &fakeStopper{}, &fakeForcer{}, &fakeRetrier{}, slog.New(slog.NewTextHandler(&logs, nil)))

	if _, err := s.ListRecordings(context.Background()); !errors.Is(err, ErrActivityUnavailable) || strings.Contains(err.Error(), "student") {
		t.Errorf("ListRecordings err = %v", err)
	}
	if _, err := s.ListJobs(context.Background()); !errors.Is(err, ErrActivityUnavailable) || strings.Contains(err.Error(), "student") {
		t.Errorf("ListJobs err = %v", err)
	}
	if strings.Count(logs.String(), "locked") != 2 {
		t.Errorf("the causes must be logged, got %q", logs.String())
	}
}

// checkActionError asserts that the page sees exactly want (nil for success) and nothing of the cause.
func checkActionError(t *testing.T, err, want error) {
	t.Helper()
	if want == nil {
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		return
	}
	if err == nil || err.Error() != want.Error() {
		t.Fatalf("err = %v, want only %q", err, want)
	}
}

func TestActivityServiceStopRecording(t *testing.T) {
	var logs bytes.Buffer
	secret := errors.New(`C:\Users\student\recordings: access denied`)
	tests := []struct {
		name    string
		id      string
		stopper *fakeStopper
		want    error
	}{
		{name: "stopped", id: "r1", stopper: &fakeStopper{}},
		{name: "not running here", id: "r1", stopper: &fakeStopper{err: fmt.Errorf("stop r1: %w", recordingsession.ErrNotRunning)}, want: ErrRecordingNotRunning},
		{name: "unknown", id: "r1", stopper: &fakeStopper{err: fmt.Errorf("stop r1: %w", shared.ErrNotFound)}, want: ErrItemNotFound},
		{name: "other failure", id: "r1", stopper: &fakeStopper{err: secret}, want: ErrActionFailed},
		{name: "empty id", id: "", stopper: &fakeStopper{}, want: ErrInvalidID},
		{name: "long id", id: strings.Repeat("a", 129), stopper: &fakeStopper{}, want: ErrInvalidID},
		{name: "path id", id: `..\db`, stopper: &fakeStopper{}, want: ErrInvalidID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewActivityService(&fakeActivity{}, tt.stopper, &fakeForcer{}, &fakeRetrier{}, slog.New(slog.NewTextHandler(&logs, nil)))
			checkActionError(t, s.StopRecording(context.Background(), tt.id), tt.want)

			switch {
			case errors.Is(tt.want, ErrInvalidID) && tt.stopper.got != "":
				t.Fatalf("an invalid id reached the stopper: %q", tt.stopper.got)
			case !errors.Is(tt.want, ErrInvalidID) && tt.stopper.got != shared.RecordingID(tt.id):
				t.Fatalf("stopped %q, want %q", tt.stopper.got, tt.id)
			}
		})
	}
	if !strings.Contains(logs.String(), "access denied") {
		t.Errorf("the cause of a failed stop must be logged, got %q", logs.String())
	}
}

func TestActivityServiceRunJobNow(t *testing.T) {
	tests := []struct {
		name   string
		id     string
		forcer *fakeForcer
		want   error
	}{
		{name: "forced", id: "j1", forcer: &fakeForcer{}},
		{name: "not waiting for power", id: "j1", forcer: &fakeForcer{err: fmt.Errorf("%w: RUNNING", jobrunner.ErrCannotRunNow)}, want: ErrCannotRunNow},
		{name: "unknown", id: "j1", forcer: &fakeForcer{err: shared.ErrNotFound}, want: ErrItemNotFound},
		{name: "other failure", id: "j1", forcer: &fakeForcer{err: errors.New("database is locked")}, want: ErrActionFailed},
		{name: "empty id", id: "", forcer: &fakeForcer{}, want: ErrInvalidID},
		{name: "long id", id: strings.Repeat("j", 129), forcer: &fakeForcer{}, want: ErrInvalidID},
		{name: "control character", id: "j\n1", forcer: &fakeForcer{}, want: ErrInvalidID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewActivityService(&fakeActivity{}, &fakeStopper{}, tt.forcer, &fakeRetrier{}, discard())
			checkActionError(t, s.RunJobNow(context.Background(), tt.id), tt.want)

			switch {
			case errors.Is(tt.want, ErrInvalidID) && tt.forcer.got != "":
				t.Fatalf("an invalid id reached the forcer: %q", tt.forcer.got)
			case !errors.Is(tt.want, ErrInvalidID) && tt.forcer.got != shared.JobID(tt.id):
				t.Fatalf("forced %q, want %q", tt.forcer.got, tt.id)
			}
		})
	}
}

// The page may match the error messages, so the frontend wrapper lists each one.
func TestFrontendKnowsActivityErrors(t *testing.T) {
	src, err := os.ReadFile("../../../frontend/src/bindings/activity.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []error{
		ErrActivityUnavailable, ErrInvalidID, ErrItemNotFound, ErrRecordingNotRunning, ErrCannotRunNow, ErrActionFailed,
		ErrInvalidWork, ErrAlreadyRunning, ErrNothingToRetry,
	} {
		if !strings.Contains(string(src), `"`+e.Error()+`"`) {
			t.Errorf("frontend/src/bindings/activity.ts does not list the error %q", e)
		}
	}
}

// The page parses the chain codes strictly (TASK-076), so the frontend wrapper must list every code the core sends.
func TestFrontendKnowsChainCodes(t *testing.T) {
	src, err := os.ReadFile("../../../frontend/src/bindings/activity.ts")
	if err != nil {
		t.Fatal(err)
	}
	codes := []string{
		string(activity.StageRecording), string(activity.StageSaving), string(activity.StageQueued),
		string(activity.StageTranscribing), string(activity.StageSummarizing), string(activity.StageDone),
		string(activity.StageFailed),
		string(activity.StepTranscription), string(activity.StepSummary),
		string(activity.QueueInLine), string(activity.QueueRetry), string(activity.QueuePower), string(activity.QueueNetwork),
		string(activity.QueueQuota), string(activity.QueueServiceNotConfigured), string(activity.QueueServiceUnavailable),
		string(activity.ErrorRecordingFailed), string(activity.ErrorTranscriptionFailed),
		string(activity.ErrorSummaryFailed), string(activity.ErrorHomeworkFailed),
		string(activity.ActionRunNow), string(activity.ActionOpenModels), string(activity.ActionOpenConnections),
		string(activity.ActionCheckSound),
	}
	for _, c := range codes {
		if !strings.Contains(string(src), `"`+c+`"`) {
			t.Errorf("frontend/src/bindings/activity.ts does not list the chain code %q", c)
		}
	}
}

// TASK-086: «Повторить» and «Сделать конспект заново» of a recording row.
func TestActivityServiceRetry(t *testing.T) {
	tests := []struct {
		name string
		call func(s *ActivityService) error
		err  error
		want error
		got  []string
	}{
		{name: "retry summary", call: func(s *ActivityService) error {
			return s.RetryPostProcessing(context.Background(), "r1", "summary")
		}, got: []string{"retry r1 summary"}},
		{name: "retry homework", call: func(s *ActivityService) error {
			return s.RetryPostProcessing(context.Background(), "r1", "homework")
		}, got: []string{"retry r1 homework"}},
		{name: "redo summary", call: func(s *ActivityService) error {
			return s.RedoSummary(context.Background(), "r1")
		}, got: []string{"redo r1"}},
		{name: "unknown work", call: func(s *ActivityService) error {
			return s.RetryPostProcessing(context.Background(), "r1", "draft")
		}, want: ErrInvalidWork},
		{name: "invalid id", call: func(s *ActivityService) error {
			return s.RetryPostProcessing(context.Background(), "r\n1", "summary")
		}, want: ErrInvalidID},
		{name: "invalid id of redo", call: func(s *ActivityService) error { return s.RedoSummary(context.Background(), "") },
			want: ErrInvalidID},
		{name: "running", call: func(s *ActivityService) error {
			return s.RetryPostProcessing(context.Background(), "r1", "summary")
		}, err: fmt.Errorf("x: %w: RUNNING", job.ErrStillActive), want: ErrAlreadyRunning, got: []string{"retry r1 summary"}},
		{name: "redo while queued", call: func(s *ActivityService) error { return s.RedoSummary(context.Background(), "r1") },
			err: fmt.Errorf("x: %w: QUEUED", job.ErrStillActive), want: ErrAlreadyRunning, got: []string{"redo r1"}},
		{name: "completed", call: func(s *ActivityService) error {
			return s.RetryPostProcessing(context.Background(), "r1", "homework")
		}, err: job.ErrNotFailed, want: ErrNothingToRetry, got: []string{"retry r1 homework"}},
		{name: "no summary", call: func(s *ActivityService) error { return s.RedoSummary(context.Background(), "r1") },
			err: shared.ErrNotFound, want: ErrItemNotFound, got: []string{"redo r1"}},
		{name: "storage", call: func(s *ActivityService) error { return s.RedoSummary(context.Background(), "r1") },
			err: errors.New(`open C:\Users\student\db: locked`), want: ErrActionFailed, got: []string{"redo r1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeRetrier{err: tt.err}
			s := NewActivityService(&fakeActivity{}, &fakeStopper{}, &fakeForcer{}, r, discard())
			checkActionError(t, tt.call(s), tt.want)
			if strings.Join(r.calls, ";") != strings.Join(tt.got, ";") {
				t.Fatalf("calls = %v, want %v", r.calls, tt.got)
			}
		})
	}
}
