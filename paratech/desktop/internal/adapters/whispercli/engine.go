package whispercli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/services/loadpolicy"
	"paratech/desktop/internal/domain/transcription/transcript"
	"paratech/desktop/internal/domain/transcription/values"
)

const (
	// runPrefix names the private per-call directories below Config.WorkDir.
	runPrefix = "run-"

	// outName is the -of prefix inside the run directory; the CLI adds ".json".
	outName = "out"

	dirPerm = 0o700

	// maxThreads bounds Config.Threads.
	maxThreads = values.MaxRunThreads

	// waitDelay bounds the wait for the process and its pipes after a kill or exit.
	waitDelay = 5 * time.Second

	// removeAttempts and removeStep retry the run directory removal: on Windows a scanner may hold a file briefly.
	removeAttempts = 5
	removeStep     = 50 * time.Millisecond
)

// Placeholders that replace directories in stderr text.
const (
	placeholderData = "<data>"
	placeholderBin  = "<bin>"
)

// Paths resolves an artifact reference to the absolute OS path of the file below the data root. The app wires it
// from the Storage Manager; the adapter checks the result again.
type Paths interface {
	Path(ctx context.Context, ref shared.ArtifactRef) (string, error)
}

// Artifacts stores the transcript document atomically (the filestore adapter satisfies it).
type Artifacts interface {
	Write(ctx context.Context, ref shared.ArtifactRef, content io.Reader) error
}

// Config is the configuration given by the app; nothing in it comes from job data.
type Config struct {
	// Binary is the absolute path of whisper-cli (whisper-cli.exe on Windows).
	Binary string

	// DataRoot is the absolute data root; model, audio and work paths must lie below it, and it is redacted
	// from error messages.
	DataRoot string

	// WorkDir is the absolute directory below DataRoot (outside the artifact kind directories) for the private
	// run directories.
	WorkDir string

	// Threads, when set, is the -t value of every run whatever the load mode (tests, diagnostics); 0 means the
	// thread count of the mode (loadpolicy).
	Threads int

	// LogicalCPUs is the logical processor count the load modes are computed for; 0 means runtime.NumCPU().
	LogicalCPUs int

	// Language is the -l value from the whisper.cpp list; "" means LanguageAuto.
	Language string

	// Timeout kills a run that takes longer; 0 means no limit besides the context.
	Timeout time.Duration

	// Log receives the warning that the CPU cap of a run could not be set; nil discards it.
	Log *slog.Logger
}

// Engine runs whisper-cli once per Transcribe call. It is safe for concurrent use.
type Engine struct {
	cfg       Config
	paths     Paths
	artifacts Artifacts
	threads   int // 0: by the load mode
	cpus      int
	policy    loadpolicy.Policy
	language  string
	redact    []redaction
	log       *slog.Logger

	// Overridable in tests.
	env           []string
	maxOutput     int64
	progressEvery time.Duration
	now           func() time.Time
}

// New validates the configuration and returns an Engine.
func New(cfg Config, paths Paths, artifacts Artifacts) (*Engine, error) {
	if paths == nil || artifacts == nil {
		return nil, fmt.Errorf("%w: paths and artifacts are required", ErrConfig)
	}

	if err := checkBinary(cfg.Binary); err != nil {
		return nil, err
	}

	if err := checkDirs(cfg.DataRoot, cfg.WorkDir); err != nil {
		return nil, err
	}

	if cfg.Threads < 0 || cfg.Threads > maxThreads {
		return nil, fmt.Errorf("%w: threads must be 0..%d", ErrConfig, maxThreads)
	}
	cpus := cfg.LogicalCPUs
	if cpus == 0 {
		cpus = runtime.NumCPU()
	}
	if cpus < 1 {
		return nil, fmt.Errorf("%w: negative logical CPU count", ErrConfig)
	}

	lang := cfg.Language
	if lang == "" {
		lang = LanguageAuto
	}
	if !languages[lang] {
		return nil, fmt.Errorf("%w: unknown language %q", ErrConfig, lang)
	}

	if cfg.Timeout < 0 {
		return nil, fmt.Errorf("%w: negative timeout", ErrConfig)
	}

	log := cfg.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Engine{
		cfg:       cfg,
		paths:     paths,
		artifacts: artifacts,
		threads:   cfg.Threads,
		cpus:      cpus,
		policy:    loadpolicy.New(),
		language:  lang,
		log:       log,
		redact: []redaction{
			newRedaction(cfg.DataRoot, placeholderData),
			newRedaction(filepath.Dir(cfg.Binary), placeholderBin),
		},
		env:           []string{},
		maxOutput:     defaultMaxOutput,
		progressEvery: defaultProgressEvery,
		now:           time.Now,
	}, nil
}

