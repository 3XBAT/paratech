package audiocapture

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// Defaults of Config.
const (
	DefaultChunkSeconds   = 60
	DefaultStartTimeout   = 15 * time.Second // the 5 s activation timeout of process loopback plus the fallback
	DefaultStopGrace      = 7 * time.Second  // below the 10 s the recording session gives a stop, kill included
	DefaultHangTimeout    = 30 * time.Second // six missed heartbeats of the child
	DefaultRestartBackoff = time.Second      // doubled per restart: 1 s, 2 s, 4 s
	DefaultMaxRestarts    = 3
	DefaultRebindInterval = 3 * time.Second // as OBS looks for a lost window again
	DefaultMaxRebinds     = 20

	// keptCaptures is how many ended captures stay for Wait and Diagnostics.
	keptCaptures = 8
	// killWait bounds how long Stop past its context waits for the killed child; the supervisor reaps it anyway.
	killWait = time.Second
)

var (
	// ErrCaptureFailed means a capture ended by itself: the child reported a failure (device lost and not
	// recovered) or kept dying until the restart budget was used up. Wait returns it wrapped with the reason.
	ErrCaptureFailed = errors.New("audiocapture: the capture ended by itself")

	// ErrNotCapturing means the recorder has no capture for the recording.
	ErrNotCapturing = errors.New("audiocapture: no capture for this recording")

	// ErrAlreadyCapturing means a capture of the recording runs already.
	ErrAlreadyCapturing = errors.New("audiocapture: the recording is already capturing")

	// ErrProcessCheck means it could not be told whether a process runs.
	ErrProcessCheck = errors.New("audiocapture: cannot check the process")
)

// ProcessChecker tells whether a process runs: process loopback activates even for a dead PID (ADR-0002), so an
// INCLUDE target is checked first. PlatformProcesses is the implementation of this OS.
type ProcessChecker interface {
	Alive(pid uint32) (bool, error)
}

// AppFinder finds the process to capture for a chosen app and tells whether it still runs (TASK-059). *Apps is the
// implementation.
type AppFinder interface {
	// Find returns the process of app, checked right before (it runs, it is app, it is in this Windows session); an
	// app that does not run yields an error wrapping recording.ErrSourceNotRunning.
	Find(ctx context.Context, app shared.AppName) (ProcessIdentity, error)

	// Same reports whether id still names the same running process.
	Same(ctx context.Context, id ProcessIdentity) (bool, error)
}

// Config tunes the Recorder; DefaultConfig gives the production values.
type Config struct {
	ChunkSeconds   int           // chunk length, 30..60
	StartTimeout   time.Duration // until the child reports a running capture
	StopGrace      time.Duration // from closing the child's stdin to killing it
	HangTimeout    time.Duration // a child without any event for this long is killed and restarted
	RestartBackoff time.Duration // delay before the first restart, doubled for each next one
	MaxRestarts    int           // restarts per recording after crashes or hangs
	OwnPID         uint32        // excluded by the default target; 0 means this process
	RebindInterval time.Duration // how often a capture of an app checks its process, or looks for a new one
	MaxRebinds     int           // captures of a new instance of the app per recording
	// CheckGrace is how long a sound check child (CheckLevels) may run past its duration before it is killed; 0 takes
	// DefaultCheckGrace.
	CheckGrace time.Duration
}

// DefaultConfig returns 60 s chunks, a 15 s start timeout, a 7 s stop grace, a 30 s hang timeout, three restarts
// after 1, 2 and 4 s, a check of the captured app every 3 s with up to 20 re-binds, and a sound check child killed
// DefaultCheckGrace past its duration.
func DefaultConfig() Config {
	return Config{
		ChunkSeconds: DefaultChunkSeconds, StartTimeout: DefaultStartTimeout, StopGrace: DefaultStopGrace,
		HangTimeout: DefaultHangTimeout, RestartBackoff: DefaultRestartBackoff, MaxRestarts: DefaultMaxRestarts,
		RebindInterval: DefaultRebindInterval, MaxRebinds: DefaultMaxRebinds, CheckGrace: DefaultCheckGrace,
	}
}

