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
	"paratech/desktop/internal/adapters/weblink"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/input/wails"
)

// Outside the E2E mode the shell keeps its native dialogs and openers; in it one e2ehooks adapter has them all.
func TestE2EHooks(t *testing.T) {
	d := newE2EDirs(t)
	normal := d.config(false)
	normal.E2E = config.E2E{}
	if r := e2eRecorder(normal, slog.New(slog.DiscardHandler)); r != nil {
		t.Fatalf("normal mode recorder %+v, want none", r)
	}
	if got := e2eHooks(nil); got != (wails.Hooks{}) {
		t.Fatalf("normal mode hooks %+v, want none", got)
	}

	r := e2eRecorder(d.config(false), slog.New(slog.DiscardHandler))
	got := e2eHooks(r)
	h, ok := got.Open.(*e2ehooks.Hooks)
	if !ok || h != r {
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

// «Подключиться» of the library: the native weblink outside the E2E mode; in it the recorder of the shell's hooks,
// so a call link of the wired core is recorded, not opened. The opener is checked to be the recorder before the open,
// so a wrong wiring fails the test instead of starting a browser.
func TestE2ECallLinksAreRecorded(t *testing.T) {
	d := newE2EDirs(t)
	normal := d.config(true)
	normal.E2E = config.E2E{}
	n, err := New(normal, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := n.coreEnv().calls.(*weblink.Opener); !ok || n.e2e != nil {
		t.Fatalf("normal mode call opener %T, recorder %v; want weblink and none", n.coreEnv().calls, n.e2e)
	}

	a, err := New(d.config(false), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	a.env, _ = testEnv(t)
	t.Cleanup(func() { _ = a.Close() })
	if got, ok := a.coreEnv().calls.(*e2ehooks.Hooks); !ok || got != a.e2e || a.e2e == nil {
		t.Fatalf("e2e call opener %T, want the recorder", a.coreEnv().calls)
	}

	ctx := context.Background()
	if _, err := a.openCore(ctx); err != nil {
		t.Fatal(err)
	}
	const link = "https://meet.example.org/j/1?pwd=FakePwd42"
	sub, err := a.core.library.CreateSubjectWithCallLink(ctx, library.NewSubject{Name: "История"}, link)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.core.library.OpenCallLink(ctx, sub.ID()); err != nil {
		t.Fatalf("OpenCallLink = %v, want it recorded", err)
	}
	opened, err := os.ReadFile(filepath.Join(d.run, "opened.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(opened), `"kind":"call","value":"`+link+`"`) {
		t.Fatalf("opened.jsonl:\n%s", opened)
	}
}