func checkBinary(bin string) error {
	if !filepath.IsAbs(bin) || filepath.Clean(bin) != bin {
		return fmt.Errorf("%w: binary path must be absolute and clean", ErrConfig)
	}
	if !binaryNameOK(bin) {
		return fmt.Errorf("%w: binary must be an .exe on Windows", ErrConfig)
	}

	fi, err := os.Stat(bin)
	if err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("%w: binary is not a regular file", ErrConfig)
	}

	return nil
}

func checkDirs(root, work string) error {
	for _, d := range []string{root, work} {
		if !filepath.IsAbs(d) || filepath.Clean(d) != d {
			return fmt.Errorf("%w: data root and work directory must be absolute and clean", ErrConfig)
		}
	}

	if !within(root, work) {
		return fmt.Errorf("%w: work directory must lie below the data root", ErrConfig)
	}

	// Run directories inside an artifact kind would show up as artifacts.
	rel, _ := filepath.Rel(root, work)
	first, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
	for _, k := range []shared.ArtifactKind{
		shared.ArtifactRecordings, shared.ArtifactTranscripts, shared.ArtifactModels, shared.ArtifactExports,
	} {
		if strings.EqualFold(first, string(k)) {
			return fmt.Errorf("%w: work directory must not lie inside an artifact directory", ErrConfig)
		}
	}

	if err := checkArg(work); err != nil {
		return fmt.Errorf("%w: work directory: %w", ErrConfig, err)
	}

	return nil
}

// Transcribe recognises audio with model and stores the transcript document at output. Cancelling ctx kills the
// process; on any failure nothing is written and the run directory is removed.
//
// load is the load mode of this run (loadpolicy): it sets the threads, the priority and, on Windows, the hard CPU cap
// of the job object. A cap that cannot be set is logged and the run goes on uncapped with the lowered priority.
//
// progress, if not nil, receives increasing percentages up to 100 (100 only after the result is stored), possibly
// skipping values. It is called on a goroutine of the adapter, so it must be safe for that; calls never overlap and
// none happens after Transcribe returns. A slow callback never stalls the process, but Transcribe waits for it.
//
// segment, if not nil, receives each segment the CLI prints on stdout while it runs, in order, with the guarantees
// of progress (its own goroutine, no overlap, none after return). These are a live preview with the rules of the
// result (valid UTF-8, no control characters, bounded, non-decreasing starts); the stored document is the result.
// Without segment stdout goes to the null device.
func (e *Engine) Transcribe(ctx context.Context, audio, model, output shared.ArtifactRef, load shared.RecognitionLoad,
	progress func(percent int), segment func(transcript.Segment)) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := errors.Join(
		wantKind("audio", audio, shared.ArtifactRecordings),
		wantKind("model", model, shared.ArtifactModels),
		wantKind("output", output, shared.ArtifactTranscripts),
	); err != nil {
		return err
	}

	modelPath, err := e.resolve(ctx, model)
	if err != nil {
		return err
	}

	audioPath, err := e.resolve(ctx, audio)
	if err != nil {
		return err
	}

	runDir, err := e.newRunDir()
	if err != nil {
		return err
	}
	// A failed removal leaves only a private directory that CleanupWork removes at the next start.
	defer func() { _ = removeAll(runDir) }()

	policy := e.runPolicy(load)
	outPrefix := filepath.Join(runDir, outName)
	args := buildArgs(modelPath, audioPath, outPrefix, policy.Threads(), e.language)

	var send func(int)
	if progress != nil {
		pump := startPump(progress)
		defer pump.stop()
		send = pump.send
	}

	sink := newStderrSink(newReporter(send, e.progressEvery, e.now))

	var out *stdoutSink
	if segment != nil {
		pump := startSegmentPump(segment)
		defer pump.stop()
		out = newStdoutSink(pump.send)
	}

	if err := e.run(ctx, runDir, args, policy, sink, out); err != nil {
		return err
	}

	doc, err := e.result(outPrefix+jsonExt, sink)
	if err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("whispercli: transcription stopped: %w", err)
	}

	if err := e.artifacts.Write(ctx, output, bytes.NewReader(doc)); err != nil {
		return err
	}

	sink.final()
	return nil
}