// Deps are the collaborators of the Recorder.
type Deps struct {
	Files     TrackFiles     // Finalize reads the chunks and writes the track through it
	Launcher  Launcher       // starts the recorder child
	Processes ProcessChecker // checks the INCLUDE targets of StartTarget; nil refuses them
	Apps      AppFinder      // finds the app of a Start; nil refuses apps
	Log       *slog.Logger
}

// Recorder captures system audio in a recorder child process per recording (ADR-0002 Decision 4) and assembles the
// chunks into the final track. It satisfies the Recorder port of the recording session. A child that crashes or
// hangs is restarted with a bounded backoff; the chunk numbering continues, so nothing is overwritten. It is safe for
// concurrent use; captures of different recordings are independent.
type Recorder struct {
	cfg   Config
	deps  Deps
	log   *slog.Logger
	now   func() time.Time
	after func(time.Duration) <-chan time.Time

	mu       sync.Mutex
	captures map[shared.RecordingID]*capture
	ended    []shared.RecordingID // ended captures, oldest first
	check    *runningCheck        // the running sound check; nil when none

	checking sync.Mutex // held by the running sound check
}

// New validates the configuration and builds the Recorder. It starts nothing.
func New(deps Deps, cfg Config) (*Recorder, error) {
	switch {
	case deps.Files == nil || deps.Launcher == nil:
		return nil, errors.New("audiocapture: files and launcher are required")
	case cfg.ChunkSeconds < minChunkSeconds || cfg.ChunkSeconds > maxChunkSeconds:
		return nil, fmt.Errorf("audiocapture: chunk seconds %d outside %d..%d", cfg.ChunkSeconds, minChunkSeconds, maxChunkSeconds)
	case cfg.StartTimeout <= 0 || cfg.StopGrace <= 0 || cfg.HangTimeout <= 0 || cfg.RestartBackoff <= 0 ||
		cfg.RebindInterval <= 0:
		return nil, errors.New("audiocapture: timeouts must be positive")
	case cfg.MaxRestarts < 0 || cfg.MaxRebinds < 0:
		return nil, errors.New("audiocapture: max restarts and re-binds must not be negative")
	case cfg.CheckGrace < 0:
		return nil, errors.New("audiocapture: the sound check grace must not be negative")
	}
	if cfg.CheckGrace == 0 {
		cfg.CheckGrace = DefaultCheckGrace
	}
	if cfg.OwnPID == 0 {
		cfg.OwnPID = uint32(os.Getpid()) //nolint:gosec // PIDs fit in 32 bits on every supported OS
	}
	log := deps.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Recorder{
		cfg: cfg, deps: deps, log: log, now: time.Now, after: time.After,
		captures: map[shared.RecordingID]*capture{},
	}, nil
}

// Start captures source into chunks (ADR-0002 Decision 1). The whole computer is everything except this app's own
// process tree (process loopback EXCLUDE). An app is process loopback INCLUDE of the process tree Deps.Apps finds
// for it; one that does not run fails with an error wrapping recording.ErrSourceNotRunning. The process of an app is
// checked every RebindInterval: when it exits, the capture waits for the app and then captures its new instance (a
// re-bind), with silence for the time between. Both modes fall back to classic loopback in the child; see StartTarget.
func (r *Recorder) Start(ctx context.Context, id shared.RecordingID, chunks shared.ArtifactRef, source values.Source) error {
	if source.IsWholeComputer() {
		return r.StartTarget(ctx, id, chunks, Target{Mode: ModeExclude, PID: r.cfg.OwnPID})
	}

	app := source.App()
	if err := checkChunks(id, chunks); err != nil {
		return fmt.Errorf("audiocapture: %s %s: %w", opStartCapture, id, err)
	}
	if r.deps.Apps == nil {
		return fmt.Errorf("audiocapture: %s %s: %w: this build cannot find %s", opStartCapture, id, ErrInvalidTarget, app)
	}
	proc, err := r.deps.Apps.Find(ctx, app)
	if err != nil {
		return fmt.Errorf("audiocapture: %s %s: %w", opStartCapture, id, err)
	}
	target := Target{Mode: ModeInclude, PID: proc.PID}
	if err := target.Validate(); err != nil {
		return fmt.Errorf("audiocapture: %s %s: %w", opStartCapture, id, err)
	}
	return r.start(ctx, newCapture(id, chunks, target, &binding{proc: proc}))
}

