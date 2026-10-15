package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/adapters/whispercli"
	"paratech/desktop/internal/cases/activity"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/notify"
	"paratech/desktop/internal/cases/preferences"
	"paratech/desktop/internal/cases/recordingprompt"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/cases/scheduler"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/input/wails"
)

// Layout of the data directory: the database at its root, the Storage Manager root below it, so that no artifact
// reference reaches the database file, and the private run directories of whisper-cli below the artifact root,
// outside the artifact kinds.
const (
	dbFile       = "learning-assistant.db"
	artifactsDir = "artifacts"
	sttWorkDir   = "stt-work"
)

// core is the opened core: the resources to release and the services the lifecycle starts and stops.
type core struct {
	db         *sqlite.DB
	store      *filestore.Store
	runner     *jobrunner.Service
	recordings *recordingsession.Service
	deliveries *senddelivery.Service

	// UI mode only (wirePrompts): nil headless.
	scheduler *scheduler.Service
	prompts   *recordingprompt.Service

	// cleanups remove what a crashed run left behind; they run at the start, before any job or recording writes.
	cleanups []cleanup

	ui wails.Core
}

// cleanup is one startup cleanup step.
type cleanup struct {
	name string
	run  func(ctx context.Context) (int, error)
}

// buildCore opens the database and the Storage Manager below dataDir and builds every use case. notifier may be nil
// (headless: nobody is told about finished jobs). It starts no goroutines; on an error it closes what it opened.
func buildCore(ctx context.Context, dataDir string, e env, notifier notify.Notifier, log *slog.Logger) (*core, error) {
	db, err := sqlite.Open(ctx, sqlite.Options{Path: filepath.Join(dataDir, dbFile), Migrate: true, Logger: log})
	if err != nil {
		return nil, fmt.Errorf("app: open database: %w", err)
	}
	root := filepath.Join(dataDir, artifactsDir)
	store, err := filestore.New(root)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("app: open artifact store: %w", err)
	}

	c := &core{db: db, store: store}
	if err := c.wire(root, e, notifier, log); err != nil {
		return nil, errors.Join(err, c.close())
	}
	return c, nil
}

