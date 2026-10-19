package whispercli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
	"paratech/desktop/testenv"
)

// sttEngine mirrors the STTEngine port of cases/transcribe (adapters may not import cases, ARCH-30).
type sttEngine interface {
	Transcribe(ctx context.Context, audio, model, output shared.ArtifactRef, load shared.RecognitionLoad,
		progress func(percent int), segment func(transcript.Segment)) error
}

var _ sttEngine = (*Engine)(nil)

const (
	workName  = "stt-work"
	goraceEnv = "GORACE=atexit_sleep_ms=0"
)

// fakePaths maps a reference to <root>/<kind>/<path>, or to what override returns.
type fakePaths struct {
	root     string
	override func(shared.ArtifactRef) (string, error)
}

func (p *fakePaths) Path(_ context.Context, ref shared.ArtifactRef) (string, error) {
	if p.override != nil {
		return p.override(ref)
	}
	return filepath.Join(p.root, string(ref.Kind()), filepath.FromSlash(ref.Path())), nil
}

type fixture struct {
	root  string
	work  string
	paths *fakePaths
	store *testenv.MemArtifacts
	eng   *Engine
}

func newFixture(t *testing.T, root string) *fixture {
	t.Helper()
	if root == "" {
		root = t.TempDir()
	}

	f := &fixture{
		root:  root,
		work:  filepath.Join(root, workName),
		paths: &fakePaths{root: root},
		store: testenv.NewMemArtifacts(),
	}

	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	eng, err := New(Config{Binary: bin, DataRoot: root, WorkDir: f.work, Threads: 2, Language: "ru"}, f.paths, f.store)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// GORACE: the race runtime of the fake otherwise sleeps a second at exit.
	eng.env = []string{fakeEnvKey + "=" + fakeOn, goraceEnv}
	eng.progressEvery = 0
	f.eng = eng

	return f
}

// file creates <root>/<kind>/<path> with content and returns its reference.
func (f *fixture) file(t *testing.T, kind shared.ArtifactKind, path, content string) shared.ArtifactRef {
	t.Helper()
	ref, err := shared.NewArtifactRef(kind, path)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(f.root, string(kind), filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return ref
}

func (f *fixture) model(t *testing.T, mode string) shared.ArtifactRef {
	t.Helper()
	return f.file(t, shared.ArtifactModels, "ggml-"+mode+".bin", mode)
}

func (f *fixture) audio(t *testing.T) shared.ArtifactRef {
	t.Helper()
	return f.file(t, shared.ArtifactRecordings, "rec-1/chunk-0001.wav", "RIFF")
}

func outRef(t *testing.T, name string) shared.ArtifactRef {
	t.Helper()
	ref, err := shared.NewArtifactRef(shared.ArtifactTranscripts, name)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

// stored returns the transcript document at ref.
func (f *fixture) stored(t *testing.T, ref shared.ArtifactRef) Transcript {
	t.Helper()
	rc, err := f.store.Open(context.Background(), ref)
	if err != nil {
		t.Fatalf("open %s: %v", ref, err)
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}

	var doc Transcript
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("decode stored transcript: %v", err)
	}
	return doc
}

// assertClean checks that no run directory and no stored artifact is left.
func (f *fixture) assertClean(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(f.work)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("work directory not empty: %d entries", len(entries))
	}
	if p := f.store.Paths(); len(p) != 0 {
		t.Errorf("artifacts written: %v", p)
	}
}

// assertStopped checks that the heartbeat file does not grow any more; a missing one (the process was killed
// before it got there) must stay missing.
func assertStopped(t *testing.T, path string) {
	t.Helper()
	size := func() int64 {
		fi, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			return -1
		}
		if err != nil {
			t.Fatal(err)
		}
		return fi.Size()
	}

	before := size()
	time.Sleep(20 * beatEvery)
	if after := size(); after != before {
		t.Errorf("%s still growing (%d -> %d): process not killed", filepath.Base(path), before, after)
	}
}

