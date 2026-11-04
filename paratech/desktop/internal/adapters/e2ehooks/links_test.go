package e2ehooks

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// linker makes a link at link to target (a symlink here, a junction on Windows); it skips when the OS refuses.
type linker func(t *testing.T, target, link string)

func symlinker(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlink here: %v", err)
	}
}

// linkCases are the link attacks of a pick, for one kind of link: a link to a folder outside, a link to a folder
// inside, and the fixtures folder itself as a link. Each must cancel.
func linkCases(t *testing.T, link linker) {
	t.Helper()
	tests := []struct {
		name  string
		setup func(t *testing.T, r run)
		path  string
	}{
		{"folder link out", func(t *testing.T, r run) {
			link(t, r.outside, filepath.Join(r.dir, fixturesDir, "out"))
		}, "fixtures/out/secret.wav"},
		{"folder link inside", func(t *testing.T, r run) {
			link(t, filepath.Join(r.dir, fixturesDir, "sub"), filepath.Join(r.dir, fixturesDir, "alias"))
		}, "fixtures/alias/notes.pdf"},
		{"nested folder link out", func(t *testing.T, r run) {
			link(t, r.outside, filepath.Join(r.dir, fixturesDir, "sub", "deep"))
		}, "fixtures/sub/deep/secret.wav"},
		{"fixtures is a link", func(t *testing.T, r run) {
			fixtures := filepath.Join(r.dir, fixturesDir)
			if err := os.Rename(fixtures, filepath.Join(r.dir, "real")); err != nil {
				t.Fatal(err)
			}
			link(t, filepath.Join(r.dir, "real"), fixtures)
		}, "fixtures/clip.wav"},
		{"fixtures is a link out", func(t *testing.T, r run) {
			fixtures := filepath.Join(r.dir, fixturesDir)
			if err := os.RemoveAll(fixtures); err != nil {
				t.Fatal(err)
			}
			link(t, r.outside, fixtures)
		}, "fixtures/secret.wav"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRun(t)
			tt.setup(t, r)
			r.queue(t, pickJSON(t, DialogRecording, tt.path))
			if got, _ := r.h.PickFile(context.Background(), DialogRecording); got != "" {
				t.Fatalf("picked %q through a link", got)
			}
			if c := r.consumed(t); len(c) != 1 || c[0].Result != resultCancelled {
				t.Fatalf("consumed %+v, want cancelled", c)
			}
		})
	}
}

func TestPickRefusesSymlinks(t *testing.T) {
	linkCases(t, symlinker)

	// A file link, out or in.
	for _, target := range []string{"outside/secret.wav", "fixtures/clip.wav"} {
		r := newRun(t)
		symlinker(t, filepath.Join(r.dir, filepath.FromSlash(target)), filepath.Join(r.dir, fixturesDir, "link.wav"))
		r.queue(t, pickJSON(t, DialogRecording, "fixtures/link.wav"))
		if got, _ := r.h.PickFile(context.Background(), DialogRecording); got != "" {
			t.Fatalf("picked %q through a file link to %s", got, target)
		}
	}
}

// saved as a link, or a link at the target name, cancels the save dialog.
func TestSaveRefusesSymlinks(t *testing.T) {
	saveLinkCases(t, symlinker)

	r := newRun(t)
	if err := os.Mkdir(filepath.Join(r.dir, savedDir), 0o700); err != nil {
		t.Fatal(err)
	}
	symlinker(t, filepath.Join(r.outside, "secret.wav"), filepath.Join(r.dir, savedDir, "t.txt"))
	if got, _ := r.h.SaveFile(context.Background(), "transcript", "t.txt"); got != "" {
		t.Fatalf("save through a file link: %q", got)
	}
}

func saveLinkCases(t *testing.T, link linker) {
	t.Helper()
	r := newRun(t)
	link(t, r.outside, filepath.Join(r.dir, savedDir))
	if got, _ := r.h.SaveFile(context.Background(), "transcript", "t.txt"); got != "" {
		t.Fatalf("saved is a link: %q", got)
	}
	if _, err := os.Stat(filepath.Join(r.outside, "t.txt")); err == nil {
		t.Fatal("a file was created through the link")
	}
}
