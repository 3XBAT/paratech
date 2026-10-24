package app

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/componentstore"
	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/googlecal"
	"paratech/desktop/internal/adapters/httpdownload"
	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/adapters/localfile"
	"paratech/desktop/internal/adapters/smtp"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/adapters/wincred"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/modelcatalog"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/cases/speechcomponent"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/cases/turnkey"
	"paratech/desktop/internal/domain/jobs/job"
	tvalues "paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

// The adapters and use cases fit the ports they are wired to, including those TASK-057 wires.
var (
	_ homeworkflow.LLMClient                = (*hubclient.Client)(nil)
	_ hubclient.Credentials                 = (*wincred.Store)(nil)
	_ smtp.SecretReader                     = (*wincred.Store)(nil)
	_ senddelivery.CredentialStore          = (*wincred.Store)(nil)
	_ senddelivery.EmailAdapter             = (*smtp.Mailer)(nil)
	_ transcribe.EventPublisher             = (*homeworkflow.PostProcessor)(nil)
	_ jobrunner.Handler                     = (*homeworkflow.Drafts)(nil)
	_ deliveryJobs                          = (*senddelivery.Service)(nil)
	_ recordingsession.ArtifactStore        = (*filestore.Store)(nil)
	_ wails.RecordingStopper                = (*recordingsession.Service)(nil)
	_ wails.JobForcer                       = (*jobrunner.Service)(nil)
	_ wails.PostProcessRetrier              = (*homeworkflow.Rerunner)(nil)
	_ homeworkflow.PostJobs                 = (*sqlite.JobRepository)(nil)
	_ homeworkflow.LatestTranscripts        = (*sqlite.TranscriptRepository)(nil)
	_ homeworkflow.DraftRepository          = (*sqlite.DraftRepository)(nil)
	_ hubclient.URLSource                   = hubSettings{}
	_ smtp.AccountSource                    = mailSettings{}
	_ homeworkflow.RecipientSource          = recipientSettings{}
	_ homeworkflow.ProposalRepository       = (*sqlite.ProposalRepository)(nil)
	_ homeworkflow.HomeworkMailer           = (*senddelivery.Service)(nil)
	_ homeworkflow.HomeworkExporter         = (*filestore.Store)(nil)
	_ schedulesync.JobEnqueuer              = (*sqlite.JobRepository)(nil)
	_ schedulesync.LessonRepository         = (*sqlite.LessonRepository)(nil)
	_ schedulesync.IDGenerator              = idSource(nil)
	_ googlecal.Store                       = secretStore(nil)
	_ settingsLoader                        = (*sqlite.SettingsRepository)(nil)
	_ secretStore                           = (*wincred.Store)(nil)
	_ secretStore                           = (*testenv.MemCredentials)(nil)
	_ recordingsession.CompletionListener   = (*transcribe.Enqueuer)(nil)
	_ transcribe.JobEnqueuer                = (*sqlite.JobRepository)(nil)
	_ transcribe.PendingRecordings          = (*sqlite.RecordingRepository)(nil)
	_ transcribe.ModelResolver              = modelResolver{}
	_ transcribe.IDGenerator                = idSource(nil)
	_ transcribe.Tx                         = (*sqlite.DB)(nil)
	_ modelcatalog.IDGenerator              = idSource(nil)
	_ modelcatalog.CatalogSource            = embeddedCatalog{}
	_ modelcatalog.ModelSource              = (*httpdownload.Client)(nil)
	_ modelcatalog.ImportSource             = (*localfile.Reader)(nil)
	_ modelcatalog.DiskSpace                = (*filestore.Store)(nil)
	_ speechcomponent.ArchiveSource         = (*httpdownload.Client)(nil)
	_ speechcomponent.DiskSpace             = (*filestore.Store)(nil)
	_ speechcomponent.ComponentFiles        = (*componentstore.Store)(nil)
	_ speechcomponent.Activator             = (*speechActivator)(nil)
	_ wails.SpeechComponentUseCase          = (*speechcomponent.Service)(nil)
	_ recordingsession.IDGenerator          = idSource(nil)
	_ homeworkflow.IDGenerator              = idSource(nil)
	_ senddelivery.IDGenerator              = idSource(nil)
	_ recordingsession.Sleeper              = timeSource(nil)
	_ homeworkflow.Clock                    = localTime(nil)
	_ jobrunner.Sleeper                     = timeSource(nil)
	_ recordingsession.RecordingTranscripts = (*sqlite.RecordingTranscripts)(nil)
)