// StartTarget starts the capture of target into chunks and returns once the child reports a running capture. A
// cancelled ctx or the start timeout kills the child; an early exit returns the child's reason. A failure because
// this build has no capture wraps ErrUnavailable; device and start failures do not.
func (r *Recorder) StartTarget(ctx context.Context, id shared.RecordingID, chunks shared.ArtifactRef, target Target) error {
	if err := r.checkStart(id, chunks, target); err != nil {
		return fmt.Errorf("audiocapture: %s %s: %w", opStartCapture, id, err)
	}
	return r.start(ctx, newCapture(id, chunks, target, nil))
}

func (r *Recorder) start(ctx context.Context, c *capture) error {
	id, target := c.id, c.target
	if err := ctx.Err(); err != nil {
		c.release()
		return err
	}

	r.mu.Lock()
	if old := r.captures[id]; old != nil && !old.isEnded() {
		r.mu.Unlock()
		c.release()
		return fmt.Errorf("audiocapture: %s %s: %w", opStartCapture, id, ErrAlreadyCapturing)
	}
	r.captures[id] = c
	check := r.check
	r.mu.Unlock()
	// From here a new sound check answers busy; one that runs is stopped before the child starts.
	r.stopCheck(check)

	startCtx, cancel := context.WithTimeoutCause(ctx, r.cfg.StartTimeout,
		fmt.Errorf("the recorder did not start within %v", r.cfg.StartTimeout))
	defer cancel()
	go r.supervise(c)

	select {
	case err := <-c.started:
		if err == nil {
			// The started event of the first child is applied before it is signalled, so the snapshot has it.
			d := c.snapshot()
			r.log.Info("capture started", slog.String("recording", id.String()), slog.String("windows", d.Windows.String()),
				slog.String("build", d.Windows.BuildString()), slog.String("target", string(target.Mode)),
				slog.String("mode", string(d.Mode)), slog.String("app", d.App.String()),
				slog.Uint64("target_pid", uint64(target.PID)), slog.String("device", d.Device),
				slog.String("format", d.Format.String()))
			return nil
		}
		<-c.done
		r.forget(c)
		return fmt.Errorf("audiocapture: %s %s: %w", opStartCapture, id, err)
	case <-startCtx.Done():
		c.abort()
		<-c.done
		r.forget(c)
		return fmt.Errorf("audiocapture: %s %s: %w", opStartCapture, id, context.Cause(startCtx))
	}
}

func checkChunks(id shared.RecordingID, chunks shared.ArtifactRef) error {
	if err := id.Validate(); err != nil {
		return err
	}
	if chunks.IsZero() || chunks.Kind() != shared.ArtifactRecordings {
		return fmt.Errorf("%w: chunks must be a recordings directory", shared.ErrInvalidArtifactRef)
	}
	return nil
}

func (r *Recorder) checkStart(id shared.RecordingID, chunks shared.ArtifactRef, target Target) error {
	if err := checkChunks(id, chunks); err != nil {
		return err
	}
	if err := target.Validate(); err != nil {
		return err
	}
	if target.Mode != ModeInclude {
		return nil
	}
	if r.deps.Processes == nil {
		return fmt.Errorf("%w: this build cannot check the target process", ErrInvalidTarget)
	}
	alive, err := r.deps.Processes.Alive(target.PID)
	if err != nil {
		return fmt.Errorf("check the target process: %w", err)
	}
	if !alive {
		return fmt.Errorf("%w: process %d is not running", ErrInvalidTarget, target.PID)
	}
	return nil
}

