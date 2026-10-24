package app

import (
	"context"
	"crypto/x509"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/internal/adapters/clock"
	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/googleauth"
	"paratech/desktop/internal/adapters/googlecal"
	"paratech/desktop/internal/adapters/httpdownload"
	"paratech/desktop/internal/adapters/instancelock"
	"paratech/desktop/internal/adapters/logfile"
	"paratech/desktop/internal/adapters/random"
	"paratech/desktop/internal/adapters/urlscheme"
	"paratech/desktop/internal/adapters/weblink"
	"paratech/desktop/internal/adapters/wincred"
	"paratech/desktop/internal/adapters/winpower"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/recordingprompt"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/shared"
)

// defaultStopTimeout bounds each graceful shutdown step (job runner, recording session); then work is interrupted and
// left for the recovery of the next start.
const defaultStopTimeout = 15 * time.Second

// timeSource is the clock of the use cases: Now and Sleep on the same time source (ARCH-24).
type timeSource interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

// localTime is the clock in the student's time zone.
type localTime interface {
	Now() time.Time
}

// idSource generates every identifier the use cases ask for.
type idSource interface {
	NewRecordingID() shared.RecordingID
	NewJobID() shared.JobID
	NewLessonID() shared.LessonID
	NewAssignmentID() shared.AssignmentID
	NewProposalID() proposal.ID
	NewDeliveryID() shared.DeliveryID
	NewSttModelID() shared.SttModelID
	NewSubjectID() shared.SubjectID
	NewFolderID() shared.FolderID
	NewMaterialID() shared.MaterialID
	NewMessageID() string
	NewThreadID() shared.ThreadID
	NewActionID() shared.ActionID
	NewWorkID() shared.WorkID
	NewAnswerID() shared.AnswerID
}

// secretStore is a credential store of one service: what senddelivery.CredentialStore writes and what the adapter of
// that service reads (hubclient.Credentials, smtp.SecretReader).
type secretStore interface {
	senddelivery.CredentialStore
	Get(ctx context.Context, name string) (string, error)
}

// Credential Manager services of the core (wincred.NewInProfile): one store per service.
const (
	hubSecretsService    = "hub"
	mailSecretsService   = "mail"
	googleSecretsService = "google"
)

// logSink is an open log file.
type logSink interface {
	io.Writer
	Close() error
}

// instanceLock is a held single-instance guard.
type instanceLock interface {
	Release() error
}

// env is the outside world of the core and its tuning. productionEnv fills it; tests of this package replace fields
// before openCore.
type env struct {
	// lock takes the single-instance guard of the data directory; it fails at once when another instance holds it.
	lock func(dataDir string) (instanceLock, error)
	// logFile opens the log file in dir after the lock is held; nil keeps the log on the console only.
	logFile func(dir string) (logSink, error)

	clock  timeSource
	local  localTime // the use cases that show the student's zone (homework, support bundle, transcript viewer)
	ids    idSource
	jitter jobrunner.Jitter
	power  jobrunner.PowerMonitor

	// recorder builds the audio recorder that writes chunks through the Storage Manager below dataDir.
	recorder func(dataDir string, files *filestore.Store, log *slog.Logger) (recordingsession.Recorder, error)
	// apps lists the open apps for the source picker of the recording prompt; tests use a fake, never real windows.
	apps func(log *slog.Logger) recordingprompt.AudioApps

	// The --recorder run mode (the audio recorder child): its stdio and the platform capture source.
	childStdin    io.Reader
	childStdout   io.Writer
	captureSource audiocapture.Source

	// speech is the speech recognition component: its manifest, the folder of the executable and the install source;
	// tests use a manifest of the test binary and a fake server. Without a verified set STT jobs wait QUEUED.
	speech speechEnv

	// secrets opens the credential store of a service in the profile of the data directory (ADR-0014); tests use
	// testenv.MemCredentials, never Credential Manager.
	secrets func(profile wincred.Profile, service string) (secretStore, error)

	// hubHTTP replaces the transport of the Hub client (tests: the fake Hub's certificate); nil in production.
	hubHTTP *http.Client
	// mailRoots replaces the system roots for the SMTP server certificate (tests); nil in production.
	mailRoots *x509.CertPool

	// authPages opens Google's consent page (weblink in production, a fake browser in tests).
	authPages googleauth.Browser
	// googleOAuth replaces the Google endpoints and timeouts (tests: a fake OAuth server); nil in production.
	googleOAuth []googleauth.Option
	// googleCalendar replaces the Calendar API base URL (tests: a fake Calendar API); nil in production, so the base
	// URL is never taken from settings or user input.
	googleCalendar []googlecal.Option

	// scheme keeps the paratech: link registration in UI mode; nil in tests, so no test writes the registry.
	scheme schemeRegistrar

	runner jobrunner.Config
	// generateRetries bounds the restarts of one draft generation (homeworkflow.DraftDeps.GenerateRetries); 0: default.
	generateRetries int
	recordings      recordingsession.Config
	stopTimeout     time.Duration
}

// productionEnv is the outside world of the real application.
func productionEnv() env {
	return env{
		lock:            acquireLock,
		logFile:         openLogFile,
		clock:           clock.System{},
		local:           clock.Local{},
		ids:             random.IDs{},
		jitter:          random.Jitter{},
		power:           winpower.New(),
		recorder:        platformRecorder,
		apps:            platformApps,
		childStdin:      os.Stdin,
		childStdout:     os.Stdout,
		captureSource:   audiocapture.PlatformSource(audiocapture.DefaultSourceConfig()),
		speech:          speechEnv{manifest: embeddedSpeechManifest(), bundledDir: executableDir(), source: httpdownload.New()},
		secrets:         credentialManager,
		authPages:       weblink.New(),
		scheme:          urlscheme.New(),
		generateRetries: homeworkflow.DefaultGenerateRetries,
		runner:          jobrunner.DefaultConfig(),
		recordings:      recordingsession.DefaultConfig(),
		stopTimeout:     defaultStopTimeout,
	}
}

// credentialManager is the Windows Credential Manager store of a service in profile. Off Windows every call fails
// with wincred.ErrUnsupported: there is no weaker fallback.
func credentialManager(profile wincred.Profile, service string) (secretStore, error) {
	s, err := wincred.NewInProfile(service, profile)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// openLogFile is the size-rotated log file of the logfile adapter (5 MiB, two old files).
func openLogFile(dir string) (logSink, error) {
	f, err := logfile.Open(dir, logfile.Options{})
	if err != nil {
		return nil, err
	}
	return f, nil
}

// acquireLock is the file lock of the data directory.
func acquireLock(dataDir string) (instanceLock, error) {
	l, err := instancelock.Acquire(dataDir)
	if err != nil {
		return nil, err
	}
	return l, nil
}