// wire builds the use cases on the opened storage.
func (c *core) wire(root string, e env, notifier notify.Notifier, log *slog.Logger) error {
	db, store := c.db, c.store
	jobs := sqlite.NewJobRepository(db)
	settings := sqlite.NewSettingsRepository(db)
	recordings := sqlite.NewRecordingRepository(db)
	transcripts := sqlite.NewTranscriptRepository(db)
	lessons := sqlite.NewLessonRepository(db)
	summaries := sqlite.NewSummaryRepository(db)
	assignments := sqlite.NewAssignmentRepository(db)
	maxAttempts := e.runner.Backoff.MaxAttempts

	post, err := homeworkflow.NewPostProcessor(homeworkflow.PostProcessDeps{
		LLM: e.llm, Transcripts: transcripts, Recordings: recordings, Lessons: lessons, Artifacts: store,
		Summaries: summaries, Assignments: assignments, Jobs: jobs, Clock: e.local, IDs: e.ids, Log: log,
		ModelAlias: modelAlias, MaxAttempts: maxAttempts,
	})
	if err != nil {
		return fmt.Errorf("app: post-processing: %w", err)
	}
	drafts, err := homeworkflow.NewDrafts(homeworkflow.DraftDeps{
		LLM: e.llm, Assignments: assignments, Drafts: e.drafts, Summaries: summaries, Lessons: lessons, Artifacts: store,
		Jobs: jobs, JobReader: jobs, Clock: e.local, IDs: e.ids, Tx: db, Log: log,
		ModelAlias: modelAlias, MaxAttempts: maxAttempts,
	})
	if err != nil {
		return fmt.Errorf("app: homework drafts: %w", err)
	}

	engine := c.sttEngine(root, e.whisperBinary, log)
	// The post-processor is the consumer of the STT completion event: it enqueues the follow-up jobs idempotently.
	stt, err := transcribe.New(transcribe.Deps{
		Engine: engine, Transcripts: transcripts, Recordings: recordings, Models: sqlite.NewModelRepository(db),
		Artifacts: store, Events: post, Clock: e.clock, Log: log,
	})
	if err != nil {
		return fmt.Errorf("app: transcription: %w", err)
	}

	c.deliveries, err = senddelivery.New(senddelivery.Deps{
		Deliveries: sqlite.NewDeliveryRepository(db), Artifacts: store, Email: e.email, Jobs: jobs,
		Credentials: e.mailSecrets, Clock: e.clock, IDs: e.ids, Tx: db, Log: log,
	}, senddelivery.Config{MaxAttempts: maxAttempts})
	if err != nil {
		return fmt.Errorf("app: email delivery: %w", err)
	}

	var observer jobrunner.Observer
	if notifier != nil {
		n, err := notify.New(notify.Deps{
			Settings: settings, Notices: sqlite.NewNoticeRepository(db), Notifier: notifier, Clock: e.clock, Log: log,
		}, notify.Config{Types: notifyTypes()})
		if err != nil {
			return fmt.Errorf("app: notifications: %w", err)
		}
		observer = n
	}

	c.runner, err = jobrunner.New(jobrunner.Deps{
		Jobs: jobs, Clock: e.clock, Sleeper: e.clock, Jitter: e.jitter, Log: log,
		Power: e.power, Settings: settings, Observer: observer,
	}, e.runner)
	if err != nil {
		return fmt.Errorf("app: job runner: %w", err)
	}
	for _, r := range registrations(jobHandlers{stt: stt, postProcess: post, drafts: drafts, delivery: c.deliveries}) {
		if err := c.runner.Register(r); err != nil {
			return fmt.Errorf("app: register %s jobs: %w", r.Type, err)
		}
	}

	c.recordings, err = recordingsession.New(recordingsession.Deps{
		Recordings: recordings, Transcripts: sqlite.NewRecordingTranscripts(db), Recorder: e.recorder(store),
		Artifacts: store, Tx: db, Clock: e.clock, Sleeper: e.clock, IDs: e.ids, Log: log,
	}, e.recordings)
	if err != nil {
		return fmt.Errorf("app: recordings: %w", err)
	}

	prefs, err := preferences.New(settings, e.clock)
	if err != nil {
		return fmt.Errorf("app: preferences: %w", err)
	}
	history, err := activity.New(recordings, jobs, log)
	if err != nil {
		return fmt.Errorf("app: activity: %w", err)
	}

	c.cleanups = append(c.cleanups, cleanup{name: "artifact temporary files", run: store.CleanupTemp})
	c.ui = wails.Core{Settings: prefs, Activity: history, Stopper: c.recordings, Forcer: c.runner}
	return nil
}

// sttEngine is whisper-cli below the artifact root. A binary that is missing or refused by the adapter's checks does
// not stop the app (settings and lists stay usable): STT jobs fail with a clear error instead, and the log says why.
func (c *core) sttEngine(root, binary string, log *slog.Logger) transcribe.STTEngine {
	if binary == "" {
		log.Warn("speech recognition is not available: whisper-cli path unknown")
		return sttNotConfigured{}
	}
	if _, err := os.Stat(binary); errors.Is(err, fs.ErrNotExist) {
		log.Warn("speech recognition is not available: whisper-cli is not installed next to the executable")
		return sttNotConfigured{}
	}
	engine, err := whispercli.New(whispercli.Config{Binary: binary, DataRoot: root, WorkDir: filepath.Join(root, sttWorkDir)},
		c.store, c.store)
	if err != nil {
		log.Error("speech recognition is not available", "err", err)
		return sttNotConfigured{}
	}
	c.cleanups = append(c.cleanups, cleanup{name: "speech recognition work directories", run: engine.CleanupWork})
	return engine
}

// close releases the storage in reverse order of opening. It is safe to call more than once.
func (c *core) close() error {
	var errs []error
	if c.store != nil {
		errs = append(errs, c.store.Close())
		c.store = nil
	}
	if c.db != nil {
		errs = append(errs, c.db.Close())
		c.db = nil
	}
	return errors.Join(errs...)
}