// Stop closes the child's stdin and waits for it to stop, up to the grace period, then kills it; the child is always
// reaped. It returns nil after a clean stop and also when the capture had ended already. If ctx ends first the child
// is killed and ctx's error returned within killWait; the supervisor reaps the child afterwards and Wait reports the
// end.
func (r *Recorder) Stop(ctx context.Context, id shared.RecordingID) error {
	c := r.lookup(id)
	if c == nil {
		return fmt.Errorf("audiocapture: %s %s: %w", opStopCapture, id, ErrNotCapturing)
	}
	c.requestStop()

	select {
	case <-c.done:
	case <-ctx.Done():
		c.abort()
		select {
		case <-c.done:
		case <-r.after(killWait):
		}
		return fmt.Errorf("audiocapture: %s %s: %w", opStopCapture, id, ctx.Err())
	}
	if err := c.stopError(); err != nil {
		return fmt.Errorf("audiocapture: %s %s: %w", opStopCapture, id, err)
	}
	return nil
}

// Wait blocks until the capture of id has ended: nil after Stop, an error wrapping ErrCaptureFailed when it ended by
// itself. Cancelling ctx stops waiting, not capturing.
func (r *Recorder) Wait(ctx context.Context, id shared.RecordingID) error {
	c := r.lookup(id)
	if c == nil {
		return fmt.Errorf("audiocapture: %s %s: %w", opWaitCapture, id, ErrNotCapturing)
	}
	select {
	case <-c.done:
		return c.result()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Finalize assembles the readable chunks of chunks into the track output (16 kHz mono 16-bit WAV) and returns its
// duration counted in frames and its audible seconds. It needs no capture and is safe to run concurrently with other
// recordings.
func (r *Recorder) Finalize(ctx context.Context, chunks, output shared.ArtifactRef) (values.AssembledTrack, error) {
	return assemble(ctx, r.deps.Files, chunks, output, r.log)
}

// Diagnostics returns a snapshot of the capture of id (TASK-058) at once: ctx is not used. ok is false for an unknown
// recording; ended captures stay readable for a while (the latest keptCaptures).
func (r *Recorder) Diagnostics(_ context.Context, id shared.RecordingID) (d values.CaptureDiagnostics, ok bool) {
	c := r.lookup(id)
	if c == nil {
		return Diagnostics{}, false
	}
	return c.snapshot(), true
}

// RecentCaptures returns the diagnostics of the captures the recorder still knows (TASK-058, the support bundle): the
// ended ones it keeps, oldest end first, then the running ones. ctx is not used.
func (r *Recorder) RecentCaptures(context.Context) []values.CaptureDiagnostics {
	r.mu.Lock()
	var caps []*capture
	seen := map[*capture]bool{}
	for _, id := range r.ended {
		if c := r.captures[id]; c != nil && c.isEnded() && !seen[c] {
			seen[c] = true
			caps = append(caps, c)
		}
	}
	for _, c := range r.captures {
		if !seen[c] && !c.isEnded() {
			caps = append(caps, c)
		}
	}
	r.mu.Unlock()

	out := make([]values.CaptureDiagnostics, 0, len(caps))
	for _, c := range caps {
		out = append(out, c.snapshot())
	}
	return out
}

func (r *Recorder) lookup(id shared.RecordingID) *capture {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.captures[id]
}

// forget drops a capture whose start failed, so that the recording can start again.
func (r *Recorder) forget(c *capture) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.captures[c.id] == c {
		delete(r.captures, c.id)
	}
}

// retire keeps an ended capture for Wait and Diagnostics and drops the oldest ended ones.
func (r *Recorder) retire(c *capture) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.captures[c.id] != c {
		return
	}
	// A recording captured again is listed once, as its latest end.
	r.ended = append(slices.DeleteFunc(r.ended, func(id shared.RecordingID) bool { return id == c.id }), c.id)
	for len(r.ended) > keptCaptures {
		old := r.ended[0]
		r.ended = r.ended[1:]
		if oc := r.captures[old]; oc != nil && oc.isEnded() {
			delete(r.captures, old)
		}
	}
}
