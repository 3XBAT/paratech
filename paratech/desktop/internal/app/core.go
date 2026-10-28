package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	"paratech/desktop/internal/adapters/componentstore"
	"paratech/desktop/internal/adapters/doctext"
	"paratech/desktop/internal/adapters/docx"
	"paratech/desktop/internal/adapters/fileopen"
	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/googleauth"
	"paratech/desktop/internal/adapters/googlecal"
	"paratech/desktop/internal/adapters/httpdownload"
	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/adapters/imagepage"
	"paratech/desktop/internal/adapters/localfile"
	"paratech/desktop/internal/adapters/logfile"
	"paratech/desktop/internal/adapters/pdf"
	"paratech/desktop/internal/adapters/pptx"
	"paratech/desktop/internal/adapters/smtp"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/adapters/sysinfo"
	"paratech/desktop/internal/adapters/textfile"
	"paratech/desktop/internal/adapters/weblink"
	"paratech/desktop/internal/adapters/wincred"
	"paratech/desktop/internal/adapters/zipfile"
	"paratech/desktop/internal/cases/activity"
	"paratech/desktop/internal/cases/agentchats"
	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/answering"
	"paratech/desktop/internal/cases/googlesignin"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/knowledgebase"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/cases/modelcatalog"
	"paratech/desktop/internal/cases/notes"
	"paratech/desktop/internal/cases/preferences"
	"paratech/desktop/internal/cases/recordingdelete"
	"paratech/desktop/internal/cases/recordingprompt"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/cases/scheduler"
	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/cases/soundcheck"
	"paratech/desktop/internal/cases/speechcomponent"
	"paratech/desktop/internal/cases/sttspeed"
	"paratech/desktop/internal/cases/summaryview"
	"paratech/desktop/internal/cases/supportbundle"
	"paratech/desktop/internal/cases/tickets"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/cases/transcriptview"
	"paratech/desktop/internal/cases/turnkey"
	"paratech/desktop/internal/domain/shared"
	agentloop "paratech/desktop/internal/input/agent"
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
	// webviewPDFDir is the WebView2 profile folder of the PDF printer (ADR-0020) and its temporary PDFs, exclusive to it.
	webviewPDFDir = "webview-pdf"
)

// core is the opened core: the resources to release and the services the lifecycle starts and stops.
type core struct {
	db         *sqlite.DB
	store      *filestore.Store
	runner     *jobrunner.Service
	recordings *recordingsession.Service
	deliveries *senddelivery.Service
	drafts     *homeworkflow.Drafts
	proposals  *homeworkflow.Service
	deletions  *recordingdelete.Service // «Удалить» of the recordings and the resume at the start (TASK-095)

	// transcriptions enqueues the STT job of a completed recording and runs the startup sweep.
	transcriptions *transcribe.Enqueuer

	// The speech recognition component (TASK-072): its folder below the data directory and the use case that finds,
	// installs and activates it.
	components *componentstore.Store
	speech     *speechcomponent.Service

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

	// sound is "Проверить звук"; its running check ends with the lifecycle.
	sound *soundcheck.Service

	// UI mode only (wirePrompts): nil headless.
	scheduler *scheduler.Service
	prompts   *recordingprompt.Service
	// advice hands the silent-source advice of the recordings to the prompts once they are wired (TASK-082).
	advice adviceRelay

	// The agent core (wireAgent): the steps of a request, the action journal and the Driver of the requests.
	agent     *agentturn.Service
	chats     *agentchats.Service // the chat list of the agent panel (ADR-0028)
	journal   *agentjournal.Service
	driver    *agentloop.Driver
	panel     *wails.AgentPanel    // the Driver's updates as UI events
	freeWorks *agentloop.FreeWorks // the free-work cards (TASK-188)
	mentions  mentionObjects       // whether a mentioned object exists, for the requests and the «@» search

	// cleanups remove what a crashed run left behind; they run at the start, before any job or recording writes.
	cleanups []cleanup
	// purge removes the assignments past the restore window once a day (wirePurge; the first run is a cleanup).
	purge loop

	ui wails.Core
}

// cleanup is one startup cleanup step.
type cleanup struct {
	name string
	run  func(ctx context.Context) (int, error)
}

// buildCore opens the database and the Storage Manager below dataDir and builds every use case, with the credential
// stores of profile (wincred.ProfileFor). It starts no goroutines; on an error it closes what it opened.
func buildCore(ctx context.Context, dataDir string, profile wincred.Profile, e env, log *slog.Logger) (*core, error) {
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
	if err := c.wire(dataDir, profile, e, log); err != nil {
		return nil, errors.Join(err, c.close())
	}
	c.detectSpeech(ctx, log)
	return c, nil
}

