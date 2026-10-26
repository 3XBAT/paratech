package wails

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

var (
	// ErrUnsupported means there is no UI shell on this operating system (only Windows has one).
	ErrUnsupported = errors.New("wails: the UI shell is supported only on Windows")
	// ErrNotRunning means a window command arrived before Run started the shell or after it stopped.
	ErrNotRunning = errors.New("wails: the UI shell is not running")
	// ErrAlreadyStarted means Run was called twice; Wails allows one application per process.
	ErrAlreadyStarted = errors.New("wails: the UI shell was already started")
)

// Options configures the shell.
type Options struct {
	// Name is the application name used for window titles and the tray tooltip.
	Name string
	// InstanceID identifies the application for the single-instance lock, e.g. "com.example.app".
	InstanceID string
	// WebviewDataDir is the WebView2 user data folder; empty uses the Wails default, %APPDATA%\<exe name>.
	WebviewDataDir string
	Log            *slog.Logger
	// OpenCore opens the core (database, use cases) and returns what the pages bind to. Run calls it once, after the
	// single-instance check: a second instance exits before it and never touches the database (ADR-0007).
	OpenCore func(ctx context.Context) (Core, error)
	// Started, if set, is called once when the application runs (Wails' ApplicationStarted), on a Wails goroutine:
	// it must return at once, so the app starts its background work there without blocking the UI.
	Started func()
}

// Core is what the pages and the tray call through the bindings. Every interface field is required.
type Core struct {
	Settings SettingsUseCase
	Activity ActivityUseCase
	Stopper  RecordingStopper
	Forcer   JobForcer
	Retrier  PostProcessRetrier    // «Повторить» and «Сделать конспект заново» of the recording rows (TASK-086)
	Prompt   PromptUseCase         // the recording popup; the tray's "Show recording popup" offers an ad-hoc prompt
	Schedule ScheduleImportUseCase // the schedule import page; the shell shows its open dialog
	Files    ScheduleFileReader    // reads the file chosen for the schedule import

	// The models page; the shell shows its open dialog.
	Models    ModelCatalogUseCase
	Downloads ModelDownloads
	Imports   ModelImports
	Links     LinkOpener             // also opens the pages of the help screens
	Speech    SpeechComponentUseCase // the speech recognition component: status and install, on both pages
	Speed     SpeedEstimates         // the recognition time estimates of the models and settings pages; optional

	Connections ConnectionsUseCase  // the mail and Hub section of the settings page
	Google      GoogleUseCase       // the Google section of the same page
	Calendar    CalendarSyncUseCase // the calendar synchronization of the Google section

	Sound  SoundCheckUseCase    // "Проверить звук" of the diagnostics page
	Bundle SupportBundleUseCase // "Сохранить пакет для поддержки"; the shell shows its save dialog

	Homework HomeworkCore // the homework page: drafts, the send confirmation and the deliveries

	Transcript TranscriptUseCase // the transcript viewer; the shell shows its save dialog and plays the recordings
	Summary    SummaryUseCase    // the summary viewer; the shell shows its save dialog

	Deletion RecordingDeleteUseCase // «Удалить» of the recording rows and the summary screen (TASK-095)

	Knowledge KnowledgeUseCase // the «База знаний» section (TASK-091); the shell shows its save dialog

	Library LibraryUseCase // «Предметы» (TASK-121); the shell shows its open dialog, Files reads the chosen file
	// «Открыть в Word / PowerPoint» of a library file (TASK-177): the artifact store keeps the copy, Opener starts it.
	MaterialViews MaterialViews
	Opener        FileOpener

	Agent AgentCore // the agent panel (TASK-119); the shell attaches its panel to the Wails events

	Today TodayUseCase // «Сегодня» (TASK-125)

	AgentConfirm AgentConfirmCore // the confirm cards of the agent panel (TASK-127)

	Turnkey TurnkeyUseCase // the turnkey cards and the helper page (TASK-129)

	Answers AnswersUseCase // the answer cards of the agent panel (TASK-118)

	Tickets TicketsUseCase // the tickets screen (TASK-133)
}