func TestTranscribeSuccess(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	f := newFixture(t, "")
	out := outRef(t, "rec-1/transcript.json")

	var got []int
	err := f.eng.Transcribe(context.Background(), f.audio(t), f.model(t, modeOK), out, shared.LoadMax, func(p int) { got = append(got, p) }, nil)
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	// The pump may skip values a slow callback missed, never the final 100.
	if len(got) == 0 || got[len(got)-1] != 100 {
		t.Fatalf("progress = %v, want increasing values up to 100", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Fatalf("progress not increasing: %v", got)
		}
	}

	want := Transcript{Version: TranscriptFormatVersion, Language: "ru", Segments: []Segment{
		{StartMs: 0, EndMs: 2500, Text: `Hello, "world" \ ok.`},
		{StartMs: 2500, EndMs: 5000, Text: "second line with a raw newline"},
	}}
	doc := f.stored(t, out)
	if doc.Version != want.Version || doc.Language != want.Language || len(doc.Segments) != len(want.Segments) {
		t.Fatalf("stored = %+v, want %+v", doc, want)
	}
	for i := range want.Segments {
		if doc.Segments[i] != want.Segments[i] {
			t.Errorf("segment %d = %+v, want %+v", i, doc.Segments[i], want.Segments[i])
		}
	}

	entries, _ := os.ReadDir(f.work)
	if len(entries) != 0 {
		t.Errorf("run directory left behind")
	}
}

func TestTranscribeNilProgressAndEmptyResult(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	f := newFixture(t, "")
	out := outRef(t, "empty.json")

	if err := f.eng.Transcribe(context.Background(), f.audio(t), f.model(t, modeEmpty), out, shared.LoadMax, nil, nil); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if doc := f.stored(t, out); len(doc.Segments) != 0 || doc.Segments == nil {
		t.Errorf("segments = %#v, want an empty array", doc.Segments)
	}
}

// TestTranscribeArgv checks that unusual but valid paths reach the process as exactly one argument each.
func TestTranscribeArgv(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	root := filepath.Join(t.TempDir(), "data root 'q' $x;&(y) %PATH% !z^ `w`")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, root)
	model := f.model(t, modeEcho)

	audios := []string{
		"plain.wav",
		"with space/a b.wav",
		"-f/-m.wav",
		"@resp/@x.wav",
		"semi;colon/$(echo hi)&x%PATH%.wav",
		"кириллица/лекция.wav",
		"ünïcödé ✓.wav",
		"fullwidth＂quote＼.wav",
	}

	for i, name := range audios {
		t.Run(name, func(t *testing.T) {
			audio := f.file(t, shared.ArtifactRecordings, name, "RIFF")
			audioPath := filepath.Join(root, "recordings", filepath.FromSlash(name))
			out := outRef(t, "argv-"+string(rune('a'+i))+".json")

			err := f.eng.Transcribe(context.Background(), audio, model, out, shared.LoadMax, nil, nil)
			if checkArgEncoding(audioPath) != nil {
				// Windows: not representable in the ANSI code page of whisper-cli.
				if !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("err = %v, want ErrInvalidInput", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Transcribe: %v", err)
			}

			segs := f.stored(t, out).Segments
			if len(segs) != 7 {
				t.Fatalf("fake saw %d values, want 7: %+v", len(segs), segs)
			}

			modelPath := filepath.Join(root, "models", "ggml-echo.bin")
			cwd := segs[5].Text
			checks := []struct{ what, got, want string }{
				{"model", segs[0].Text, modelPath},
				{"audio", segs[1].Text, audioPath},
				{"output dir", filepath.Dir(segs[2].Text), cwd},
				{"output name", filepath.Base(segs[2].Text), outName},
				{"threads", segs[3].Text, "2"},
				{"language", segs[4].Text, "ru"},
				{"run dir parent", filepath.Dir(cwd), f.work},
				{"flags", segs[6].Text, "-oj -pp -ng"},
			}
			for _, c := range checks {
				if c.got != c.want {
					t.Errorf("%s = %q, want %q", c.what, c.got, c.want)
				}
			}
		})
	}
}

