package wails

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"paratech/desktop/internal/domain/jobs/notice"
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

// Core is what the pages and the tray call through the bindings. Every field is required.
type Core struct {
	Settings SettingsUseCase
	Activity ActivityUseCase
	Stopper  RecordingStopper
	Forcer   JobForcer
	Prompt   PromptUseCase         // the recording popup; the tray's "Show recording popup" offers an ad-hoc prompt
	Schedule ScheduleImportUseCase // the schedule import page; the shell shows its open dialog
	Files    ScheduleFileReader    // reads the file chosen for the schedule import

	Connections ConnectionsUseCase // the mail and Hub section of the settings page
}

// ui is the running platform shell; window commands are forwarded to it.
type ui interface {
	showPopup() error
	hidePopup() error
	showMain() error
	notify(ctx context.Context, t toast) error
	canNotify() bool
	pickScheduleFile() (string, error)
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

// Notify shows a Windows notification about a finished job (PRD section 15) with fixed texts for its type and
// status; it is the notify.Notifier port. Windows applies the user's notification settings (the per-app switch,
// Focus Assist / Do Not Disturb). It returns when ctx ends even if Windows is still busy with the toast.
func (s *Shell) Notify(ctx context.Context, n *notice.Notice) error {
	if n == nil {
		return ErrNoNotice
	}
	u, err := s.current()
	if err != nil {
		return err
	}
	return u.notify(ctx, toastFor(n))
}

// Ready reports whether Notify can show a toast now: the shell runs and Windows notifications were set up. It is false
// before Run, after the shell stopped and on other systems (the notify.Notifier port).
func (s *Shell) Ready() bool {
	u, err := s.current()
	return err == nil && u.canNotify()
}

// bindings are the services bound to the pages; Wails needs each one as its concrete type.
type bindings struct {
	settings *SettingsService
	activity *ActivityService
	prompt   *PromptService
	schedule *ScheduleImportService

	connections *ConnectionsService
}

// openBindings opens the core and builds the services bound to the pages.
func (s *Shell) openBindings(ctx context.Context) (bindings, error) {
	core, err := s.opts.OpenCore(ctx)
	if err != nil {
		return bindings{}, fmt.Errorf("wails: open core: %w", err)
	}
	if core.Settings == nil {
		return bindings{}, errors.New("wails: open core: no settings use case")
	}
	if core.Activity == nil {
		return bindings{}, errors.New("wails: open core: no activity use case")
	}
	if core.Stopper == nil {
		return bindings{}, errors.New("wails: open core: no recording stopper")
	}
	if core.Forcer == nil {
		return bindings{}, errors.New("wails: open core: no job forcer")
	}
	if core.Prompt == nil {
		return bindings{}, errors.New("wails: open core: no recording prompt use case")
	}
	if core.Schedule == nil {
		return bindings{}, errors.New("wails: open core: no schedule import use case")
	}
	if core.Files == nil {
		return bindings{}, errors.New("wails: open core: no schedule file reader")
	}
	if core.Connections == nil {
		return bindings{}, errors.New("wails: open core: no connections use case")
	}
	return bindings{
		settings: NewSettingsService(core.Settings, s.opts.Log),
		activity: NewActivityService(core.Activity, core.Stopper, core.Forcer, s.opts.Log),
		prompt:   NewPromptService(core.Prompt, s.opts.Log),
		schedule: NewScheduleImportService(core.Schedule, core.Files, pickerFunc(s.pickScheduleFile), s.opts.Log),

		connections: NewConnectionsService(core.Connections, s.opts.Log),
	}, nil
}

// pickScheduleFile shows the open dialog of the schedule import over the main window; "" when the user cancels.
func (s *Shell) pickScheduleFile(context.Context) (string, error) {
	u, err := s.current()
	if err != nil {
		return "", err
	}
	return u.pickScheduleFile()
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