func handlersForTest() jobHandlers {
	var s *senddelivery.Service
	return jobHandlers{
		stt: &transcribe.Service{}, postProcess: &homeworkflow.PostProcessor{}, drafts: &homeworkflow.Drafts{}, delivery: s,
		calendar: &schedulesync.CalendarSync{},
	}
}

func registrationOf(t *testing.T, regs []jobrunner.Registration, typ job.Type) jobrunner.Registration {
	t.Helper()
	for _, r := range regs {
		if r.Type == typ {
			return r
		}
	}
	t.Fatalf("no registration for %s", typ)
	return jobrunner.Registration{}
}

func TestRegistrationPolicies(t *testing.T) {
	regs := registrations(handlersForTest())
	tests := []struct {
		typ           job.Type
		heavy         bool
		recovery      jobrunner.Recovery
		reconciler    bool
		uncancellable bool
	}{
		{typ: transcribe.JobType, heavy: true, recovery: jobrunner.RecoveryRequeue},
		{typ: homeworkflow.SummarizeJobType, recovery: jobrunner.RecoveryRequeue},
		{typ: homeworkflow.ExtractHomeworkJobType, recovery: jobrunner.RecoveryRequeue},
		// TASK-090: light like the other LLM jobs; an interrupted attempt continues from the parts it stored.
		{typ: homeworkflow.LectureTextJobType, recovery: jobrunner.RecoveryRequeue},
		// TASK-122: the work on a library material runs like the post-processing of a transcript.
		{typ: homeworkflow.MaterialSummaryJobType, recovery: jobrunner.RecoveryRequeue},
		{typ: homeworkflow.MaterialHomeworkJobType, recovery: jobrunner.RecoveryRequeue},
		{typ: homeworkflow.HomeworkDraftJobType, recovery: jobrunner.RecoveryRequeue},
		// Hard rule 7: the side effect is never Heavy, never requeued blindly and not cancelled mid-send.
		{typ: senddelivery.JobType, recovery: jobrunner.RecoveryReconcile, reconciler: true, uncancellable: true},
		// Idempotent writes by stable event ids: requeued after an interruption, never Heavy.
		{typ: schedulesync.CalendarSyncJobType, recovery: jobrunner.RecoveryRequeue},
	}
	for _, tt := range tests {
		t.Run(string(tt.typ), func(t *testing.T) {
			r := registrationOf(t, regs, tt.typ)
			if r.Heavy != tt.heavy || r.Recovery != tt.recovery || (r.Reconciler != nil) != tt.reconciler ||
				r.Uncancellable != tt.uncancellable || r.Handler == nil || r.Timeout <= 0 {
				t.Fatalf("registration = %+v, want %+v", r, tt)
			}
		})
	}

	if got := registrationOf(t, regs, senddelivery.JobType).Timeout; got < 2*smtp.DefaultTimeout {
		t.Errorf("send_email timeout %v, want at least twice the SMTP timeout %v", got, smtp.DefaultTimeout)
	}
	if got := registrationOf(t, regs, schedulesync.CalendarSyncJobType).Timeout; got < 10*googlecal.DefaultRequestTimeout {
		t.Errorf("calendar_sync timeout %v, want room for many Calendar API requests", got)
	}
	for _, tt := range []struct {
		typ   job.Type
		calls int
	}{
		{homeworkflow.SummarizeJobType, homeworkflow.AnswerCalls},
		{homeworkflow.ExtractHomeworkJobType, homeworkflow.AnswerCalls},
		{homeworkflow.MaterialSummaryJobType, homeworkflow.AnswerCalls},
		{homeworkflow.MaterialHomeworkJobType, homeworkflow.AnswerCalls},
		{homeworkflow.HomeworkDraftJobType, 1},
	} {
		if got := registrationOf(t, regs, tt.typ).Timeout; got < time.Duration(2*tt.calls)*hubclient.DefaultRequestTimeout {
			t.Errorf("%s timeout %v, want at least twice the Hub request timeout for each of %d calls", tt.typ, got, tt.calls)
		}
	}
	// The lecture text makes its calls one after the other: one Hub request timeout each.
	if got, calls := registrationOf(t, regs, homeworkflow.LectureTextJobType).Timeout, homeworkflow.MaxLectureTextCalls; got <
		time.Duration(calls)*hubclient.DefaultRequestTimeout {
		t.Errorf("lecture_text timeout %v, want at least the Hub request timeout for each of %d calls", got, calls)
	}
}

