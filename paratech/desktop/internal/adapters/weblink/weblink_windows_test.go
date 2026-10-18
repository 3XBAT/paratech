//go:build windows

package weblink

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSweepShortcutsRemovesOnlyOldOnes(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	write := func(name string, age time.Duration) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
		return p
	}
	old := write(shortcutPrefix+"1.url", 2*shortcutTTL)
	fresh := write(shortcutPrefix+"2.url", 0)
	other := write("someone-else.url", 2*shortcutTTL)

	sweepShortcuts(dir, now)

	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("old shortcut kept: %v", err)
	}
	for _, p := range []string{fresh, other} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s removed: %v", filepath.Base(p), err)
		}
	}
}

func TestStartShortcutCancelledWritesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before, _ := filepath.Glob(filepath.Join(os.TempDir(), shortcutPrefix+"*.url"))

	if err := startShortcut(ctx, "https://example.com/a?x=1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("startShortcut = %v, want context.Canceled", err)
	}
	after, _ := filepath.Glob(filepath.Join(os.TempDir(), shortcutPrefix+"*.url"))
	if len(after) > len(before) {
		t.Fatalf("a shortcut was written: %v", after)
	}
}
