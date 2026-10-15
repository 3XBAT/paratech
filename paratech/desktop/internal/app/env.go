package app

import (
	"context"
	"crypto/x509"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/internal/adapters/clock"
	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/instancelock"
	"paratech/desktop/internal/adapters/random"
	"paratech/desktop/internal/adapters/wincred"
	"paratech/desktop/internal/adapters/winpower"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/jobrunner"
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
	NewMessageID() string
}

// secretStore is a credential store of one service: what senddelivery.CredentialStore writes and what the adapter of
// that service reads (hubclient.Credentials, smtp.SecretReader).
type secretStore interface {
	senddelivery.CredentialStore
	Get(ctx context.Context, name string) (string, error)
}

// Credential Manager services of the core (wincred.New): one store per service.
const (
	hubSecretsService  = "hub"
	mailSecretsService = "mail"
)

// instanceLock is a held single-instance guard.
type instanceLock interface {
	Release() error
}

// env is the outside world of the core and its tuning. productionEnv fills it; tests of this package replace fields
// before openCore.
type env struct {
	// lock takes the single-instance guard of the data directory; it fails at once when another instance holds it.
	lock func(dataDir string) (instanceLock, error)

	clock  timeSource
	local  localTime // homeworkflow only: its Clock port names the student's zone
	ids    idSource
	jitter jobrunner.Jitter
	power  jobrunner.PowerMonitor

	// recorder builds the audio recorder that writes chunks through the Storage Manager below dataDir.
	recorder func(dataDir string, files *filestore.Store, log *slog.Logger) (recordingsession.Recorder, error)

	// The --recorder run mode (the audio recorder child): its stdio and the platform capture source.
	childStdin    io.Reader
	childStdout   io.Writer
	captureSource audiocapture.Source

	// whisperBinary is the absolute path of whisper-cli; when it cannot be used, the stt job type is not registered and
	// STT jobs wait QUEUED for a start with a usable binary.
	whisperBinary string

	// secrets opens the credential store of a service; tests use testenv.MemCredentials, never Credential Manager.
	secrets func(service string) (secretStore, error)

	// hubHTTP replaces the transport of the Hub client (tests: the fake Hub's certificate); nil in production.
	hubHTTP *http.Client
	// mailRoots replaces the system roots for the SMTP server certificate (tests); nil in production.
	mailRoots *x509.CertPool

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
		clock:           clock.System{},
		local:           clock.Local{},
		ids:             random.IDs{},
		jitter:          random.Jitter{},
		power:           winpower.New(),
		recorder:        platformRecorder,
		childStdin:      os.Stdin,
		childStdout:     os.Stdout,
		captureSource:   audiocapture.PlatformSource(),
		whisperBinary:   whisperBinaryPath(),
		secrets:         credentialManager,
		generateRetries: homeworkflow.DefaultGenerateRetries,
		runner:          jobrunner.DefaultConfig(),
		recordings:      recordingsession.DefaultConfig(),
		stopTimeout:     defaultStopTimeout,
	}
}

// credentialManager is the Windows Credential Manager store of a service. Off Windows every call fails with
// wincred.ErrUnsupported: there is no weaker fallback.
func credentialManager(service string) (secretStore, error) {
	s, err := wincred.New(service)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// acquireLock is the file lock of the data directory.
func acquireLock(dataDir string) (instanceLock, error) {
	l, err := instancelock.Acquire(dataDir)
	if err != nil {
		return nil, err
	}
	return l, nil
}

// whisperBinaryPath is whisper-cli next to the executable; empty when the executable cannot be found.
func whisperBinaryPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	name := "whisper-cli"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(filepath.Dir(exe), name)
}
