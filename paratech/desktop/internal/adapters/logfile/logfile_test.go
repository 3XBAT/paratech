package logfile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRotation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	f, err := Open(dir, Options{MaxBytes: 100, Keep: 2})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 39) + "\n" // 40 bytes: two lines per file
	for i := range 7 {
		if _, err := fmt.Fprintf(f, "%d%s", i, line[1:]); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	// 7 lines: 0-1, 2-3, 4-5 rotated, 6 current; the oldest pair was dropped.
	want := map[string]string{"desktop.log": "6", "desktop.1.log": "45", "desktop.2.log": "23"}
	for name, firsts := range want {
		got := read(t, filepath.Join(dir, name))
		var heads strings.Builder
		for l := range strings.SplitSeq(strings.TrimSpace(got), "\n") {
			heads.WriteByte(l[0])
		}
		if heads.String() != firsts {
			t.Errorf("%s holds lines %q, want %q", name, heads.String(), firsts)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "desktop.3.log")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a third rotated file: %v", err)
	}

	if _, err := f.Write([]byte("late\n")); !errors.Is(err, ErrClosed) {
		t.Fatalf("Write after Close = %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("a second Close = %v", err)
	}
}

func TestReopenAppends(t *testing.T) {
	dir := t.TempDir()
	for _, text := range []string{"first\n", "second\n"} {
		f, err := Open(dir, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(text)); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if got := read(t, filepath.Join(dir, Name())); got != "first\nsecond\n" {
		t.Fatalf("log = %q", got)
	}
}

func TestConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	f, err := Open(dir, Options{MaxBytes: 4096, Keep: 2})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 200 {
				_, _ = fmt.Fprintf(f, "goroutine %d line %03d\n", g, i)
			}
		})
	}
	wg.Wait()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range Names(2) {
		for l := range strings.SplitSeq(strings.TrimSpace(read(t, filepath.Join(dir, name))), "\n") {
			if !strings.HasPrefix(l, "goroutine ") || len(l) != len("goroutine 0 line 000") {
				t.Fatalf("%s has a torn line %q", name, l)
			}
		}
	}
}

func TestOpenErrorsHaveNoPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secret-student")
	if err := os.WriteFile(dir, []byte("a file where the directory should be"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(filepath.Join(dir, "logs"), Options{})
	if !errors.Is(err, ErrLogFile) || strings.Contains(err.Error(), "secret-student") {
		t.Fatalf("Open = %v, want ErrLogFile without the path", err)
	}
	if _, err := Open("relative/logs", Options{}); !errors.Is(err, ErrLogFile) {
		t.Fatalf("a relative directory: %v", err)
	}
}

func TestEachLogNewestFirstWithinTheBound(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"desktop.log": "current-1\ncurrent-2\n", "desktop.1.log": "older\n", "desktop.2.log": "oldest-a\noldest-b\n",
		"desktop.3.log": "not ours\n", "other.txt": "not ours\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	type got struct{ name, data string }
	collect := func(limit int64) []got {
		var out []got
		err := Dir{Path: dir}.EachLog(context.Background(), limit, func(name string, data []byte) error {
			out = append(out, got{name, string(data)})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	all := collect(1 << 20)
	want := []got{{"desktop.log", "current-1\ncurrent-2\n"}, {"desktop.1.log", "older\n"}, {"desktop.2.log", "oldest-a\noldest-b\n"}}
	if fmt.Sprint(all) != fmt.Sprint(want) {
		t.Fatalf("EachLog = %v, want %v", all, want)
	}
	// 20 + 6 + the last 9 bytes of the oldest file.
	bounded := collect(35)
	if len(bounded) != 3 || bounded[2].data != "oldest-b\n" {
		t.Fatalf("bounded EachLog = %v", bounded)
	}
	if b := collect(10); len(b) != 1 || b[0].data != "current-2\n" {
		t.Fatalf("EachLog of 10 bytes = %v, want the tail of the current file", b)
	}

	if err := (Dir{Path: filepath.Join(dir, "none")}).EachLog(context.Background(), 100, func(string, []byte) error {
		t.Fatal("a missing directory has no logs")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	stop := errors.New("stop")
	if err := (Dir{Path: dir}).EachLog(context.Background(), 100, func(string, []byte) error { return stop }); !errors.Is(err, stop) {
		t.Fatalf("EachLog = %v, want the callback's error", err)
	}
}

func TestTee(t *testing.T) {
	var console bytes.Buffer
	tee := NewTee(slog.NewTextHandler(&console, &slog.HandlerOptions{Level: slog.LevelInfo}))
	root := slog.New(tee)
	early := root.With("recording", "rec1").WithGroup("capture") // built before the file is attached

	root.Info("before the file")
	var file syncBuffer
	tee.Attach(&file)
	early.Info("capture started", "mode", "include")
	root.Debug("below the level of the console")
	floored := slog.New(floor{min: slog.LevelWarn, next: root.Handler()})
	floored.Info("below the floor")
	floored.Warn("above the floor")
	tee.Detach()
	root.Info("after the detach")

	got := file.String()
	for _, want := range []string{`msg="capture started" recording=rec1 capture.mode=include`, `msg="above the floor"`} {
		if !strings.Contains(got, want) {
			t.Errorf("the file lacks %q:\n%s", want, got)
		}
	}
	for _, not := range []string{"before the file", "below the level", "below the floor", "after the detach"} {
		if strings.Contains(got, not) {
			t.Errorf("the file has %q:\n%s", not, got)
		}
	}
	for _, want := range []string{"before the file", "capture started", "above the floor", "after the detach"} {
		if !strings.Contains(console.String(), want) {
			t.Errorf("the console lacks %q", want)
		}
	}
}

// A failing file never fails a log call.
func TestTeeIgnoresAFailingFile(t *testing.T) {
	var console bytes.Buffer
	tee := NewTee(slog.NewTextHandler(&console, nil))
	tee.Attach(failingWriter{})
	if err := tee.Handle(context.Background(), slog.NewRecord(timeZero, slog.LevelInfo, "hello", 0)); err != nil {
		t.Fatalf("Handle = %v", err)
	}
	if !strings.Contains(console.String(), "hello") {
		t.Fatal("the console lost the record")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// floor drops records below min, like the Wails floor of input/wails.
type floor struct {
	min  slog.Level
	next slog.Handler
}

func (h floor) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.min && h.next.Enabled(ctx, l)
}
func (h floor) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < h.min {
		return nil
	}
	return h.next.Handle(ctx, r)
}
func (h floor) WithAttrs(a []slog.Attr) slog.Handler { return floor{h.min, h.next.WithAttrs(a)} }
func (h floor) WithGroup(n string) slog.Handler      { return floor{h.min, h.next.WithGroup(n)} }

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

var timeZero = time.Time{}