func TestTranscribeEnvironmentIsMinimal(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	t.Setenv("WHISPERCLI_TEST_CANARY", "secret")
	t.Setenv("GGML_BACKEND_PATH", "/tmp/evil")
	f := newFixture(t, "")
	out := outRef(t, "env.json")

	if err := f.eng.Transcribe(context.Background(), f.audio(t), f.model(t, modeEnv), out, shared.LoadMax, nil, nil); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	for _, s := range f.stored(t, out).Segments {
		key, _, _ := strings.Cut(s.Text, "=")
		switch {
		case key == fakeEnvKey, s.Text == goraceEnv:
		case runtime.GOOS == "windows" && strings.EqualFold(key, "SYSTEMROOT"):
		default:
			t.Errorf("unexpected variable %q in the child environment", key)
		}
	}
}

func TestTranscribeRejectsUnsafePaths(t *testing.T) {
	root := t.TempDir()
	recordings := filepath.Join(root, "recordings")
	if err := os.MkdirAll(filepath.Join(recordings, "dir.wav"), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.wav")
	if err := os.WriteFile(outside, []byte("RIFF"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		want error
	}{
		{"relative flag", "-m", ErrInvalidInput},
		{"relative response file", "@args.txt", ErrInvalidInput},
		{"relative", filepath.Join("recordings", "a.wav"), ErrInvalidInput},
		{"outside root", outside, ErrInvalidInput},
		{"the root itself", root, ErrInvalidInput},
		{"not clean", recordings + string(filepath.Separator) + ".." + string(filepath.Separator) + "x.wav", ErrInvalidInput},
		{"newline", filepath.Join(recordings, "a\nb.wav"), ErrInvalidInput},
		{"NUL", filepath.Join(recordings, "a\x00b.wav"), ErrInvalidInput},
		{"directory", filepath.Join(recordings, "dir.wav"), ErrInvalidInput},
		{"missing", filepath.Join(recordings, "missing.wav"), shared.ErrNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			f := newFixture(t, root)
			model := f.model(t, modeOK)
			f.paths.override = func(ref shared.ArtifactRef) (string, error) {
				if ref.Kind() == shared.ArtifactModels {
					return filepath.Join(root, "models", filepath.FromSlash(ref.Path())), nil
				}
				return tt.path, nil
			}
			audio, _ := shared.NewArtifactRef(shared.ArtifactRecordings, "x.wav")

			err := f.eng.Transcribe(context.Background(), audio, model, outRef(t, "x.json"), shared.LoadMax, nil, nil)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if strings.Contains(err.Error(), root) {
				t.Errorf("error leaks the data root: %v", err)
			}
			if _, err := os.Stat(f.work); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("work directory created: the process may have run")
			}
			f.assertClean(t)
		})
	}
}

func TestTranscribeRejectsLinks(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	f := newFixture(t, "")
	target := f.file(t, shared.ArtifactRecordings, "real.wav", "RIFF")
	link := filepath.Join(f.root, "recordings", "link.wav")
	if err := os.Symlink(filepath.Join(f.root, "recordings", target.Path()), link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	ref, _ := shared.NewArtifactRef(shared.ArtifactRecordings, "link.wav")

	err := f.eng.Transcribe(context.Background(), ref, f.model(t, modeOK), outRef(t, "x.json"), shared.LoadMax, nil, nil)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
	f.assertClean(t)
}

func TestTranscribeRefusesUnusableWorkDir(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, work string)
	}{
		{"link to a directory outside the root", func(t *testing.T, work string) { makeDirLink(t, work, t.TempDir()) }},
		{"file", func(t *testing.T, work string) {
			if err := os.WriteFile(work, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			f := newFixture(t, "")
			tt.setup(t, f.work)

			err := f.eng.Transcribe(context.Background(), f.audio(t), f.model(t, modeOK), outRef(t, "x.json"), shared.LoadMax, nil, nil)
			if !errors.Is(err, ErrWorkDir) || !errors.Is(err, shared.ErrStorage) {
				t.Fatalf("err = %v, want ErrWorkDir", err)
			}
			if p := f.store.Paths(); len(p) != 0 {
				t.Errorf("artifacts written: %v", p)
			}
		})
	}
}