// Every job type has a handler.
func TestRegisteredTypes(t *testing.T) {
	var got []job.Type
	for _, r := range registrations(handlersForTest()) {
		got = append(got, r.Type)
	}
	want := []job.Type{
		transcribe.JobType, homeworkflow.SummarizeJobType, homeworkflow.ExtractHomeworkJobType,
		homeworkflow.LectureTextJobType, homeworkflow.HomeworkDraftJobType, senddelivery.JobType,
		schedulesync.CalendarSyncJobType, homeworkflow.MaterialSummaryJobType, homeworkflow.MaterialHomeworkJobType, turnkey.WritingJobType,
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("registered types %v, want %v", got, want)
	}
}

// Every registered type is classified for «Записи и задачи» (TASK-074): the calendar sync, which involves no user, and
// the work on a library material, shown in the agent panel (TASK-122), are hidden. A new job type must be added here
// on purpose.
func TestHiddenJobTypes(t *testing.T) {
	shown := []job.Type{
		transcribe.JobType, homeworkflow.SummarizeJobType, homeworkflow.ExtractHomeworkJobType,
		homeworkflow.LectureTextJobType, homeworkflow.HomeworkDraftJobType, senddelivery.JobType,
	}
	hidden := hiddenJobTypes()
	wantHidden := []job.Type{
		schedulesync.CalendarSyncJobType, homeworkflow.MaterialSummaryJobType, homeworkflow.MaterialHomeworkJobType,
		turnkey.WritingJobType,
	}
	if !slices.Equal(hidden, wantHidden) {
		t.Fatalf("hidden types %v, want %v", hidden, wantHidden)
	}
	for _, r := range registrations(handlersForTest()) {
		if slices.Contains(hidden, r.Type) == slices.Contains(shown, r.Type) {
			t.Errorf("%s is not classified as either shown or hidden", r.Type)
		}
	}
}

// The wired runner has every type registered.
func TestWiredCoreRegistersEveryType(t *testing.T) {
	a, _ := newTestAppWith(t, t.TempDir())
	if _, err := a.openCore(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Registering again is refused for every type: each one is taken.
	for _, r := range registrations(handlersForTest()) {
		if err := a.core.runner.Register(r); !errors.Is(err, jobrunner.ErrInvalidConfig) {
			t.Errorf("Register(%s) again = %v, want it already registered", r.Type, err)
		}
	}
}

// The use cases get the runner's attempt cap (so the last attempt is known); their own defaults fit it too.
func TestProductionEnvUsesTheRunnerAttemptCap(t *testing.T) {
	e := productionEnv()
	if e.runner.Backoff.MaxAttempts < senddelivery.DefaultMaxAttempts ||
		e.runner.Backoff.MaxAttempts < homeworkflow.DefaultPostProcessAttempts {
		t.Fatalf("runner MaxAttempts %d is below a use case default", e.runner.Backoff.MaxAttempts)
	}
	if e.stopTimeout <= 0 || e.speech.bundledDir == "" || e.speech.source == nil {
		t.Fatalf("production env: stop timeout %v, speech component %+v", e.stopTimeout, e.speech)
	}
	if _, err := tvalues.ParseSpeechComponent(e.speech.manifest); err != nil {
		t.Fatalf("production env: speech component manifest: %v", err)
	}
}

// Without a speech recognition engine the stt type is not registered (its jobs wait QUEUED); the others are.
func TestRegistrationsWithoutAnEngine(t *testing.T) {
	h := handlersForTest()
	h.stt = nil
	var got []job.Type
	for _, r := range registrations(h) {
		got = append(got, r.Type)
	}
	want := []job.Type{
		homeworkflow.SummarizeJobType, homeworkflow.ExtractHomeworkJobType, homeworkflow.LectureTextJobType,
		homeworkflow.HomeworkDraftJobType, senddelivery.JobType, schedulesync.CalendarSyncJobType,
		homeworkflow.MaterialSummaryJobType, homeworkflow.MaterialHomeworkJobType, turnkey.WritingJobType,
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("registered types %v, want %v", got, want)
	}
}
