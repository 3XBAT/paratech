//go:build windows

package weblink

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	schedvalues "paratech/desktop/internal/domain/schedule/values"
)

// fakeLauncher replaces explorer.exe for the test and an isolated %TMP% holds the .url files: no browser starts.
type fakeLauncher struct {
	calls    int
	path     string
	contents string
}

func useFakeLauncher(t *testing.T) (*fakeLauncher, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMP", dir)
	t.Setenv("TEMP", dir)
	if got := os.TempDir(); !strings.EqualFold(filepath.Clean(got), filepath.Clean(dir)) {
		t.Fatalf("os.TempDir() = %q, not the test directory %q", got, dir)
	}

	f := &fakeLauncher{}
	prev := startShortcutFile
	startShortcutFile = func(_ context.Context, path string) error {
		f.calls++
		f.path = path
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f.contents = string(b)
		return nil
	}
	t.Cleanup(func() { startShortcutFile = prev })
	return f, dir
}

func TestOpenCallLinkWritesTheShortcut(t *testing.T) {
	for _, link := range []string{zoomCallLink, teamsCallLink, "http://bbb.example.edu/b/room-42"} {
		t.Run(link, func(t *testing.T) {
			f, dir := useFakeLauncher(t)

			if err := New().OpenCallLink(context.Background(), mustCallLink(t, link)); err != nil {
				t.Fatalf("OpenCallLink = %v", err)
			}
			if f.calls != 1 {
				t.Fatalf("launcher calls = %d, want 1", f.calls)
			}
			if !strings.EqualFold(filepath.Dir(f.path), filepath.Clean(dir)) ||
				!strings.HasPrefix(filepath.Base(f.path), shortcutPrefix) || filepath.Ext(f.path) != ".url" {
				t.Fatalf("launcher got %q, want our .url file in %q", f.path, dir)
			}
			// The launcher gets only the file path, never the link.
			if strings.Contains(f.path, "://") {
				t.Fatalf("the link reached the launcher argument: %q", f.path)
			}
			want := "[InternetShortcut]\r\nURL=" + link + "\r\n"
			if f.contents != want {
				t.Fatalf(".url contents = %q, want %q", f.contents, want)
			}
		})
	}
}

func TestOpenCallLinkCancelledStartsNothing(t *testing.T) {
	f, dir := useFakeLauncher(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := New().OpenCallLink(ctx, mustCallLink(t, zoomCallLink)); !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenCallLink = %v, want context.Canceled", err)
	}
	if f.calls != 0 {
		t.Fatalf("launcher calls = %d, want 0", f.calls)
	}
	if files, _ := filepath.Glob(filepath.Join(dir, shortcutPrefix+"*.url")); len(files) != 0 {
		t.Fatalf("a shortcut was written: %v", files)
	}
}

func TestOpenCallLinkRefusedStartsNothing(t *testing.T) {
	f, dir := useFakeLauncher(t)

	if err := New().OpenCallLink(context.Background(), schedvalues.CallLink{}); !errors.Is(err, ErrRefused) {
		t.Fatalf("OpenCallLink(zero) = %v, want ErrRefused", err)
	}
	if f.calls != 0 {
		t.Fatalf("launcher calls = %d, want 0", f.calls)
	}
	if files, _ := filepath.Glob(filepath.Join(dir, shortcutPrefix+"*.url")); len(files) != 0 {
		t.Fatalf("a shortcut was written: %v", files)
	}
}
