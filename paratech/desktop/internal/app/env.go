package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/internal/adapters/clock"
	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/instancelock"
	"paratech/desktop/internal/adapters/random"
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
	NewAssignmentID() shared.AssignmentID
	NewProposalID() proposal.ID
	NewDeliveryID() shared.DeliveryID
	NewMessageID() string
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

	clock  timeSource
	local  localTime // homeworkflow only: its Clock port names the student's zone
	ids    idSource
	jitter jobrunner.Jitter
	power  jobrunner.PowerMonitor

	// recorder builds the audio recorder that writes chunks through the Storage Manager.
	recorder func(files *filestore.Store) recordingsession.Recorder

	// whisperBinary is the absolute path of whisper-cli; when it cannot be used, STT jobs fail with a clear error.
	whisperBinary string

	// Not configured in this build (TASK-057): stand-ins with safe error classes.
	llm         homeworkflow.LLMClient
	email       senddelivery.EmailAdapter
	mailSecrets senddelivery.CredentialStore
	drafts      homeworkflow.DraftRepository

	runner      jobrunner.Config
	recordings  recordingsession.Config
	stopTimeout time.Duration
}

// productionEnv is the outside world of the real application.
func productionEnv() env {
	return env{
		lock:          acquireLock,
		clock:         clock.System{},
		local:         clock.Local{},
		ids:           random.IDs{},
		jitter:        random.Jitter{},
		power:         winpower.New(),
		recorder:      func(*filestore.Store) recordingsession.Recorder { return audiocapture.Unavailable{} },
		whisperBinary: whisperBinaryPath(),
		llm:           hubNotConfigured{},
		email:         mailNotConfigured{},
		mailSecrets:   mailSecretsNotConfigured{},
		drafts:        draftsNotConfigured{},
		runner:        jobrunner.DefaultConfig(),
		recordings:    recordingsession.DefaultConfig(),
		stopTimeout:   defaultStopTimeout,
	}
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