func TestTranscribeWrongKinds(t *testing.T) {
	f := newFixture(t, "")
	audio, model, out := f.audio(t), f.model(t, modeOK), outRef(t, "x.json")

	tests := []struct {
		name               string
		audio, model, outp shared.ArtifactRef
	}{
		{"audio is a model", model, model, out},
		{"model is audio", audio, audio, out},
		{"output is a recording", audio, model, audio},
		{"zero output", audio, model, shared.ArtifactRef{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := f.eng.Transcribe(context.Background(), tt.audio, tt.model, tt.outp, shared.LoadMax, nil, nil)
			if !errors.Is(err, ErrInvalidInput) || !errors.Is(err, shared.ErrInvalidArtifactRef) {
				t.Fatalf("err = %v, want ErrInvalidInput and ErrInvalidArtifactRef", err)
			}
			f.assertClean(t)
		})
	}
}

func TestTranscribeExitCodeCarriesStderr(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	f := newFixture(t, "")
	var progress []int

	err := f.eng.Transcribe(context.Background(), f.audio(t), f.model(t, modeExit3), outRef(t, "x.json"), shared.LoadMax, func(p int) { progress = append(progress, p) }, nil)

	var re *RunError
	if !errors.As(err, &re) || !errors.Is(err, ErrExit) {
		t.Fatalf("err = %v, want *RunError with ErrExit", err)
	}
	if re.Code != 3 {
		t.Errorf("code = %d, want 3", re.Code)
	}
	if !strings.Contains(re.Stderr, "failed to initialize whisper context") {
		t.Errorf("stderr tail = %q, want the CLI error", re.Stderr)
	}
	if !strings.Contains(re.Stderr, placeholderData) {
		t.Errorf("stderr tail = %q, want the data root replaced by %s", re.Stderr, placeholderData)
	}
	if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(f.root)) {
		t.Errorf("error leaks the data root: %v", err)
	}
	if !strings.Contains(err.Error(), exitMeanings[3]) {
		t.Errorf("error %q lacks the meaning of the exit code", err)
	}
	if len(progress) != 0 {
		t.Errorf("progress reported for a failed run: %v", progress)
	}
	f.assertClean(t)
}

func TestTranscribeExitZeroWithoutResult(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	f := newFixture(t, "")

	err := f.eng.Transcribe(context.Background(), f.audio(t), f.model(t, modeNoOut), outRef(t, "x.json"), shared.LoadMax, nil, nil)

	var re *RunError
	if !errors.As(err, &re) || !errors.Is(err, ErrNoOutput) {
		t.Fatalf("err = %v, want *RunError with ErrNoOutput", err)
	}
	if !strings.Contains(re.Stderr, "unknown language") {
		t.Errorf("stderr tail = %q", re.Stderr)
	}
	f.assertClean(t)
}

