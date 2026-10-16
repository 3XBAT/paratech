package app

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paratech/desktop/config"
	"paratech/desktop/internal/adapters/instancelock"
	"paratech/desktop/internal/adapters/logfile"
)

// uiTeeApp is a UI-mode app as New builds it from main's logger (a console at level): the tee, the shell and its
// Wails logger come from New; the environment is the test one with the real log file. The shell never runs, so no
// window appears.
func uiTeeApp(t *testing.T, dir string, level slog.Level) (*App, *fakes, *syncBuffer) {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir, cfg.LogLevel = dir, "debug"
	console := &syncBuffer{}
	a, err := New(cfg, slog.New(slog.NewTextHandler(console, &slog.HandlerOptions{Level: level})))
	if err != nil {
		t.Fatal(err)
	}
	if a.shell == nil {
		t.Fatal("UI mode built no shell")
	}
	var f *fakes
	a.env, f = testEnv(t)
	a.env.logFile = openLogFile
	t.Cleanup(func() { _ = a.Close() }) // before TempDir removal: Windows cannot delete open files
	return a, f, console
}

// teeApp is a headless test app whose logger is the tee of New on a console at level, with the real log file.
func teeApp(t *testing.T, dir string, level slog.Level) (*App, *syncBuffer) {
	t.Helper()
	a, _ := newTestAppWith(t, dir)
	console := &syncBuffer{}
	a.tee = logfile.NewTee(slog.NewTextHandler(console, &slog.HandlerOptions{Level: level}))
	a.log = slog.New(a.tee)
	a.env.logFile = openLogFile
	return a, console
}

func logFileText(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, logsDir, logfile.Name()))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The log file starts with the instance lock, gets the records of the console's level and is closed by Close.
func TestLogFileAfterTheLock(t *testing.T) {
	dir := t.TempDir()
	a, console := teeApp(t, dir, slog.LevelInfo)
	a.log.Info("before the core")
	startCore(t, a)
	a.log.Debug("below the level")
	a.log.Info("while running")
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a.log.Info("after the close")

	got := logFileText(t, dir)
	for _, want := range []string{`msg="instance lock acquired"`, `msg="background work started"`, `msg="while running"`} {
		if !strings.Contains(got, want) {
			t.Errorf("the log file lacks %s:\n%s", want, got)
		}
	}
	for _, not := range []string{"before the core", "below the level", "after the close"} {
		if strings.Contains(got, not) {
			t.Errorf("the log file has %q", not)
		}
	}
	if !strings.Contains(console.String(), "after the close") {
		t.Fatal("the console stopped after the close")
	}
}

// A second instance never writes into the log of the live one.
func TestSecondInstanceWritesNoLogFile(t *testing.T) {
	dir := t.TempDir()
	first, _ := teeApp(t, dir, slog.LevelInfo)
	startCore(t, first)

	second, _ := teeApp(t, dir, slog.LevelInfo)
	if _, err := second.openCore(context.Background()); !errors.Is(err, instancelock.ErrHeld) {
		t.Fatalf("second openCore = %v, want ErrHeld", err)
	}
	second.log.Info("second instance line")
	if got := logFileText(t, dir); strings.Contains(got, "second instance line") {
		t.Fatalf("the second instance wrote the log file:\n%s", got)
	}
}

// A log file that cannot be opened is a warning on the console; the core starts.
func TestLogFileFailureIsAWarning(t *testing.T) {
	dir := t.TempDir()
	a, console := teeApp(t, dir, slog.LevelInfo)
	a.env.logFile = func(string) (logSink, error) { return nil, errors.New("logfile: the log file failed: access denied") }
	startCore(t, a)
	if !strings.Contains(console.String(), "the log file cannot be opened") {
		t.Fatalf("console:\n%s", console.String())
	}
	if _, err := os.Stat(filepath.Join(dir, logsDir)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("logs directory: %v", err)
	}
}

// New routes every logger through the tee, the one the UI shell (and its Wails floor) gets included.
func TestNewTeesTheLogger(t *testing.T) {
	a, err := New(config.Default(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if a.tee == nil || a.log.Handler() != slog.Handler(a.tee) {
		t.Fatalf("handler %T, want the tee", a.log.Handler())
	}
	if a.env.logFile == nil {
		t.Fatal("the production env has no log file")
	}
}

// In UI mode the shell, its page services and the Wails logger write through the tee, so their records reach the log
// file (and the support bundle); the Wails logger keeps its Info floor in front of the file even at --log-level debug:
// its Debug line "Binding call complete" carries the binding arguments, the mail password among them.
func TestUIShellLogsThroughTheTee(t *testing.T) {
	const secret = "wails-debug-secret-5b2e"
	ctx := context.Background()
	dir := t.TempDir()
	a, _, console := uiTeeApp(t, dir, slog.LevelDebug)

	svc, err := a.shell.OpenServices(ctx) // opens the core: the log file is attached from here on
	if err != nil {
		t.Fatal(err)
	}
	// The shell does not run, so the save dialog fails and the diagnostics service logs a warning.
	if got, err := svc.Diagnostics.SaveSupportBundle(ctx); err != nil || got.Status != "failed" {
		t.Fatalf("SaveSupportBundle = %+v, %v", got, err)
	}
	wl := a.shell.WailsLogger()
	wl.Debug("Binding call complete:", "args", `["`+secret+`"]`)
	wl.Info("wails info line")
	a.log.Debug("app debug line")

	got := logFileText(t, dir)
	for _, want := range []string{`msg="ui: choose where to save the support bundle"`, `msg="wails info line"`, `msg="app debug line"`} {
		if !strings.Contains(got, want) {
			t.Errorf("the log file lacks %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, secret) || strings.Contains(console.String(), secret) {
		t.Fatalf("the Wails Debug line passed its floor:\nfile:\n%s\nconsole:\n%s", got, console.String())
	}
}