// ui is the running platform shell; window commands are forwarded to it.
type ui interface {
	showPopup() error
	hidePopup() error
	fitPopup(height int) (int, error)
	showMain() error
	pickScheduleFile() (string, error)
	pickModelFile() (string, error)
	pickBundleFile(name string) (string, error)
	pickTranscriptFile(name, format string) (string, error)
	pickSummaryFile(name, format string) (string, error)
	pickDocxFile(name string) (string, error)
	pickKnowledgeFile(name string) (string, error)
	pickMaterialFile() (string, error)
	setClipboardText(text string) error
}

// Shell owns the UI: the main window, the tray icon and the popup. Create it with New. Window commands are safe
// from any goroutine; Run must be called once, from the main goroutine.
type Shell struct {
	opts Options

	mu      sync.Mutex
	started bool
	ui      ui
}

// New validates the options. It creates no windows and starts nothing; that happens in Run.
func New(opts Options) (*Shell, error) {
	var errs []error
	if opts.Name == "" {
		errs = append(errs, errors.New("wails: name is required"))
	}
	if opts.InstanceID == "" {
		errs = append(errs, errors.New("wails: instance id is required"))
	}
	if opts.Log == nil {
		errs = append(errs, errors.New("wails: logger is required"))
	}
	if opts.OpenCore == nil {
		errs = append(errs, errors.New("wails: core opener is required"))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return &Shell{opts: opts}, nil
}

// Run shows the tray icon and the main window and blocks until ctx is cancelled or the user quits from the tray.
// Wails needs the main OS thread (its package init locks it), so Run must be called from the main goroutine.
// A second instance of the program does not return from Run: Wails notifies the first instance and exits.
func (s *Shell) Run(ctx context.Context) error {
	if err := s.markStarted(); err != nil {
		return err
	}
	defer s.setUI(nil)

	return s.run(ctx)
}

// ShowPopup shows the recording popup without activating it and emits PromptShownEvent, so the page reads the current
// prompt. It is the Popup port of the recording prompt.
func (s *Shell) ShowPopup() error {
	u, err := s.current()
	if err != nil {
		return err
	}
	return u.showPopup()
}

// HidePopup hides the recording popup.
func (s *Shell) HidePopup() error {
	u, err := s.current()
	if err != nil {
		return err
	}
	return u.hidePopup()
}

// fitPopup resizes the popup for its content height and returns the height it got.
func (s *Shell) fitPopup(height int) (int, error) {
	u, err := s.current()
	if err != nil {
		return 0, err
	}
	return u.fitPopup(height)
}

// ShowMain shows the main window and brings it to the front.
func (s *Shell) ShowMain() error {
	u, err := s.current()
	if err != nil {
		return err
	}
	return u.showMain()
}

// Services are the services bound to the pages; Wails needs each one as its concrete type.
type Services struct {
	Settings *SettingsService
	Activity *ActivityService
	Prompt   *PromptService
	Popup    *PopupWindowService
	Schedule *ScheduleImportService
	Models   *ModelsService
	Speech   *SpeechService

	Connections *ConnectionsService
	Google      *GoogleService
	Calendar    *GoogleCalendarService
	Diagnostics *DiagnosticsService
	Help        *HelpService
	Homework    *HomeworkService
	Transcript  *TranscriptService
	Summary     *SummaryService
	LectureText *LectureTextService
	Deletion    *RecordingDeleteService
	Knowledge   *KnowledgeBaseService
	Library     *LibraryService
	Agent       *AgentService
	Today       *TodayService

	AgentConfirm *AgentConfirmService
	Turnkey      *TurnkeyService
	Answers      *AnswerService
	Tickets      *TicketService

	// AgentPanel is not bound: the shell attaches it to the Wails events while the app runs.
	AgentPanel *AgentPanel
}

// OpenServices opens the core and builds the services bound to the pages, all logging to Options.Log. Run calls it
// once; the tests of the app call it to check the wiring without a window.
func (s *Shell) OpenServices(ctx context.Context) (Services, error) {
	core, err := s.opts.OpenCore(ctx)
	if err != nil {
		return Services{}, fmt.Errorf("wails: open core: %w", err)
	}
	if core.Settings == nil {
		return Services{}, errors.New("wails: open core: no settings use case")
	}
	if core.Activity == nil {
		return Services{}, errors.New("wails: open core: no activity use case")
	}
	if core.Stopper == nil {
		return Services{}, errors.New("wails: open core: no recording stopper")
	}
	if core.Forcer == nil {
		return Services{}, errors.New("wails: open core: no job forcer")
	}
	if core.Retrier == nil {
		return Services{}, errors.New("wails: open core: no post-processing retrier")
	}
	if core.Prompt == nil {
		return Services{}, errors.New("wails: open core: no recording prompt use case")
	}
	if core.Schedule == nil {
		return Services{}, errors.New("wails: open core: no schedule import use case")
	}
	if core.Files == nil {
		return Services{}, errors.New("wails: open core: no schedule file reader")
	}
	if core.Models == nil || core.Downloads == nil || core.Imports == nil || core.Links == nil {
		return Services{}, errors.New("wails: open core: no model manager")
	}
	if core.Speech == nil {
		return Services{}, errors.New("wails: open core: no speech component use case")
	}
	if core.Connections == nil {
		return Services{}, errors.New("wails: open core: no connections use case")
	}
	if core.Google == nil {
		return Services{}, errors.New("wails: open core: no google sign-in use case")
	}
	if core.Calendar == nil {
		return Services{}, errors.New("wails: open core: no calendar sync use case")
	}
	if core.Sound == nil || core.Bundle == nil {
		return Services{}, errors.New("wails: open core: no diagnostics use cases")
	}
	if !core.Homework.complete() {
		return Services{}, errors.New("wails: open core: no homework use cases")
	}
	if core.Transcript == nil {
		return Services{}, errors.New("wails: open core: no transcript viewer")
	}
	if core.Summary == nil {
		return Services{}, errors.New("wails: open core: no summary viewer")
	}
	if core.Deletion == nil {
		return Services{}, errors.New("wails: open core: no recording deletion")
	}
	if core.Knowledge == nil {
		return Services{}, errors.New("wails: open core: no knowledge base")
	}
	if core.Library == nil || core.MaterialViews == nil || core.Opener == nil {
		return Services{}, errors.New("wails: open core: no library")
	}
	if !core.Agent.complete() {
		return Services{}, errors.New("wails: open core: no agent")
	}
	if core.Today == nil {
		return Services{}, errors.New("wails: open core: no today screen")
	}
	if !core.AgentConfirm.complete() {
		return Services{}, errors.New("wails: open core: no agent confirm cards")
	}
	if core.Turnkey == nil {
		return Services{}, errors.New("wails: open core: no turnkey work")
	}
	if core.Answers == nil {
		return Services{}, errors.New("wails: open core: no answers")
	}
	if core.Tickets == nil {
		return Services{}, errors.New("wails: open core: no tickets")
	}
	google := NewGoogleService(core.Google, s.opts.Log)
	google.window = s
	return Services{
		Settings: NewSettingsService(core.Settings, s.opts.Log).withSpeed(core.Speed),
		Activity: NewActivityService(core.Activity, core.Stopper, core.Forcer, core.Retrier, s.opts.Log),
		Prompt:   NewPromptService(core.Prompt, s.opts.Log),
		Popup:    NewPopupWindowService(popupFitterFunc(s.fitPopup), s.opts.Log),
		Schedule: NewScheduleImportService(core.Schedule, core.Files, pickerFunc(s.pickScheduleFile), s.opts.Log),
		Models: NewModelsService(ModelsDeps{
			Catalog: core.Models, Downloads: core.Downloads, Imports: core.Imports, Links: core.Links,
			Picker: modelPickerFunc(s.pickModelFile), Speech: core.Speech, Speed: core.Speed,
		}, s.opts.Log),
		Speech: NewSpeechService(core.Speech, s.opts.Log),

		Connections: NewConnectionsService(core.Connections, s.opts.Log),
		Google:      google,
		Calendar:    NewGoogleCalendarService(core.Calendar, s.opts.Log),
		Diagnostics: NewDiagnosticsService(DiagnosticsDeps{
			Sound: core.Sound, Bundle: core.Bundle, Picker: bundlePickerFunc(s.pickBundleFile),
		}, s.opts.Log),
		Help:     NewHelpService(core.Links, s.opts.Log),
		Homework: NewHomeworkService(core.Homework, s.opts.Log).withDocxPicker(docxPickerFunc(s.pickDocxFile)),
		Transcript: NewTranscriptService(core.Transcript, transcriptPickerFunc(s.pickTranscriptFile),
			clipboardFunc(s.setClipboardText), s.opts.Log),
		Summary: NewSummaryService(core.Summary, summaryPickerFunc(s.pickSummaryFile), clipboardFunc(s.setClipboardText),
			s.opts.Log),
		LectureText: NewLectureTextService(core.Summary, summaryPickerFunc(s.pickSummaryFile),
			clipboardFunc(s.setClipboardText), s.opts.Log),
		Deletion:  NewRecordingDeleteService(core.Deletion, s.opts.Log),
		Knowledge: NewKnowledgeBaseService(core.Knowledge, knowledgePickerFunc(s.pickKnowledgeFile), s.opts.Log),
		Library: NewLibraryService(core.Library, materialPickerFunc(s.pickMaterialFile), core.Files, s.opts.Log).
			withOpener(core.MaterialViews, core.Opener),
		Agent:      NewAgentService(core.Agent, s.opts.Log),
		Today:      NewTodayService(core.Today, s.opts.Log),
		AgentPanel: core.Agent.Panel,

		AgentConfirm: NewAgentConfirmService(core.AgentConfirm, s.opts.Log),
		Turnkey:      NewTurnkeyService(core.Turnkey, s.opts.Log),
		Answers:      NewAnswerService(core.Answers, s.opts.Log),
		Tickets:      NewTicketService(core.Tickets, s.opts.Log),
	}, nil
}

// WailsLogger is the logger Wails gets: Options.Log behind the Info floor, as Wails logs binding arguments at Debug.
func (s *Shell) WailsLogger() *slog.Logger { return wailsLogger(s.opts.Log) }

// pickScheduleFile shows the open dialog of the schedule import over the main window; "" when the user cancels.
func (s *Shell) pickScheduleFile(context.Context) (string, error) {
	u, err := s.current()
	if err != nil {
		return "", err
	}
	return u.pickScheduleFile()
}

// pickModelFile shows the open dialog of the model import over the main window; "" when the user cancels.
func (s *Shell) pickModelFile(context.Context) (string, error) {
	u, err := s.current()
	if err != nil {
		return "", err
	}
	return u.pickModelFile()
}

// pickBundleFile shows the save dialog of the support bundle over the main window; "" when the user cancels.
func (s *Shell) pickBundleFile(_ context.Context, name string) (string, error) {
	u, err := s.current()
	if err != nil {
		return "", err
	}
	return u.pickBundleFile(name)
}

// pickTranscriptFile shows the save dialog of a transcript over the main window; "" when the user cancels.
func (s *Shell) pickTranscriptFile(_ context.Context, name, format string) (string, error) {
	u, err := s.current()
	if err != nil {
		return "", err
	}
	return u.pickTranscriptFile(name, format)
}

// pickSummaryFile shows the save dialog of a summary over the main window; "" when the user cancels.
func (s *Shell) pickSummaryFile(_ context.Context, name, format string) (string, error) {
	u, err := s.current()
	if err != nil {
		return "", err
	}
	return u.pickSummaryFile(name, format)
}

// pickDocxFile shows the save dialog of a Word file over the main window; "" when the user cancels.
func (s *Shell) pickDocxFile(_ context.Context, name string) (string, error) {
	u, err := s.current()
	if err != nil {
		return "", err
	}
	return u.pickDocxFile(name)
}

// pickKnowledgeFile shows the save dialog of an exported subject over the main window; "" when the user cancels.
func (s *Shell) pickKnowledgeFile(_ context.Context, name string) (string, error) {
	u, err := s.current()
	if err != nil {
		return "", err
	}
	return u.pickKnowledgeFile(name)
}

// pickMaterialFile shows the open dialog of a file of the library over the main window; "" when the user cancels.
func (s *Shell) pickMaterialFile(context.Context) (string, error) {
	u, err := s.current()
	if err != nil {
		return "", err
	}
	return u.pickMaterialFile()
}

// setClipboardText puts text on the system clipboard.
func (s *Shell) setClipboardText(_ context.Context, text string) error {
	u, err := s.current()
	if err != nil {
		return err
	}
	return u.setClipboardText(text)
}

func (s *Shell) markStarted() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return ErrAlreadyStarted
	}
	s.started = true
	return nil
}

func (s *Shell) setUI(u ui) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ui = u
}

func (s *Shell) current() (ui, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ui == nil {
		return nil, ErrNotRunning
	}
	return s.ui, nil
}
