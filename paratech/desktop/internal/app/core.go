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
	"paratech/desktop/internal/adapters/googleauth"
	"paratech/desktop/internal/adapters/googlecal"
	"paratech/desktop/internal/adapters/httpdownload"
	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/adapters/localfile"
	"paratech/desktop/internal/adapters/logfile"
	"paratech/desktop/internal/adapters/smtp"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/adapters/sysinfo"
	"paratech/desktop/internal/adapters/weblink"
	"paratech/desktop/internal/adapters/whispercli"
	"paratech/desktop/internal/adapters/zipfile"
	"paratech/desktop/internal/cases/activity"
	"paratech/desktop/internal/cases/googlesignin"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/modelcatalog"
	"paratech/desktop/internal/cases/preferences"
	"paratech/desktop/internal/cases/recordingprompt"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/cases/scheduler"
	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/cases/soundcheck"
	"paratech/desktop/internal/cases/supportbundle"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/input/wails"
)

// Layout of the data directory: the database at its root, the Storage Manager root below it, so that no artifact
// reference reaches the database file, and the private run directories of whisper-cli below the artifact root,
// outside the artifact kinds.
const (
	dbFile       = "learning-assistant.db"
	artifactsDir = "artifacts"
	sttWorkDir   = "stt-work"
	logsDir      = "logs" // the log files of the logfile adapter, read back by the support bundle
)

