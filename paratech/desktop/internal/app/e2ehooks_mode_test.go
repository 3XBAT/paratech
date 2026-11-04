package app

import (
	"log/slog"
	"testing"

	"paratech/desktop/config"
	"paratech/desktop/internal/adapters/weblink"
	"paratech/desktop/internal/input/wails"
)

// Outside the E2E mode, in every build, the shell keeps its native dialogs and openers and «Подключиться» the weblink.
func TestE2ECallOpenerOutsideMode(t *testing.T) {
	d := newE2EDirs(t)
	normal := d.config(true)
	normal.E2E = config.E2E{}
	if r := newE2ERecorder(normal, slog.New(slog.DiscardHandler)); r != nil {
		t.Fatalf("normal mode recorder %T, want none", r)
	}
	if got := e2eHooks(nil); got != (wails.Hooks{}) {
		t.Fatalf("normal mode hooks %+v, want none", got)
	}

	n, err := New(normal, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	if _, ok := n.coreEnv().calls.(*weblink.Opener); !ok || n.e2e != nil {
		t.Fatalf("normal mode call opener %T, recorder %T; want weblink and none", n.coreEnv().calls, n.e2e)
	}
}
