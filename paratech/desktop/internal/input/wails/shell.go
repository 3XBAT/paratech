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
	Log        *slog.Logger
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
	Prompt   PromptUseCase         // the recording popup; the tray's "Show recording popup" offers an ad-hoc prompt
	Schedule ScheduleImportUseCase // the schedule import page; the shell shows its open dialog
	Files    ScheduleFileReader    // reads the file chosen for the schedule import

	// The models page; the shell shows its open dialog.
	Models      ModelCatalogUseCase
	Downloads   ModelDownloads
	Imports     ModelImports
	Links       LinkOpener // also opens the pages of the help screens
	SpeechReady bool       // whisper-cli was found at start

	Connections ConnectionsUseCase // the mail and Hub section of the settings page
	Google      GoogleUseCase      // the Google section of the same page

	Sound  SoundCheckUseCase    // "Проверить звук" of the diagnostics page
	Bundle SupportBundleUseCase // "Сохранить пакет для поддержки"; the shell shows its save dialog
}

// ui is the running platform shell; window commands are forwarded to it.
type ui interface {
	showPopup() error
	hidePopup() error
	showMain() error
	pickScheduleFile() (string, error)
	pickModelFile() (string, error)
	pickBundleFile(name string) (string, error)
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
	Schedule *ScheduleImportService
	Models   *ModelsService

	Connections *ConnectionsService
	Google      *GoogleService
	Diagnostics *DiagnosticsService
	Help        *HelpService
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
	if core.Connections == nil {
		return Services{}, errors.New("wails: open core: no connections use case")
	}
	if core.Google == nil {
		return Services{}, errors.New("wails: open core: no google sign-in use case")
	}
	if core.Sound == nil || core.Bundle == nil {
		return Services{}, errors.New("wails: open core: no diagnostics use cases")
	}
	return Services{
		Settings: NewSettingsService(core.Settings, s.opts.Log),
		Activity: NewActivityService(core.Activity, core.Stopper, core.Forcer, s.opts.Log),
		Prompt:   NewPromptService(core.Prompt, s.opts.Log),
		Schedule: NewScheduleImportService(core.Schedule, core.Files, pickerFunc(s.pickScheduleFile), s.opts.Log),
		Models: NewModelsService(ModelsDeps{
			Catalog: core.Models, Downloads: core.Downloads, Imports: core.Imports, Links: core.Links,
			Picker: modelPickerFunc(s.pickModelFile), SpeechReady: core.SpeechReady,
		}, s.opts.Log),

		Connections: NewConnectionsService(core.Connections, s.opts.Log),
		Google:      NewGoogleService(core.Google, s.opts.Log),
		Diagnostics: NewDiagnosticsService(DiagnosticsDeps{
			Sound: core.Sound, Bundle: core.Bundle, Picker: bundlePickerFunc(s.pickBundleFile),
		}, s.opts.Log),
		Help: NewHelpService(core.Links, s.opts.Log),
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