// runPolicy is the policy of the mode on this computer; Config.Threads, when set, replaces its thread count.
func (e *Engine) runPolicy(load shared.RecognitionLoad) values.RunPolicy {
	p := e.policy.For(load, e.cpus)
	if e.threads == 0 {
		return p
	}
	if o, err := values.NewRunPolicy(e.threads, p.LowPriority(), p.CPUCapPercent()); err == nil {
		return o
	}
	return p
}

// resolve turns a reference into a checked path of an existing regular file (links are refused).
func (e *Engine) resolve(ctx context.Context, ref shared.ArtifactRef) (string, error) {
	p, err := e.paths.Path(ctx, ref)
	if err != nil {
		return "", err
	}

	if err := checkPath(e.cfg.DataRoot, p); err != nil {
		return "", fmt.Errorf("%w (%s)", err, ref)
	}

	fi, err := os.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("%w: %s", shared.ErrNotFound, ref)
	case err != nil:
		return "", fmt.Errorf("%w: %s: %w", ErrInvalidInput, ref, bareErr(err))
	case !fi.Mode().IsRegular():
		return "", fmt.Errorf("%w: %s is not a regular file", ErrInvalidInput, ref)
	}

	return p, nil
}

// newRunDir creates a fresh private directory for one call.
func (e *Engine) newRunDir() (string, error) {
	if err := os.MkdirAll(e.cfg.WorkDir, dirPerm); err != nil {
		return "", fmt.Errorf("%w: %w", ErrWorkDir, bareErr(err))
	}

	// A link (symlink or junction) here would move run directories out of the data root.
	fi, err := os.Lstat(e.cfg.WorkDir)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrWorkDir, bareErr(err))
	}
	if fi.Mode().Type() != fs.ModeDir {
		return "", fmt.Errorf("%w: not a plain directory", ErrWorkDir)
	}

	dir, err := os.MkdirTemp(e.cfg.WorkDir, runPrefix)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrWorkDir, bareErr(err))
	}

	return dir, nil
}

