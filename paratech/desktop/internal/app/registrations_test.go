package app

import (
	"context"
	"errors"
	"slices"
	"testing"

	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/httpdownload"
	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/adapters/localfile"
	"paratech/desktop/internal/adapters/smtp"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/adapters/wincred"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/modelcatalog"
	"paratech/desktop/internal/cases/notify"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/transcription/values"
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
	_ jobrunner.Observer                    = (*notify.Service)(nil)
	_ notify.Notifier                       = (*wails.Shell)(nil)
	_ notify.NoticeRepository               = (*sqlite.NoticeRepository)(nil)
	_ recordingsession.ArtifactStore        = (*filestore.Store)(nil)
	_ wails.RecordingStopper                = (*recordingsession.Service)(nil)
	_ wails.JobForcer                       = (*jobrunner.Service)(nil)
	_ homeworkflow.DraftRepository          = (*sqlite.DraftRepository)(nil)
	_ hubclient.URLSource                   = hubSettings{}
	_ smtp.AccountSource                    = mailSettings{}
	_ homeworkflow.RecipientSource          = recipientSettings{}
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
		{typ: homeworkflow.HomeworkDraftJobType, recovery: jobrunner.RecoveryRequeue},
		// Hard rule 7: the side effect is never Heavy, never requeued blindly and not cancelled mid-send.
		{typ: senddelivery.JobType, recovery: jobrunner.RecoveryReconcile, reconciler: true, uncancellable: true},
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
	for _, typ := range []job.Type{homeworkflow.SummarizeJobType, homeworkflow.ExtractHomeworkJobType, homeworkflow.HomeworkDraftJobType} {
		if got := registrationOf(t, regs, typ).Timeout; got < 2*hubclient.DefaultRequestTimeout {
			t.Errorf("%s timeout %v, want at least twice the Hub request timeout", typ, got)
		}
	}
}

// Every job type has a handler, and the notified types are the registered ones except email delivery.
func TestRegisteredTypes(t *testing.T) {
	var got []job.Type
	for _, r := range registrations(handlersForTest()) {
		got = append(got, r.Type)
	}
	want := append(notifyTypes(), senddelivery.JobType)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("registered types %v, want %v", got, want)
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
	if e.stopTimeout <= 0 || e.whisperBinary == "" {
		t.Fatalf("production env: stop timeout %v, whisper %q", e.stopTimeout, e.whisperBinary)
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
		homeworkflow.SummarizeJobType, homeworkflow.ExtractHomeworkJobType, homeworkflow.HomeworkDraftJobType, senddelivery.JobType,
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("registered types %v, want %v", got, want)
	}
}

// The embedded catalog is a valid manifest without models, and every call returns its own copy.
func TestEmbeddedCatalogIsEmpty(t *testing.T) {
	data, err := (embeddedCatalog{}).Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cat, err := values.ParseCatalog(data)
	if err != nil {
		t.Fatalf("ParseCatalog = %v", err)
	}
	if n := len(cat.Entries()); n != 0 {
		t.Fatalf("%d catalog entries, want none until their SHA-256 is verified", n)
	}
	if hosts := cat.DownloadHosts().Hosts(); len(hosts) != 0 {
		t.Fatalf("download hosts %v, want none", hosts)
	}
	data[0] = 'x'
	if again, err := (embeddedCatalog{}).Manifest(context.Background()); err != nil || again[0] != '{' {
		t.Fatalf("a changed copy leaked into the next call: %q, %v", again, err)
	}
}
