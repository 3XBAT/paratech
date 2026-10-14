package wails

import (
	"context"
	"errors"
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
}

// ui is the running platform shell; window commands are forwarded to it.
type ui interface {
	showPopup() error
	hidePopup() error
	showMain() error
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

// ShowPopup shows the recording popup without activating it.
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
