package app

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"paratech/desktop/config"
)

func newTestApp(t *testing.T, dataDir string) *App {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = dataDir
	a, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() }) // before TempDir removal: Windows cannot delete open files
	return a
}

// The settings page path end to end without a window: the core the shell binds to stores toggles in SQLite, and
// they survive a restart.
func TestOpenCoreStoresSettings(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data") // created by openCore

	a := newTestApp(t, dir)
	core, err := a.openCore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.openCore(ctx); err == nil {
		t.Error("a second openCore must fail")
	}
	if _, err := core.Settings.SetProcessOnlyOnACPower(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	b := newTestApp(t, dir)
	core, err = b.openCore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := core.Settings.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProcessOnlyOnACPower || !got.RecordingPromptsEnabled {
		t.Fatalf("after restart: %+v, want AC restriction off and prompts on (default)", got)
	}
}

func TestOpenCoreNeedsDataDir(t *testing.T) {
	if _, err := newTestApp(t, "").openCore(context.Background()); err == nil {
		t.Fatal("openCore without a data directory must fail")
	}
}
