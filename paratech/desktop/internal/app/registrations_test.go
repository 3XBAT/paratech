package app

import (
	"context"
	"errors"
	"slices"
	"testing"

	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/adapters/smtp"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/adapters/wincred"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/notify"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/input/wails"
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
	_ homeworkflow.DraftRepository          = draftsNotConfigured{}
	_ senddelivery.CredentialStore          = mailSecretsNotConfigured{}
	_ transcribe.STTEngine                  = sttNotConfigured{}
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

// The stand-ins fail with the classes that keep the jobs safe until TASK-057 configures the real adapters.
func TestStandInsFailSafely(t *testing.T) {
	ctx := context.Background()
	_, llmErr := hubNotConfigured{}.Complete(ctx, values.LLMRequest{})
	sendErr := mailNotConfigured{}.Send(ctx, nil, nil)
	accepted, reconcileErr := mailNotConfigured{}.Reconcile(ctx, nil)
	_, getErr := draftsNotConfigured{}.Get(ctx, "a1")
	saveErr := draftsNotConfigured{}.Save(ctx, nil)
	sttErr := sttNotConfigured{}.Transcribe(ctx, shared.ArtifactRef{}, shared.ArtifactRef{}, shared.ArtifactRef{}, nil)
	_, existsErr := mailSecretsNotConfigured{}.Exists(ctx, "smtp-password")

	tests := []struct {
		name string
		err  error
		want []error
	}{
		{"llm waits for the network", llmErr, []error{shared.ErrNetworkUnavailable}},
		{"send is a definite refusal", sendErr, []error{delivery.ErrRejected}},
		{"reconcile stays unknown", reconcileErr, []error{delivery.ErrOutcomeUnknown}},
		{"drafts get", getErr, []error{backoff.ErrPermanent}},
		{"drafts save", saveErr, []error{backoff.ErrPermanent}},
		{"stt", sttErr, []error{backoff.ErrPermanent}},
		{"mail secrets", existsErr, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(tt.err, errNotConfigured) {
				t.Fatalf("err = %v, want errNotConfigured", tt.err)
			}
			for _, w := range tt.want {
				if !errors.Is(tt.err, w) {
					t.Fatalf("err = %v, want %v", tt.err, w)
				}
			}
		})
	}
	if accepted {
		t.Error("Reconcile must not claim the message was accepted")
	}
	if got := delivery.OutcomeOf(sendErr); got != delivery.OutcomeOf(delivery.ErrRejected) {
		t.Errorf("outcome of the send error = %v, want the rejected outcome", got)
	}
	if got, ok := backoff.WaitStatus(llmErr); !ok || got != job.StatusWaitingForNetwork {
		t.Errorf("wait status of the hub error = %v, %v; want WAITING_FOR_NETWORK", got, ok)
	}
}