func TestTranscribeStderrIsBounded(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	f := newFixture(t, "")

	err := f.eng.Transcribe(context.Background(), f.audio(t), f.model(t, modeSpam), outRef(t, "x.json"), shared.LoadMax, nil, nil)

	var re *RunError
	if !errors.As(err, &re) || re.Code != 1 {
		t.Fatalf("err = %v, want *RunError with code 1", err)
	}
	if len(re.Stderr) > stderrMax {
		t.Errorf("stderr tail has %d bytes, limit %d", len(re.Stderr), stderrMax)
	}
	if !strings.Contains(re.Stderr, "input file(s) failed near "+placeholderData) {
		t.Errorf("stderr tail = %q, want the last line redacted", re.Stderr)
	}
	if strings.Contains(strings.ToLower(re.Stderr), strings.ToLower(f.root)) {
		t.Errorf("stderr tail leaks the data root")
	}
	f.assertClean(t)
}

func TestTranscribeCancelKillsProcess(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	f := newFixture(t, "")
	model := f.model(t, modeHang)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := f.eng.Transcribe(ctx, f.audio(t), model, outRef(t, "x.json"), shared.LoadMax, func(int) { cancel() }, nil)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}

	// The fake starts its heartbeat before the progress line that cancelled.
	beat := filepath.Join(f.root, "models", model.Path()+beatSuffix)
	if _, err := os.Stat(beat); err != nil {
		t.Fatalf("heartbeat missing: %v", err)
	}
	assertStopped(t, beat)
	f.assertClean(t)
}

func TestTranscribeTimeout(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	f := newFixture(t, "")
	// Long enough for the fake to start under load; if it does not, the kill is still checked.
	f.eng.cfg.Timeout = 2 * time.Second
	model := f.model(t, modeHang)

	err := f.eng.Transcribe(context.Background(), f.audio(t), model, outRef(t, "x.json"), shared.LoadMax, nil, nil)

	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	assertStopped(t, filepath.Join(f.root, "models", model.Path()+beatSuffix))
	f.assertClean(t)
}

