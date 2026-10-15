package audiocapture

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// Defaults of Config.
const (
	DefaultChunkSeconds   = 60
	DefaultStartTimeout   = 15 * time.Second // the 5 s activation timeout of process loopback plus the fallback
	DefaultStopGrace      = 10 * time.Second
	DefaultHangTimeout    = 30 * time.Second // six missed heartbeats of the child
	DefaultRestartBackoff = time.Second      // doubled per restart: 1 s, 2 s, 4 s
	DefaultMaxRestarts    = 3

	// keptCaptures is how many ended captures stay for Wait and Diagnostics.
	keptCaptures = 8
	// killWait bounds the wait for a killed child to be reaped when the caller gave up.
	killWait = 5 * time.Second
)

var (
	// ErrCaptureFailed means a capture ended by itself: the child reported a failure (device lost and not
	// recovered) or kept dying until the restart budget was used up. Wait returns it wrapped with the reason.
	ErrCaptureFailed = errors.New("audiocapture: the capture ended by itself")

	// ErrNotCapturing means the recorder has no capture for the recording.
	ErrNotCapturing = errors.New("audiocapture: no capture for this recording")

	// ErrAlreadyCapturing means a capture of the recording runs already.
	ErrAlreadyCapturing = errors.New("audiocapture: the recording is already capturing")
)

// ProcessChecker tells whether a process runs: process loopback activates even for a dead PID (ADR-0002), so an
// INCLUDE target is checked first. The Windows implementation comes with the WASAPI source.
type ProcessChecker interface {
	Alive(pid uint32) (bool, error)
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
}

// DefaultConfig returns 60 s chunks, a 15 s start timeout, a 10 s stop grace, a 30 s hang timeout and three restarts
// after 1, 2 and 4 s.
func DefaultConfig() Config {
	return Config{
		ChunkSeconds: DefaultChunkSeconds, StartTimeout: DefaultStartTimeout, StopGrace: DefaultStopGrace,
		HangTimeout: DefaultHangTimeout, RestartBackoff: DefaultRestartBackoff, MaxRestarts: DefaultMaxRestarts,
	}
}

// Deps are the collaborators of the Recorder.
type Deps struct {
	Files     TrackFiles     // Finalize reads the chunks and writes the track through it
	Launcher  Launcher       // starts the recorder child
	Processes ProcessChecker // checks INCLUDE targets; nil refuses them
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
}

// New validates the configuration and builds the Recorder. It starts nothing.
func New(deps Deps, cfg Config) (*Recorder, error) {
	switch {
	case deps.Files == nil || deps.Launcher == nil:
		return nil, errors.New("audiocapture: files and launcher are required")
	case cfg.ChunkSeconds < minChunkSeconds || cfg.ChunkSeconds > maxChunkSeconds:
		return nil, fmt.Errorf("audiocapture: chunk seconds %d outside %d..%d", cfg.ChunkSeconds, minChunkSeconds, maxChunkSeconds)
	case cfg.StartTimeout <= 0 || cfg.StopGrace <= 0 || cfg.HangTimeout <= 0 || cfg.RestartBackoff <= 0:
		return nil, errors.New("audiocapture: timeouts must be positive")
	case cfg.MaxRestarts < 0:
		return nil, errors.New("audiocapture: max restarts must not be negative")
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

// Start captures the whole computer except this app's own process tree (process loopback EXCLUDE, falling back to
// classic loopback inside the child) until TASK-059 lets the user pick the lecture app.
func (r *Recorder) Start(ctx context.Context, id shared.RecordingID, chunks shared.ArtifactRef) error {
	return r.StartTarget(ctx, id, chunks, Target{Mode: ModeExclude, PID: r.cfg.OwnPID})
}

// StartTarget starts the capture of target into chunks and returns once the child reports a running capture. A
// cancelled ctx or the start timeout kills the child; an early exit returns the child's reason. A failure because
// this build has no capture wraps ErrUnavailable; device and start failures do not.
func (r *Recorder) StartTarget(ctx context.Context, id shared.RecordingID, chunks shared.ArtifactRef, target Target) error {
	if err := r.checkStart(id, chunks, target); err != nil {
		return fmt.Errorf("audiocapture: %s %s: %w", opStartCapture, id, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	c := newCapture(id, chunks, target)
	r.mu.Lock()
	if old := r.captures[id]; old != nil && !old.isEnded() {
		r.mu.Unlock()
		return fmt.Errorf("audiocapture: %s %s: %w", opStartCapture, id, ErrAlreadyCapturing)
	}
	r.captures[id] = c
	r.mu.Unlock()

	startCtx, cancel := context.WithTimeoutCause(ctx, r.cfg.StartTimeout,
		fmt.Errorf("the recorder did not start within %v", r.cfg.StartTimeout))
	defer cancel()
	go r.supervise(c)

	select {
	case err := <-c.started:
		if err == nil {
			r.log.Info("capture started", slog.String("recording", id.String()), slog.String("target", string(target.Mode)))
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

func (r *Recorder) checkStart(id shared.RecordingID, chunks shared.ArtifactRef, target Target) error {
	if err := id.Validate(); err != nil {
		return err
	}
	if chunks.IsZero() || chunks.Kind() != shared.ArtifactRecordings {
		return fmt.Errorf("%w: chunks must be a recordings directory", shared.ErrInvalidArtifactRef)
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
// is killed and ctx's error returned.
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
// duration counted in frames. It needs no capture and is safe to run concurrently with other recordings.
func (r *Recorder) Finalize(ctx context.Context, chunks, output shared.ArtifactRef) (time.Duration, error) {
	return assemble(ctx, r.deps.Files, chunks, output, r.log)
}

// Diagnostics returns a snapshot of the capture of id (TASK-058). ok is false for an unknown recording; ended
// captures stay readable for a while.
func (r *Recorder) Diagnostics(id shared.RecordingID) (d Diagnostics, ok bool) {
	c := r.lookup(id)
	if c == nil {
		return Diagnostics{}, false
	}
	return c.snapshot(), true
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
	r.ended = append(r.ended, c.id)
	for len(r.ended) > keptCaptures {
		old := r.ended[0]
		r.ended = r.ended[1:]
		if oc := r.captures[old]; oc != nil && oc.isEnded() {
			delete(r.captures, old)
		}
	}
}