// core is the opened core: the resources to release and the services the lifecycle starts and stops.
type core struct {
	db         *sqlite.DB
	store      *filestore.Store
	runner     *jobrunner.Service
	recordings *recordingsession.Service
	deliveries *senddelivery.Service
	drafts     *homeworkflow.Drafts  // no binding yet (TASK-045)
	proposals  *homeworkflow.Service // no binding yet (TASK-045)

	// transcriptions enqueues the STT job of a completed recording and runs the startup sweep.
	transcriptions *transcribe.Enqueuer

	// The model manager of the models page; manager is what the page binds to and owns its background work.
	models    *modelcatalog.Service
	downloads *modelcatalog.Downloader
	imports   *modelcatalog.Importer
	manager   *modelManager

	// Google (wireGoogle, wireCalendar): the authorization, the page's sign-in use case, the Calendar adapter and the
	// calendar sync of the schedule (TASK-051).
	google       *googleauth.Authorizer
	googleSignIn *googlesignin.Service
	calendar     *googlecal.Client
	calendarSync *schedulesync.CalendarSync

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

// buildCore opens the database and the Storage Manager below dataDir and builds every use case. It starts no
// goroutines; on an error it closes what it opened.
func buildCore(ctx context.Context, dataDir string, e env, log *slog.Logger) (*core, error) {
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
	if err := c.wire(dataDir, e, log); err != nil {
		return nil, errors.Join(err, c.close())
	}
	return c, nil
}

// wire builds the use cases on the opened storage.
func (c *core) wire(dataDir string, e env, log *slog.Logger) error {
	db, store, root := c.db, c.store, filepath.Join(dataDir, artifactsDir)
	jobs := sqlite.NewJobRepository(db)
	settings := sqlite.NewSettingsRepository(db)
	recordings := sqlite.NewRecordingRepository(db)
	transcripts := sqlite.NewTranscriptRepository(db)
	lessons := sqlite.NewLessonRepository(db)
	summaries := sqlite.NewSummaryRepository(db)
	assignments := sqlite.NewAssignmentRepository(db)
	maxAttempts := e.runner.Backoff.MaxAttempts

	// One credential store per service, given to its adapter (which reads) and to the use case (which writes).
	hubSecrets, err := e.secrets(hubSecretsService)
	if err != nil {
		return fmt.Errorf("app: hub credentials: %w", err)
	}
	mailSecrets, err := e.secrets(mailSecretsService)
	if err != nil {
		return fmt.Errorf("app: mail credentials: %w", err)
	}
	// The Hub URL and the mail account are read from the settings at every call; empty means not configured.
	llm, err := hubclient.New(hubclient.Config{URLs: hubSettings{settings}, HTTPClient: e.hubHTTP}, hubSecrets)
	if err != nil {
		return fmt.Errorf("app: hub client: %w", err)
	}
	mailer, err := smtp.New(smtp.Config{Accounts: mailSettings{settings}, RootCAs: e.mailRoots}, mailSecrets)
	if err != nil {
		return fmt.Errorf("app: mail: %w", err)
	}

	post, err := homeworkflow.NewPostProcessor(homeworkflow.PostProcessDeps{
		LLM: llm, Transcripts: transcripts, Recordings: recordings, Lessons: lessons, Artifacts: store,
		Summaries: summaries, Assignments: assignments, Jobs: jobs, Clock: e.local, IDs: e.ids, Log: log,
		ModelAlias: modelAlias, MaxAttempts: maxAttempts,
	})
	if err != nil {
		return fmt.Errorf("app: post-processing: %w", err)
	}
	c.drafts, err = homeworkflow.NewDrafts(homeworkflow.DraftDeps{
		LLM: llm, Assignments: assignments, Drafts: sqlite.NewDraftRepository(db), Summaries: summaries, Lessons: lessons,
		Artifacts: store, Jobs: jobs, JobReader: jobs, Clock: e.local, IDs: e.ids, Tx: db, Log: log,
		ModelAlias: modelAlias, MaxAttempts: maxAttempts, GenerateRetries: e.generateRetries,
	})
	if err != nil {
		return fmt.Errorf("app: homework drafts: %w", err)
	}

	if err := c.wireModels(e); err != nil {
		return err
	}
	c.transcriptions, err = transcribe.NewEnqueuer(transcribe.EnqueueDeps{
		Recordings: recordings, Pending: recordings, Models: modelResolver{models: c.models}, Jobs: jobs, IDs: e.ids,
		Tx: db, Clock: e.clock, Log: log, MaxAttempts: maxAttempts,
	})
	if err != nil {
		return fmt.Errorf("app: transcription queue: %w", err)
	}

	// Without a usable engine the stt type stays unregistered and STT jobs wait QUEUED (see registrations).
	handlers := jobHandlers{postProcess: post, drafts: c.drafts}
	engine := c.sttEngine(root, e.whisperBinary, log)
	if engine != nil {
		// The post-processor is the consumer of the STT completion event: it enqueues the follow-up jobs idempotently.
		stt, err := transcribe.New(transcribe.Deps{
			Engine: engine, Transcripts: transcripts, Recordings: recordings, Models: sqlite.NewModelRepository(db),
			Artifacts: store, Events: post, Clock: e.clock, Log: log,
		})
		if err != nil {
			return fmt.Errorf("app: transcription: %w", err)
		}
		handlers.stt = stt
	}

	deliveries := sqlite.NewDeliveryRepository(db)
	c.deliveries, err = senddelivery.New(senddelivery.Deps{
		Deliveries: deliveries, History: deliveries, Artifacts: store, Email: mailer, Jobs: jobs,
		Credentials: mailSecrets, Clock: e.clock, IDs: e.ids, Tx: db, Log: log,
	}, senddelivery.Config{MaxAttempts: maxAttempts})
	if err != nil {
		return fmt.Errorf("app: email delivery: %w", err)
	}
	handlers.delivery = c.deliveries
	// Before the proposals (the deadline calendar) and the runner (the calendar sync handler).
	if err := c.wireGoogle(e, log); err != nil {
		return err
	}
	if err := c.wireCalendar(e, jobs, lessons, log, maxAttempts); err != nil {
		return err
	}
	handlers.calendar = c.calendarSync
	if err := c.wireProposals(e, settings); err != nil {
		return err
	}

	c.runner, err = jobrunner.New(jobrunner.Deps{
		Jobs: jobs, Clock: e.clock, Sleeper: e.clock, Jitter: e.jitter, Log: log,
		Power: e.power, Settings: settings,
	}, e.runner)
	if err != nil {
		return fmt.Errorf("app: job runner: %w", err)
	}
	for _, r := range registrations(handlers) {
		if err := c.runner.Register(r); err != nil {
			return fmt.Errorf("app: register %s jobs: %w", r.Type, err)
		}
	}

	recorder, err := e.recorder(dataDir, store, log)
	if err != nil {
		return fmt.Errorf("app: audio recorder: %w", err)
	}
	c.recordings, err = recordingsession.New(recordingsession.Deps{
		Recordings: recordings, Transcripts: sqlite.NewRecordingTranscripts(db), Recorder: recorder,
		Artifacts: store, Tx: db, Clock: e.clock, Sleeper: e.clock, IDs: e.ids, Completions: c.transcriptions, Log: log,
	}, e.recordings)
	if err != nil {
		return fmt.Errorf("app: recordings: %w", err)
	}

	sound, err := soundcheck.New(soundChecker(recorder), settings, recordingActivity{c.recordings}, log)
	if err != nil {
		return fmt.Errorf("app: sound check: %w", err)
	}
	bundle, err := supportbundle.New(supportbundle.Deps{
		Archive: zipfile.New(), Logs: logfile.Dir{Path: filepath.Join(dataDir, logsDir)}, System: sysinfo.New(),
		Settings: settings, Captures: captureHistory(recorder), Clock: e.local, Log: log,
	}, supportbundle.Config{})
	if err != nil {
		return fmt.Errorf("app: support bundle: %w", err)
	}

	prefs, err := preferences.New(settings, e.clock)
	if err != nil {
		return fmt.Errorf("app: preferences: %w", err)
	}
	history, err := activity.New(recordings, jobs, log)
	if err != nil {
		return fmt.Errorf("app: activity: %w", err)
	}

	// A model installed while the app runs gets the waiting recordings enqueued at once; whisper-cli is still found
	// only at start, so without it their jobs wait QUEUED until a restart (the page says so: SpeechReady).
	c.manager = newModelManager(c, c.transcriptions, log, e.stopTimeout)

	// The model manager writes artifacts only after the temporary files of a crashed run are gone.
	c.cleanups = append(c.cleanups, cleanup{name: "artifact temporary files", run: store.CleanupTemp},
		cleanup{name: "model manager writes allowed", run: c.manager.markReady})
	c.ui = wails.Core{
		Settings: prefs, Activity: history, Stopper: c.recordings, Forcer: c.runner,
		Models: c.manager, Downloads: c.manager, Imports: c.manager, Links: weblink.New(), SpeechReady: engine != nil,
		Sound: sound, Bundle: bundle, Google: c.googleSignIn, Calendar: c.calendarSync,
	}
	if err := c.wireConnections(e, settings, mailSecrets, mailer, hubSecrets, llm, log); err != nil {
		return err
	}
	return c.wireSchedule(e, log)
}

// sttEngine is whisper-cli below the artifact root, or nil when the binary is missing or refused by the adapter's
// checks. That does not stop the app (settings and lists stay usable): the log says why, and STT jobs wait in the
// queue until a start finds a usable binary.
func (c *core) sttEngine(root, binary string, log *slog.Logger) transcribe.STTEngine {
	engine, err := c.whisper(root, binary, log)
	if engine == nil {
		if err != nil {
			log.Error("speech recognition is not available", "err", err)
		}
		log.Warn("transcription jobs wait in the queue until whisper-cli is installed next to the executable and the app restarts")
		return nil
	}
	c.cleanups = append(c.cleanups, cleanup{name: "speech recognition work directories", run: engine.CleanupWork})
	return engine
}

// whisper builds the whispercli adapter; nil and no error when there is no binary (logged here).
func (c *core) whisper(root, binary string, log *slog.Logger) (*whispercli.Engine, error) {
	if binary == "" {
		log.Warn("speech recognition is not available: whisper-cli path unknown")
		return nil, nil
	}
	if _, err := os.Stat(binary); errors.Is(err, fs.ErrNotExist) {
		log.Warn("speech recognition is not available: whisper-cli is not installed next to the executable")
		return nil, nil
	}
	return whispercli.New(whispercli.Config{Binary: binary, DataRoot: root, WorkDir: filepath.Join(root, sttWorkDir)},
		c.store, c.store)
}

// wireModels builds the model manager (PRD section 8): the installed models, the default, the pin of an STT job, the
// download of a catalog model and the import of a file the user picked. The catalog is the embedded manifest, which
// lists no model yet (embeddedCatalog).
func (c *core) wireModels(e env) error {
	var err error
	c.models, err = modelcatalog.New(modelcatalog.Deps{
		Models: sqlite.NewModelRepository(c.db), Pins: sqlite.NewModelPins(c.db), Settings: sqlite.NewSettingsRepository(c.db),
		Files: c.store, Catalog: embeddedCatalog{}, Clock: e.clock, IDs: e.ids, Tx: c.db,
	})
	if err != nil {
		return fmt.Errorf("app: model catalog: %w", err)
	}
	c.downloads, err = modelcatalog.NewDownloader(c.models, modelcatalog.DownloadDeps{Source: httpdownload.New(), Space: c.store})
	if err != nil {
		return fmt.Errorf("app: model download: %w", err)
	}
	c.imports, err = modelcatalog.NewImporter(c.models, modelcatalog.ImportDeps{Source: localfile.New(), Space: c.store})
	if err != nil {
		return fmt.Errorf("app: model import: %w", err)
	}
	return nil
}

// modelResolver is the model port of the transcription queue over the model catalog; mapping only.
type modelResolver struct {
	models *modelcatalog.Service
}

// ResolveForJob returns the id of the model an STT job pins.
func (r modelResolver) ResolveForJob(ctx context.Context, requested shared.SttModelID) (shared.SttModelID, error) {
	m, err := r.models.ResolveForJob(ctx, requested)
	return m.ID, err
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