// TestTranscribeBlockingProgress checks that a callback that blocks does not stall the process: it writes far more
// stderr than a pipe holds, finishes and the result is stored while the callback still waits.
func TestTranscribeBlockingProgress(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	f := newFixture(t, "")
	out := outRef(t, "flood.json")
	audio, model := f.audio(t), f.model(t, modeFlood)

	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var got []int
	progress := func(p int) {
		once.Do(func() {
			close(entered)
			<-release
		})
		mu.Lock()
		got = append(got, p)
		mu.Unlock()
	}

	done := make(chan error, 1)
	go func() {
		done <- f.eng.Transcribe(context.Background(), audio, model, out, shared.LoadMax, progress, nil)
	}()

	<-entered
	testenv.Eventually(t, "transcript stored while the callback blocks", func() bool {
		return len(f.store.Paths()) == 1
	}, nil)

	select {
	case err := <-done:
		t.Fatalf("Transcribe returned while its callback was running: %v", err)
	default:
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 || got[len(got)-1] != 100 {
		t.Errorf("progress = %v, want the last value 100", got)
	}
}

// TestTranscribeStderrRedactsNonASCIIRoot: the fake prints paths as whisper-cli does (ANSI code page bytes on
// Windows), so a profile name outside ASCII must still be redacted.
func TestTranscribeStderrRedactsNonASCIIRoot(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	root := filepath.Join(t.TempDir(), "Иван Петров")
	if err := checkArgEncoding(root); err != nil {
		t.Skipf("root not representable in this system's ANSI code page: %v", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, root)

	err := f.eng.Transcribe(context.Background(), f.audio(t), f.model(t, modeExit3), outRef(t, "x.json"), shared.LoadMax, nil, nil)

	var re *RunError
	if !errors.As(err, &re) || re.Code != 3 {
		t.Fatalf("err = %v, want *RunError with code 3", err)
	}
	if !strings.Contains(re.Stderr, placeholderData) || strings.Contains(re.Stderr, "?") {
		t.Errorf("stderr tail = %q, want %s and no undecoded bytes", re.Stderr, placeholderData)
	}

	lower := strings.ToLower(err.Error())
	for _, leak := range []string{"иван", "петров", native("Иван"), native("ИВАН")} {
		if strings.Contains(lower, strings.ToLower(leak)) {
			t.Errorf("error leaks %q: %q", leak, err)
		}
	}
	f.assertClean(t)
}

func TestKillRunRunningProcess(t *testing.T) {
	dir := t.TempDir()
	model := filepath.Join(dir, "ggml-hang.bin")
	if err := os.WriteFile(model, []byte(modeHang), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := fakeCommand(t, model)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	grp, _ := newProcGroup(0)
	defer grp.close()
	_ = grp.attach(cmd.Process.Pid)

	if err := killRun(cmd.Process, grp); err != nil {
		t.Errorf("killRun = %v, want nil for a running process", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Error("the killed process reported success")
	}
}

// fakeCommand returns the fake whisper-cli with the given model; a missing model makes it exit with fakeExitNoModel.
func fakeCommand(t *testing.T, model string) *exec.Cmd {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, flagModel, model) //nolint:gosec // test binary
	cmd.Env = []string{fakeEnvKey + "=" + fakeOn, goraceEnv}
	return cmd
}

func TestTranscribeCancelledBeforeStart(t *testing.T) {
	f := newFixture(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := f.eng.Transcribe(ctx, f.audio(t), f.model(t, modeOK), outRef(t, "x.json"), shared.LoadMax, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	f.assertClean(t)
}

func TestTranscribeInvalidResults(t *testing.T) {
	tests := []struct {
		mode string
		want error
	}{
		{modeGarbage, ErrOutput},
		{modeBadTS, ErrOutput},
		{modeTrailing, ErrOutput},
		{modeMissing, ErrOutput},
		{modeHuge, ErrOutputTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			f := newFixture(t, "")
			f.eng.maxOutput = 64 << 10
			var progress []int

			err := f.eng.Transcribe(context.Background(), f.audio(t), f.model(t, tt.mode), outRef(t, "x.json"), shared.LoadMax, func(p int) { progress = append(progress, p) }, nil)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if len(progress) != 0 {
				t.Errorf("progress reported: %v", progress)
			}
			f.assertClean(t)
		})
	}
}

func TestTranscribeStartFailure(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	f := newFixture(t, "")
	f.eng.cfg.Binary = filepath.Join(t.TempDir(), "missing", "whisper-cli.exe")

	err := f.eng.Transcribe(context.Background(), f.audio(t), f.model(t, modeOK), outRef(t, "x.json"), shared.LoadMax, nil, nil)
	if !errors.Is(err, ErrStart) {
		t.Fatalf("err = %v, want ErrStart", err)
	}
	f.assertClean(t)
}

func TestTranscribeWriteFailure(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	f := newFixture(t, "")
	f.store.FailWrites(shared.ErrStorageFull)
	var progress []int

	err := f.eng.Transcribe(context.Background(), f.audio(t), f.model(t, modeOK), outRef(t, "x.json"), shared.LoadMax, func(p int) { progress = append(progress, p) }, nil)
	if !errors.Is(err, shared.ErrStorageFull) {
		t.Fatalf("err = %v, want ErrStorageFull", err)
	}
	if len(progress) > 0 && progress[len(progress)-1] == 100 {
		t.Errorf("100%% reported although the result was not stored")
	}
	f.assertClean(t)
}

func TestTranscribeConcurrent(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	f := newFixture(t, "")
	audio, model := f.audio(t), f.model(t, modeOK)

	const n = 4
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Go(func() {
			errs[i] = f.eng.Transcribe(context.Background(), audio, model, outRef(t, "c"+string(rune('0'+i))+".json"), shared.LoadMax, nil, nil)
		})
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("call %d: %v", i, err)
		}
	}
	if got := len(f.store.Paths()); got != n {
		t.Errorf("%d transcripts stored, want %d", got, n)
	}
}

func TestCleanupWork(t *testing.T) {
	f := newFixture(t, "")
	for _, d := range []string{runPrefix + "a/sub", runPrefix + "b", "keep"} {
		if err := os.MkdirAll(filepath.Join(f.work, filepath.FromSlash(d)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(f.work, runPrefix+"a", "out.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	n, err := f.eng.CleanupWork(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("CleanupWork = %d, %v; want 2, nil", n, err)
	}

	entries, _ := os.ReadDir(f.work)
	if len(entries) != 1 || entries[0].Name() != "keep" {
		t.Errorf("left: %v", entries)
	}

	missing := newFixture(t, "")
	if n, err := missing.eng.CleanupWork(context.Background()); n != 0 || err != nil {
		t.Errorf("missing work dir: %d, %v", n, err)
	}
}

func TestNewValidatesConfig(t *testing.T) {
	root := t.TempDir()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "whisper-cli.bat")
	if err := os.WriteFile(script, []byte("@echo off"), 0o600); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(root, workName)
	paths, store := &fakePaths{root: root}, testenv.NewMemArtifacts()
	valid := Config{Binary: bin, DataRoot: root, WorkDir: work}

	tests := []struct {
		name string
		edit func(*Config)
		nilP bool
		ok   bool
	}{
		{name: "valid defaults", edit: func(*Config) {}, ok: true},
		{name: "valid explicit", edit: func(c *Config) { c.Threads, c.Language, c.Timeout = 8, "en", time.Hour }, ok: true},
		{name: "nil ports", edit: func(*Config) {}, nilP: true},
		{name: "relative binary", edit: func(c *Config) { c.Binary = "whisper-cli" }},
		{name: "unclean binary", edit: func(c *Config) {
			c.Binary = filepath.Dir(bin) + string(filepath.Separator) + "." + string(filepath.Separator) + filepath.Base(bin)
		}},
		{name: "missing binary", edit: func(c *Config) { c.Binary = filepath.Join(root, "nope.exe") }},
		{name: "directory binary", edit: func(c *Config) { c.Binary = root }},
		{name: "relative root", edit: func(c *Config) { c.DataRoot = "data" }},
		{name: "work outside root", edit: func(c *Config) { c.WorkDir = filepath.Join(t.TempDir(), "w") }},
		{name: "work is root", edit: func(c *Config) { c.WorkDir = root }},
		{name: "work in an artifact kind", edit: func(c *Config) { c.WorkDir = filepath.Join(root, "transcripts", "tmp") }},
		{name: "work in an artifact kind, other case", edit: func(c *Config) { c.WorkDir = filepath.Join(root, "Models") }},
		{name: "negative threads", edit: func(c *Config) { c.Threads = -1 }},
		{name: "negative logical CPUs", edit: func(c *Config) { c.LogicalCPUs = -1 }},
		{name: "too many threads", edit: func(c *Config) { c.Threads = maxThreads + 1 }},
		{name: "unknown language", edit: func(c *Config) { c.Language = "xx" }},
		{name: "upper-case language", edit: func(c *Config) { c.Language = "EN" }},
		{name: "language flag", edit: func(c *Config) { c.Language = "-m" }},
		{name: "negative timeout", edit: func(c *Config) { c.Timeout = -time.Second }},
		// Windows would run a batch file through cmd.exe.
		{name: "batch file binary", edit: func(c *Config) { c.Binary = script }, ok: runtime.GOOS != "windows"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.edit(&cfg)
			p, s := Paths(paths), Artifacts(store)
			if tt.nilP {
				p, s = nil, nil
			}

			eng, err := New(cfg, p, s)
			if tt.ok {
				if err != nil {
					t.Fatalf("New: %v", err)
				}
				if eng.cpus < 1 || !languages[eng.language] {
					t.Errorf("defaults not applied: cpus %d, language %q", eng.cpus, eng.language)
				}
				return
			}
			if !errors.Is(err, ErrConfig) {
				t.Fatalf("err = %v, want ErrConfig", err)
			}
		})
	}
}
