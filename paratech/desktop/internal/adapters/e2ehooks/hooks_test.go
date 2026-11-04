package e2ehooks

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

func TestRecorder(t *testing.T) {
	ctx := context.Background()
	r := newRun(t)

	u, err := values.ParseSourceURL("https://huggingface.example.org/models/ggml")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := shared.NewArtifactRef(shared.ArtifactExports, "library/m1/lecture.docx")
	if err != nil {
		t.Fatal(err)
	}
	call, err := subject.ParseCallLink("https://meet.example.org/abc-defg-hij")
	if err != nil {
		t.Fatal(err)
	}
	steps := []func() error{
		func() error { return r.h.OpenLink(ctx, u) },
		func() error { return r.h.OpenArtifact(ctx, ref) },
		func() error { return r.h.OpenSubjectCallLink(ctx, call) },
		func() error { return r.h.SetText(ctx, "Текст\nс \"кавычками\"") },
	}
	for i, step := range steps {
		if err := step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}

	want := []openedLine{
		{KindLink, "https://huggingface.example.org/models/ggml", "2026-11-04T10:30:00Z"},
		{KindFile, ref.String(), "2026-11-04T10:30:00Z"},
		{KindCall, "https://meet.example.org/abc-defg-hij", "2026-11-04T10:30:00Z"},
		{KindClipboard, "Текст\nс \"кавычками\"", "2026-11-04T10:30:00Z"},
	}
	got := r.opened(t)
	if len(got) != len(want) {
		t.Fatalf("opened %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// Concurrent writes give whole lines, one per call.
func TestRecorderConcurrent(t *testing.T) {
	r := newRun(t)
	const n = 40
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.h.SetText(context.Background(), "text "+strconv.Itoa(i)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	for _, l := range r.opened(t) {
		seen[l.Value] = true
	}
	if len(seen) != n {
		t.Fatalf("%d distinct lines, want %d", len(seen), n)
	}
}

// opened.jsonl that is not a plain file fails the call; nothing is written through it.
func TestRecorderRefusesALinkedRecord(t *testing.T) {
	r := newRun(t)
	writeFile(t, filepath.Join(r.dir, openedFile, "x"), "")
	if err := r.h.SetText(context.Background(), "x"); err == nil {
		t.Fatal("no error for a folder at opened.jsonl")
	}

	r = newRun(t)
	if err := os.Symlink(filepath.Join(r.outside, "secret.wav"), filepath.Join(r.dir, openedFile)); err != nil {
		t.Skipf("no symlink here: %v", err)
	}
	if err := r.h.SetText(context.Background(), "x"); err == nil {
		t.Fatal("no error for a link at opened.jsonl")
	}
	b, err := os.ReadFile(filepath.Join(r.outside, "secret.wav"))
	if err != nil || string(b) != "data of outside/secret.wav" {
		t.Fatalf("the link target changed: %q, %v", b, err)
	}
}

// The hooks can start nothing: the package imports no process or system call API, and only domain of this module.
func TestImportsNoProcessStart(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{"os/exec", "syscall", "unsafe", "golang.org/x/sys", "plugin", "net"}
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, imp := range parsed.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			for _, bad := range forbidden {
				if path == bad || strings.HasPrefix(path, bad+"/") {
					t.Errorf("%s imports %s", f, path)
				}
			}
			if strings.HasPrefix(path, "paratech/") && !strings.HasPrefix(path, "paratech/desktop/internal/domain/") {
				t.Errorf("%s imports %s: an adapter imports only domain of this module (ARCH-30)", f, path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no source files checked")
	}
}
