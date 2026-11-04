package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paratech/desktop/config"
	"paratech/desktop/internal/adapters/e2ehooks"
	"paratech/desktop/internal/input/wails"
)

// Outside the E2E mode the shell keeps its native dialogs and openers; in it one e2ehooks adapter has them all.
func TestE2EHooks(t *testing.T) {
	d := newE2EDirs(t)
	normal := d.config(false)
	normal.E2E = config.E2E{}
	if got := e2eHooks(normal, slog.New(slog.DiscardHandler)); got != (wails.Hooks{}) {
		t.Fatalf("normal mode hooks %+v, want none", got)
	}

	got := e2eHooks(d.config(false), slog.New(slog.DiscardHandler))
	h, ok := got.Open.(*e2ehooks.Hooks)
	if !ok {
		t.Fatalf("open dialogs %T, want the e2ehooks adapter", got.Open)
	}
	if got.Save != wails.SaveDialogs(h) || got.Clipboard != wails.Clipboard(h) || got.Links != wails.LinkOpener(h) ||
		got.Opener != wails.FileOpener(h) {
		t.Fatalf("hooks %+v, want one adapter for all", got)
	}
}

// The pick queue names the dialogs by the shell's names.
func TestE2EDialogNames(t *testing.T) {
	pairs := [][2]string{
		{wails.DialogSchedule, e2ehooks.DialogSchedule},
		{wails.DialogModel, e2ehooks.DialogModel},
		{wails.DialogMaterial, e2ehooks.DialogMaterial},
		{wails.DialogNotes, e2ehooks.DialogNotes},
		{wails.DialogRecording, e2ehooks.DialogRecording},
	}
	for _, p := range pairs {
		if p[0] != p[1] {
			t.Errorf("shell dialog %q, pick queue dialog %q", p[0], p[1])
		}
	}
}

// In the E2E mode a help link of the real wiring is recorded, not opened. The context is cancelled, so the native
// opener could not start explorer.exe even if it were wired: it would fail instead of recording.
func TestE2ELinksAreRecorded(t *testing.T) {
	d := newE2EDirs(t)
	a, err := New(d.config(false), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if a.shell == nil {
		t.Fatal("no UI shell")
	}
	a.env, _ = testEnv(t)
	t.Cleanup(func() { _ = a.Close() })

	b, err := a.shell.OpenServices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Help.OpenHelpPage(ctx, "google-syncselect"); err != nil {
		t.Fatalf("OpenHelpPage = %v, want it recorded", err)
	}
	opened, err := os.ReadFile(filepath.Join(d.run, "opened.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(opened), `"kind":"link","value":"https://www.google.com/calendar/syncselect"`) {
		t.Fatalf("opened.jsonl:\n%s", opened)
	}
}