// run starts the process with the priority and CPU cap of policy and waits for it. It returns nil only for exit code 0.
// out, if not nil, reads stdout.
func (e *Engine) run(ctx context.Context, runDir string, args []string, policy values.RunPolicy, sink *stderrSink,
	out *stdoutSink) error {
	var runCtx context.Context
	var cancel context.CancelFunc
	if e.cfg.Timeout > 0 {
		runCtx, cancel = context.WithTimeoutCause(ctx, e.cfg.Timeout, ErrTimeout)
	} else {
		runCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	grp, capErr := newProcGroup(policy.CPUCapPercent())
	defer grp.close()
	if capErr != nil {
		e.log.Warn("speech recognition runs without the CPU cap of its load mode",
			slog.Int("cap_percent", policy.CPUCapPercent()), slog.Any("err", capErr))
	}

	cmd := exec.CommandContext(runCtx, e.cfg.Binary, args...) //nolint:gosec // binary and arguments are validated (args.go)
	cmd.Dir = runDir
	cmd.Env = append([]string{}, e.env...)
	// Stdin stays nil (the null device). Stdout repeats the recognised text: it is parsed for live segments or goes
	// to the null device, never to logs.
	if out != nil {
		cmd.Stdout = out
	}
	cmd.Stderr = sink
	cmd.WaitDelay = waitDelay
	setProcAttrs(cmd, policy.LowPriority())
	cmd.Cancel = func() error { return killRun(cmd.Process, grp) }

	if err := cmd.Start(); err != nil {
		if cerr := stopped(ctx, runCtx); cerr != nil {
			return cerr
		}
		return fmt.Errorf("%w: %s", ErrStart, applyRedactions(bareErr(err).Error(), e.redact))
	}

	// Best effort: without the job only the process itself is killed (whisper-cli starts no children).
	_ = grp.attach(cmd.Process.Pid)

	err := cmd.Wait()
	sink.stop()
	if out != nil {
		out.stop()
	}

	if err == nil || (errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState.Success()) {
		return nil
	}

	if cerr := stopped(ctx, runCtx); cerr != nil {
		return cerr
	}

	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}

	return &RunError{Err: ErrExit, Code: code, Stderr: sink.tailText(e.redact)}
}

// killRun is exec.Cmd.Cancel. It returns os.ErrProcessDone for a process that has already exited, so exec keeps
// its real exit status and a finished run is not reported as cancelled or timed out.
func killRun(p *os.Process, grp *procGroup) error {
	if processDone(p.Pid) {
		return os.ErrProcessDone
	}

	grp.kill()
	if err := p.Kill(); errors.Is(err, os.ErrProcessDone) {
		return os.ErrProcessDone
	}
	return nil
}

// stopped maps a done context to the caller's error: the time limit or the caller's cancellation.
func stopped(ctx, runCtx context.Context) error {
	if runCtx.Err() == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("whispercli: transcription stopped: %w", err)
	}
	if errors.Is(context.Cause(runCtx), ErrTimeout) {
		return ErrTimeout
	}
	return fmt.Errorf("whispercli: transcription stopped: %w", runCtx.Err())
}

// result reads, validates and renders the CLI result.
func (e *Engine) result(path string, sink *stderrSink) ([]byte, error) {
	data, err := readResult(path, e.maxOutput)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, &RunError{Err: ErrNoOutput, Code: 0, Stderr: sink.tailText(e.redact)}
	}
	if errors.Is(err, ErrOutput) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOutput, bareErr(err))
	}

	doc, err := parseResult(data)
	if err != nil {
		return nil, err
	}

	return encodeTranscript(doc)
}

// CleanupWork removes run directories left behind by a crash. Call it once at startup, before any Transcribe.
// It returns how many it removed and the first error.
func (e *Engine) CleanupWork(ctx context.Context) (int, error) {
	entries, err := os.ReadDir(e.cfg.WorkDir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrWorkDir, bareErr(err))
	}

	n := 0
	var first error
	for _, en := range entries {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		if !strings.HasPrefix(en.Name(), runPrefix) {
			continue
		}

		if err := removeAll(filepath.Join(e.cfg.WorkDir, en.Name())); err != nil {
			if first == nil {
				first = fmt.Errorf("%w: %w", ErrWorkDir, bareErr(err))
			}
			continue
		}
		n++
	}

	return n, first
}

// removeAll removes a run directory, retrying briefly. It does not follow links.
func removeAll(dir string) error {
	var err error
	for i := range removeAttempts {
		if err = os.RemoveAll(dir); err == nil {
			return nil
		}
		if i < removeAttempts-1 {
			time.Sleep(time.Duration(i+1) * removeStep)
		}
	}
	return err
}

// bareErr drops the path from os errors, so messages do not carry user paths.
func bareErr(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Err
	}
	var ee *exec.Error
	if errors.As(err, &ee) {
		return ee.Err
	}
	return err
}