// wire builds the use cases on the opened storage.
func (c *core) wire(dataDir string, profile wincred.Profile, e env, log *slog.Logger) error {
	db, store := c.db, c.store
	jobs := sqlite.NewJobRepository(db)
	settings := sqlite.NewSettingsRepository(db)
	recordings := sqlite.NewRecordingRepository(db)
	transcripts := sqlite.NewTranscriptRepository(db)
	parts := sqlite.NewTranscriptParts(db) // the live transcript while the recognition runs (TASK-079)
	speeds := sqlite.NewSpeedStore(db)     // the recognition speed measured on this computer (TASK-084)
	lessons := sqlite.NewLessonRepository(db)
	summaries := sqlite.NewSummaryRepository(db)
	lectureTexts := sqlite.NewLectureTextRepository(db) // the lecture text of a transcript (TASK-090)
	assignments := sqlite.NewAssignmentRepository(db)
	maxAttempts := e.runner.Backoff.MaxAttempts

	// One credential store per service, given to its adapter (which reads) and to the use case (which writes).
	hubSecrets, err := e.secrets(profile, hubSecretsService)
	if err != nil {
		return fmt.Errorf("app: hub credentials: %w", err)
	}
	mailSecrets, err := e.secrets(profile, mailSecretsService)
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

	// The library of subjects and files (ADR-0024): files in the artifact store by sha256; a new subject's semester in
	// the local time zone. Its texts are read locally (doctext); only Inspect asks the Hub's reader (TASK-122).
	// A subject with notes of the student is not removed (ADR-0027). Photos are encoded again locally (imagepage) and
	// kept as pages without text (TASK-165).
	noteRepo := sqlite.NewNoteRepository(db)
	lib, err := library.New(library.Deps{
		Subjects: sqlite.NewSubjectRepository(db), Materials: sqlite.NewMaterialRepository(db), Blobs: store,
		Lessons: lessons, Strings: sqlite.NewSubjectStrings(db), Clock: e.local, IDs: e.ids, Texts: doctext.New(),
		Reader: llm, Inspections: sqlite.NewInspectionRepository(db), Notes: noteRepo, Images: imagepage.New(), Log: log,
	})
	if err != nil {
		return fmt.Errorf("app: library: %w", err)
	}
	// «Мой конспект» (TASK-151): the files through the library; the date of a lecture in the local time zone.
	ownNotes, err := notes.New(notes.Deps{
		Notes: noteRepo, Materials: lib, Recordings: recordings, Lessons: lessons, Clock: e.local, IDs: e.ids, Log: log,
	})
	if err != nil {
		return fmt.Errorf("app: notes: %w", err)
	}

	// The post-processing also runs the summary and the homework search of a library material (TASK-122).
	post, err := homeworkflow.NewPostProcessor(homeworkflow.PostProcessDeps{
		LLM: llm, Transcripts: transcripts, Recordings: recordings, Lessons: lessons, Artifacts: store,
		Summaries: summaries, Assignments: assignments, LectureTexts: lectureTexts, Jobs: jobs, Clock: e.local, IDs: e.ids,
		Materials: lib, MaterialAssignments: assignments, Log: log, ModelAlias: modelAlias, MaxAttempts: maxAttempts,
	})
	if err != nil {
		return fmt.Errorf("app: post-processing: %w", err)
	}
	// «Повторить» and «Сделать конспект заново» of the recording rows create new post-processing jobs (TASK-086); the
	// lecture text of an older transcript is started the same way (TASK-090).
	rerunner, err := homeworkflow.NewRerunner(homeworkflow.RerunDeps{
		Transcripts: transcripts, Jobs: jobs, Summaries: summaries, Clock: e.local, IDs: e.ids, Log: log,
		MaxAttempts: maxAttempts,
	})
	if err != nil {
		return fmt.Errorf("app: post-processing reruns: %w", err)
	}
	materialJobs, err := homeworkflow.NewMaterialJobs(homeworkflow.MaterialJobsDeps{
		Materials: lib, Jobs: jobs, Summaries: summaries, Clock: e.local, IDs: e.ids, Log: log, MaxAttempts: maxAttempts,
	})
	if err != nil {
		return fmt.Errorf("app: material jobs: %w", err)
	}

	deliveries := sqlite.NewDeliveryRepository(db)
	c.deliveries, err = senddelivery.New(senddelivery.Deps{
		Deliveries: deliveries, History: deliveries, Artifacts: store, Email: mailer, Jobs: jobs,
		Credentials: mailSecrets, Clock: e.clock, IDs: e.ids, Tx: db, Log: log,
	}, senddelivery.Config{MaxAttempts: maxAttempts})
	if err != nil {
		return fmt.Errorf("app: email delivery: %w", err)
	}
	// The deliveries lock the export file of an assignment while one of its emails is unresolved.
	c.drafts, err = homeworkflow.NewDrafts(homeworkflow.DraftDeps{
		LLM: llm, Assignments: assignments, Recordings: recordings, Drafts: sqlite.NewDraftRepository(db),
		Summaries: summaries, Lessons: lessons, Artifacts: store, Jobs: jobs, JobReader: jobs, Deliveries: c.deliveries,
		Profiles: settings, Renderer: docx.New(), Files: textfile.New(), Clock: e.local, IDs: e.ids, Tx: db,
		Log: log, ModelAlias: modelAlias, MaxAttempts: maxAttempts, GenerateRetries: e.generateRetries,
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

	// The stt type is registered by the speech activator once the speech component verified (wireSpeech); until then
	// STT jobs wait QUEUED.
	postHandler, err := withTodayPosts(post, db, e, log) // a post on «Сегодня» after each lesson (TASK-125)
	if err != nil {
		return err
	}
	handlers := jobHandlers{postProcess: postHandler, drafts: c.drafts}
	handlers.delivery = c.deliveries
	// Before the proposals (the deadline calendar) and the runner (the calendar sync handler).
	if err := c.wireGoogle(e, profile, log); err != nil {
		return err
	}
	if err := c.wireCalendar(e, jobs, lessons, log, maxAttempts); err != nil {
		return err
	}
	handlers.calendar = c.calendarSync
	if err := c.wireProposals(e, lib, log); err != nil {
		return err
	}
	if err := c.wirePurge(e, log); err != nil {
		return err
	}

	// Turnkey works (TASK-129): the triage reader on the Hub, texts from the assignments and the library;
	// «Спросить по шагу» (TASK-179) on homework_draft with the desktop's prompt and the writing alias. The writing
	// (TASK-131) runs as a job on the Hub's homework_draft operation; the title page from the profile and the lesson the
	// assignment shows, the paper filed into the subject's «ДЗ»; a presentation (TASK-106) files its .pptx and the
	// speaker text there. «Остановить» (TASK-178) cancels through the runner built below. «Скачать PDF» (TASK-198)
	// prints with the one Printer of the process, whose WebView2 profile folder is its own (never the windows' one); off
	// Windows the printer is a stub that answers unsupported.
	printer := pdf.NewPrinter(filepath.Join(dataDir, webviewPDFDir), log)
	workRepo := sqlite.NewWorkRepository(db)
	works, err := turnkey.New(turnkey.Deps{
		Works: workRepo, Sends: workRepo, Assignments: assignments, Materials: lib, Reader: llm,
		Advisor: llm.StepAdvisor(modelAlias), Clock: e.local, IDs: e.ids, Log: log,
		Writing: &turnkey.WritingDeps{
			Writer: llm, Jobs: jobs, JobIDs: e.ids, Canceller: runnerCanceller{c: c}, Profiles: settings,
			Renderer: docx.New(), Slides: pptx.New(), PDF: pdf.NewRenderer(printer), Lessons: c.proposals, Shelf: lib,
			ModelAlias: modelAlias, MaxAttempts: maxAttempts,
		},
	})
	if err != nil {
		return fmt.Errorf("app: turnkey works: %w", err)
	}
	handlers.works = works

	// The knowledge base reads the same local results at query time and groups lessons by library subject; dates of
	// the export in the local time zone. The student's notes (ADR-0027) are read with their texts through the library.
	knowledge, err := knowledgebase.New(knowledgebase.Deps{
		Library: lib, Notes: sqlite.NewNoteRepository(db), NoteTexts: lib, Subjects: lib,
		Lessons: sqlite.NewKnowledgeBase(db), Recordings: recordings, Schedule: lessons, Transcripts: transcripts,
		Artifacts: store, Summaries: summaries, LectureTexts: lectureTexts, Writer: textfile.New(), Clock: e.local, Log: log,
	})
	if err != nil {
		return fmt.Errorf("app: knowledge base: %w", err)
	}
	// Answers with citations (TASK-118): passages of the knowledge base, the ANSWER reader on the Hub, the answers kept
	// locally for their cards; a card marks a citation of a note removed since.
	answers, err := answering.New(answering.Deps{
		Knowledge: knowledge, Reader: llm, Answers: sqlite.NewAnswerRepository(db), Notes: answerNotes{notes: noteRepo},
		Clock: e.clock, IDs: e.ids, Log: log,
	})
	if err != nil {
		return fmt.Errorf("app: answers: %w", err)
	}
	// Exam tickets (TASK-133): one answer with citations per ticket as a job, the file filed into the subject's
	// «Билеты»; the title page from the profile, the lesson dates in the local time zone. The runner that cancels a
	// run is built next.
	ticketSets, err := tickets.New(tickets.Deps{
		Sets: sqlite.NewTicketSetRepository(db), Materials: lib, Answerer: ticketAnswers{answers: answers}, Shelf: lib,
		Jobs: jobs, Canceller: runnerCanceller{c: c}, Profiles: settings, Renderer: docx.New(), Clock: e.local,
		IDs: e.ids, Log: log, MaxAttempts: maxAttempts,
	})
	if err != nil {
		return fmt.Errorf("app: tickets: %w", err)
	}
	handlers.tickets = ticketSets

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
	// The post-processor is the consumer of the STT completion event: it enqueues the follow-up jobs idempotently.
	if err := c.wireSpeech(dataDir, e, transcribe.Deps{
		Transcripts: transcripts, Recordings: recordings, Models: sqlite.NewModelRepository(db), Artifacts: store,
		Events: post, Clock: e.clock, Log: log, Live: parts, Settings: settings, Speeds: speeds,
	}, log); err != nil {
		return err
	}

	recorder, err := e.recorder(dataDir, store, log)
	if err != nil {
		return fmt.Errorf("app: audio recorder: %w", err)
	}
	c.recordings, err = recordingsession.New(recordingsession.Deps{
		Recordings: recordings, Transcripts: sqlite.NewRecordingTranscripts(db), Recorder: recorder,
		Artifacts: store, Tx: db, Clock: e.clock, Sleeper: e.clock, IDs: e.ids, Completions: c.transcriptions, Log: log,
		Lessons: lessons, Apps: e.apps(log), Advice: &c.advice,
	}, e.recordings)
	if err != nil {
		return fmt.Errorf("app: recordings: %w", err)
	}

	// After the runner (it cancels the jobs of a recording) and the recordings (the recovery guard).
	c.deletions, err = recordingdelete.New(recordingdelete.Deps{
		Recordings: recordings, Store: sqlite.NewRecordingDeletions(db), Jobs: jobs, Canceller: c.runner, Artifacts: store,
		Recovery: c.recordings, Clock: e.clock, Sleeper: e.clock, Tx: db,
		Sync: calendarRequest{c: c, log: log, why: "recording deletion"}, Log: log,
	}, recordingdelete.Config{TrackExtension: e.recordings.TrackExtension})
	if err != nil {
		return fmt.Errorf("app: recording deletion: %w", err)
	}
	recordingImport, err := c.wireRecordingImport(e, recordings, lessons, log)
	if err != nil {
		return err
	}

	c.sound, err = soundcheck.New(soundcheck.Deps{
		Checker: levelChecker(recorder), Settings: settings, Recordings: recordingActivity{c.recordings}, Clock: e.clock,
		Log: log,
	}, soundcheck.Config{})
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
	// The time estimates use the processor count the engine runs with (ADR-0016).
	speed, err := sttspeed.New(sttspeed.Deps{
		Models: sqlite.NewModelRepository(db), Catalog: embeddedCatalog{}, Speeds: speeds, Settings: settings,
	}, logicalCPUs())
	if err != nil {
		return fmt.Errorf("app: recognition time estimates: %w", err)
	}
	history, err := activity.New(recordings, jobs, lessons, log, hiddenJobTypes()...)
	if err != nil {
		return fmt.Errorf("app: activity: %w", err)
	}
	history.WithRemaining(speed, e.clock)
	history.WithDraftAssignments(draftNames{c.proposals}) // «ДЗ: <задание> · <предмет>» (TASK-094)
	// Local only: the viewer works while the Hub or the internet is down. Titles in the local time zone.
	viewer, err := transcriptview.New(transcriptview.Deps{
		Recordings: recordings, Lessons: lessons, Transcripts: transcripts, Jobs: jobs, Live: parts, Artifacts: store,
		Writer: textfile.New(), Clock: e.local, Log: log,
	})
	if err != nil {
		return fmt.Errorf("app: transcript viewer: %w", err)
	}
	// Local only as well: the summary and the homework found are read from the post-processing results.
	summaryViewer, err := summaryview.New(summaryview.Deps{
		Recordings: recordings, Lessons: lessons, Transcripts: transcripts, Summaries: summaries,
		LectureTexts: lectureTexts, Assignments: assignments, Jobs: jobs, Writer: textfile.New(), Clock: e.local, Log: log,
	})
	if err != nil {
		return fmt.Errorf("app: summary viewer: %w", err)
	}
	todayScreen, err := c.newTodayScreen(e, lib, log)
	if err != nil {
		return err
	}
	subjectPage, err := c.newSubjectView(dataDir, e, lib, knowledge, history, log)
	if err != nil {
		return err
	}

	if err := c.wireAgent(e, llm, agentReads{
		library: lib, knowledge: knowledge, activity: history, assignments: c.proposals, deliveries: c.deliveries,
		rerunner: rerunner, materials: materialJobs, turnkey: works, answers: answers, prefs: prefs,
	}, log); err != nil {
		return err
	}

	// A model installed while the app runs gets the waiting recordings enqueued at once; without the speech component
	// their jobs wait QUEUED until it is installed (the pages offer it: Speech).
	c.manager = newModelManager(c, c.transcriptions, log, e.stopTimeout)

	// The model manager writes artifacts only after the temporary files of a crashed run are gone.
	c.cleanups = append(c.cleanups, cleanup{name: "artifact temporary files", run: store.CleanupTemp},
		cleanup{name: "model manager writes allowed", run: c.manager.markReady})
	c.ui = wails.Core{
		Settings: prefs, Activity: history, Stopper: c.recordings, Forcer: c.runner, Retrier: rerunner,
		Models: c.manager, Downloads: c.manager, Imports: c.manager, Links: weblink.New(), Speech: c.speech, Speed: speed,
		Sound: c.sound, Bundle: bundle, Google: c.googleSignIn, Calendar: c.calendarSync,
		Homework:   wails.HomeworkCore{Proposals: c.proposals, Drafts: c.drafts, Deliveries: c.deliveries},
		Transcript: viewer,
		Summary:    summaryViewer,
		Deletion:   c.deletions,
		// «Импортировать запись лекции» (TASK-161).
		RecordingImport: recordingImport,
		Knowledge:       knowledge,
		Library:         lib,
		// «Открыть в Word / PowerPoint»: a named copy in the artifact store, started below that root only.
		MaterialViews:   store,
		Opener:          fileopen.New(filepath.Join(dataDir, artifactsDir)),
		SubjectOverview: subjectPage,
		Notes:           ownNotes,
		// Life is set by openCore: the requests live as long as the app.
		Agent: wails.AgentCore{
			Driver: c.driver, Panel: c.panel, Journal: c.journal, Clock: e.clock, Chats: c.chats, FreeWork: c.freeWorks,
			Current: driverCurrent{c.driver},
			// The «@» search reads the same use cases as the screens; a lesson is checked as a mention of a request is.
			Mentions: wails.AgentMentionSources{
				Library: lib, Lectures: knowledge, Assignments: c.proposals, Exists: c.mentions, Local: e.local,
			},
		},
		Today: todayScreen,
		// The confirm cards of the agent: only the student's click here confirms what the agent proposed.
		AgentConfirm: wails.AgentConfirmCore{Proposals: c.proposals, Materials: lib, Deletions: c.deletions},
		Turnkey:      works,
		Answers:      answers,
		Tickets:      ticketSets,
	}
	if err := c.wireConnections(e, settings, mailSecrets, mailer, hubSecrets, llm, log); err != nil {
		return err
	}
	c.ui.Agent.Link = c.ui.Connections // whether the Hub address and key are set
	return c.wireSchedule(e, log)
}

// wireModels builds the model manager (PRD section 8): the installed models, the default, the pin of an STT job, the
// download of a catalog model and the import of a file the user picked. The catalog is the embedded manifest of the
// recommended whisper.cpp models (embeddedCatalog).
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

// draftNames is the DraftAssignments port of the activity screen over the homework use case; mapping only.
type draftNames struct {
	homework *homeworkflow.Service
}

// DraftAssignment returns the title and the subject the assignment shows.
func (n draftNames) DraftAssignment(ctx context.Context, id shared.AssignmentID) (string, string, error) {
	v, err := n.homework.Assignment(ctx, id)
	return v.Title, v.Subject, err
}

// close releases the storage in reverse order of opening. It is safe to call more than once.
func (c *core) close() error {
	var errs []error
	if c.driver != nil {
		c.driver.Close() // its requests write to the database
		c.driver = nil
	}
	if c.components != nil {
		errs = append(errs, c.components.Close())
		c.components = nil
	}
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
