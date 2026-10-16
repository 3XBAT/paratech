package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paratech/desktop/config"
)

// fakeScheme records the executables it was asked to register.
type fakeScheme struct {
	calls []string
	wrote bool
	err   error
}

func (f *fakeScheme) Ensure(exe string) (bool, error) {
	f.calls = append(f.calls, exe)
	return f.wrote, f.err
}

func TestEnsureURLScheme(t *testing.T) {
	const exe = `C:\Users\Student\AppData\Local\Programs\LearningAssistant\learning-assistant.exe`
	found := func() (string, error) { return exe, nil }
	tests := []struct {
		name       string
		scheme     *fakeScheme
		executable func() (string, error)
		wantCalls  int
		wantLog    string
	}{
		{name: "registered now", scheme: &fakeScheme{wrote: true}, executable: found, wantCalls: 1, wantLog: "level=INFO msg=\"paratech: links registered"},
		{name: "already registered", scheme: &fakeScheme{}, executable: found, wantCalls: 1},
		{name: "registry fails", scheme: &fakeScheme{err: errors.New("urlscheme: registry access failed")}, executable: found,
			wantCalls: 1, wantLog: "level=WARN msg=\"paratech: links are not registered\""},
		{name: "no executable", scheme: &fakeScheme{}, executable: func() (string, error) { return "", errors.New(exe) },
			wantLog: "level=WARN msg=\"paratech: links are not registered: the executable path is unknown\""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs strings.Builder
			ensureURLScheme(tt.scheme, tt.executable, slog.New(slog.NewTextHandler(&logs, nil)))
			if len(tt.scheme.calls) != tt.wantCalls || (tt.wantCalls > 0 && tt.scheme.calls[0] != exe) {
				t.Fatalf("calls %q, want %d with the executable", tt.scheme.calls, tt.wantCalls)
			}
			if !strings.Contains(logs.String(), tt.wantLog) || (tt.wantLog == "" && logs.Len() != 0) {
				t.Fatalf("logs %q, want %q", logs.String(), tt.wantLog)
			}
			if strings.Contains(logs.String(), "Student") {
				t.Fatalf("the log holds the path: %q", logs.String())
			}
		})
	}
	ensureURLScheme(nil, found, slog.New(slog.DiscardHandler)) // tests and headless runs: nothing to call
}

// Only the instance with the UI registers the links, with its own executable; a headless one does not.
func TestOpenCoreRegistersTheSchemeInUIModeOnly(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, headless := range []bool{false, true} {
		cfg := config.Default()
		cfg.Headless, cfg.DataDir = headless, filepath.Join(t.TempDir(), "data")
		a, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		a.env, _ = testEnv(t)
		scheme := &fakeScheme{}
		a.env.scheme = scheme
		if _, err := a.openCore(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := a.Close(); err != nil {
			t.Fatal(err)
		}
		want := 1
		if headless {
			want = 0
		}
		if len(scheme.calls) != want || (want == 1 && scheme.calls[0] != exe) {
			t.Fatalf("headless %v: calls %q, want %d with %q", headless, scheme.calls, want, exe)
		}
	}
}

// The production environment registers through the urlscheme adapter; tests never do (testEnv leaves it nil).
func TestProductionEnvRegistersTheScheme(t *testing.T) {
	if productionEnv().scheme == nil {
		t.Fatal("the production env has no scheme registrar")
	}
	if e, _ := testEnv(t); e.scheme != nil {
		t.Fatal("the test env would write the registry")
	}
}
