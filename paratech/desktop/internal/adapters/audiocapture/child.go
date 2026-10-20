package audiocapture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
)

// Exit codes of the recorder child.
const (
	ChildExitOK         = 0
	ChildExitInternal   = 1 // the chunk files could not be prepared or written
	ChildExitBadRequest = 2
	ChildExitFailed     = 3 // the capture could not start or ended by itself
)

// Defaults of ChildConfig.
const (
	DefaultStatsInterval = 5 * time.Second
	DefaultDrainTimeout  = 3 * time.Second
	DefaultMaxGap        = 60 * time.Second
	// DefaultCaptureLevelInterval is how often a capture reports its level for the live capsule (TASK-099): 4 times
	// a second.
	DefaultCaptureLevelInterval = 250 * time.Millisecond

	minGap = 100 * time.Millisecond // shorter differences between the clock and the frames are jitter
)

// ChildConfig tunes the recorder child; zero fields take the defaults.
type ChildConfig struct {
	// StatsInterval is how often the child reports its counters; it doubles as the heartbeat the parent watches.
	StatsInterval time.Duration
	// DrainTimeout bounds how long the stream may deliver buffered packets after a stop.
	DrainTimeout time.Duration
	// MaxGap caps the silence written for one discontinuity.
	MaxGap time.Duration
	// Now is the monotonic clock that measures gaps; time.Now by default.
	Now func() time.Time

	// LevelInterval is how often a level check reports its level: at most this often, so 20 events per second
	// by default.
	LevelInterval time.Duration
	// CaptureLevelInterval is how often a capture reports its level (TASK-099).
	CaptureLevelInterval time.Duration
}

func (c ChildConfig) withDefaults() ChildConfig {
	if c.StatsInterval <= 0 {
		c.StatsInterval = DefaultStatsInterval
	}
	if c.DrainTimeout <= 0 {
		c.DrainTimeout = DefaultDrainTimeout
	}
	if c.MaxGap <= 0 {
		c.MaxGap = DefaultMaxGap
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.LevelInterval <= 0 {
		c.LevelInterval = DefaultLevelInterval
	}
	if c.CaptureLevelInterval <= 0 {
		c.CaptureLevelInterval = DefaultCaptureLevelInterval
	}
	return c
}

// RunChild is the recorder child process (the --recorder run mode, ADR-0002 Decision 4). It reads one start request
// from stdin, captures from src into closed chunks written through files, reports events on stdout and returns the
// exit code. It stops when stdin closes or ctx ends: the stream is stopped and drained, the last chunk closed, and
// "stopped" reported. A capture that ends by itself closes its chunks too and reports "failed". The goroutine that
// watches stdin ends when stdin reaches EOF. A level check request captures its target from src for a while and
// reports levels instead (levelCheck); it writes no file.
func RunChild(ctx context.Context, stdin io.Reader, stdout io.Writer, src Source, files ChunkFiles, log *slog.Logger,
	cfg ChildConfig) int {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	c := &child{cfg: cfg.withDefaults(), out: &eventWriter{w: stdout}, log: log}

	in := newLineReader(stdin, maxLineBytes)
	line, oversized, err := in.next()
	if err == nil && oversized {
		err = fmt.Errorf("%w: request longer than %d bytes", ErrProtocol, maxLineBytes)
	}
	var (
		req   Request
		lc    LevelRequest
		level bool
	)
	if err == nil {
		req, lc, level, err = decodeChildRequest(line)
	}
	if err != nil {
		c.out.emit(event{Type: evFailed, Reason: shortText("bad request: " + err.Error())})
		return ChildExitBadRequest
	}
	c.req = req

	stop := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, stdin) // nothing is expected after the request; EOF is the stop
		close(stop)
	}()
	if level {
		return c.levelCheck(ctx, stop, src, lc)
	}
	return c.run(ctx, stop, src, files)
}

// child is the state of one recorder child run.
type child struct {
	cfg ChildConfig
	out *eventWriter
	log *slog.Logger
	req Request

	packets, silent, audible atomic.Uint64
	mono                     []float32 // scratch of the peak of a packet
	meter                    levelMeter
	frames                   atomic.Int64
	stream                   atomic.Value // Stream, once open

	w *chunkWriter
	// last is when the previous packet arrived. Gaps are measured from it, not from the stream start, so the drift
	// of the device clock over hours is never taken for a gap.
	last    time.Time
	started bool // the first packet arrived
}

func (c *child) run(ctx context.Context, stop <-chan struct{}, src Source, files ChunkFiles) int {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-stop:
			cancel()
		case <-runCtx.Done():
		}
	}()

	var bg sync.WaitGroup
	defer bg.Wait()
	beatDone := make(chan struct{})
	defer close(beatDone)
	bg.Go(func() { c.heartbeat(beatDone) })

	w, err := newChunkWriter(ctx, files, c.req.Chunks, c.req.ChunkSeconds, func(n int, frames int64, f Format) {
		c.out.emit(event{Type: evChunk, N: n, Frames: frames, Rate: f.SampleRate})
	})
	if err != nil {
		c.out.emit(event{Type: evFailed, Reason: shortText(err.Error())})
		return ChildExitInternal
	}
	c.w = w

	stream, err := src.Open(runCtx, c.req.effectiveTarget(), &childObserver{out: c.out})
	if err != nil {
		if runCtx.Err() != nil {
			c.out.emit(event{Type: evStopped})
			return ChildExitOK
		}
		c.out.emit(event{Type: evFailed, Reason: shortText("open capture: " + err.Error()),
			Unavailable: errors.Is(err, recording.ErrCaptureUnavailable)})
		return ChildExitFailed
	}
	c.stream.Store(stream)

	info := stream.Info()
	info.Windows.Product, info.Windows.DisplayVersion = shortText(info.Windows.Product), shortText(info.Windows.DisplayVersion)
	c.out.emit(event{Type: evStarted, Mode: string(info.Mode), Format: toWireFormat(info.Format),
		Windows: toWireWindows(info.Windows), Device: shortText(info.Device)})
	c.log.Info("capture started", slog.String("mode", string(info.Mode)), slog.String("format", info.Format.String()),
		slog.String("device", shortText(info.Device)))

	// The level goes out only while the capture runs: never after the final event.
	ticking := make(chan struct{})
	var levels sync.WaitGroup
	levels.Go(func() { c.reportLevels(&c.meter, c.cfg.CaptureLevelInterval, ticking) })
	failure := c.capture(runCtx, stream)
	close(ticking)
	levels.Wait()

	stream.Stop()
	if err := stream.Close(); err != nil {
		c.log.Warn("closing the capture failed", slog.String("error", shortText(err.Error())))
	}
	if err := c.w.Close(); err != nil && failure == nil {
		failure = err
	}
	c.emitStats()
	if failure != nil {
		c.out.emit(event{Type: evFailed, Reason: shortText(failure.Error())})
		return ChildExitFailed
	}
	c.out.emit(event{Type: evStopped, Frames: c.frames.Load()})
	return ChildExitOK
}

// capture reads packets until the stream ends. It returns nil after a stop and the cause when the capture or the
// chunk writing failed.
func (c *child) capture(runCtx context.Context, stream Stream) error {
	readCtx, cancelRead := context.WithCancel(context.WithoutCancel(runCtx))
	defer cancelRead()

	var (
		mu       sync.Mutex
		drain    *time.Timer
		finished bool
	)
	stopper := context.AfterFunc(runCtx, func() {
		stream.Stop()
		// The drained packets come next; a stream that never reports io.EOF is cut off.
		mu.Lock()
		defer mu.Unlock()
		if !finished {
			drain = time.AfterFunc(c.cfg.DrainTimeout, cancelRead)
		}
	})
	defer func() {
		stopper()
		mu.Lock()
		defer mu.Unlock()
		finished = true
		if drain != nil {
			drain.Stop()
		}
	}()

	for {
		p, err := stream.Read(readCtx)
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case err != nil && runCtx.Err() != nil:
			return nil // stopped; the drain timed out
		case err != nil:
			return fmt.Errorf("capture ended: %w", err)
		}
		if err := c.write(p); err != nil {
			return err
		}
	}
}

// write stores a packet, after the lead silence (first packet) or the silence of a gap (discontinuity).
func (c *child) write(p Packet) error {
	if err := p.Format.Validate(); err != nil {
		return fmt.Errorf("source packet: %w", err)
	}
	if p.Frames < 0 || (!p.Silent && len(p.Data) != p.Frames*p.Format.BlockAlign()) {
		return fmt.Errorf("source packet: %d bytes for %d frames of %s", len(p.Data), p.Frames, p.Format)
	}
	if err := c.w.SetFormat(p.Format); err != nil {
		return err
	}
	rate := p.Format.SampleRate
	now, prev := c.cfg.Now(), c.last
	c.last = now

	var fill int64
	switch {
	case !c.started:
		c.started = true
		fill = framesFor(c.req.LeadSilence, rate)
	case p.Discontinuity:
		// The packet ends about when it arrives; the previous one ended at prev.
		if gap := now.Sub(prev) - durationOf(int64(p.Frames), rate); gap >= minGap {
			gap = min(gap, c.cfg.MaxGap)
			fill = framesFor(gap, rate)
			c.log.Info("filling a capture gap with silence", slog.Duration("gap", gap))
		}
	}
	if err := c.w.WriteSilence(fill); err != nil {
		return err
	}

	var err error
	if p.Silent {
		err = c.w.WriteSilence(int64(p.Frames))
		c.silent.Add(1)
	} else {
		err = c.w.Write(p.Data)
		// Counted for the silent-source advice (TASK-082): zeros that are not flagged silent are no sound either.
		c.mono = toMono(p.Format, p.Data, c.mono[:0])
		peak := samplePeak(c.mono)
		c.meter.add(peak)
		if values.ClampSoundLevel(peak).Heard() {
			c.audible.Add(1)
		}
	}
	if err != nil {
		return err
	}
	c.packets.Add(1)
	c.frames.Add(fill + int64(p.Frames))
	return nil
}

// heartbeat reports the counters every StatsInterval until done is closed.
func (c *child) heartbeat(done <-chan struct{}) {
	t := time.NewTicker(c.cfg.StatsInterval)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			c.emitStats()
		}
	}
}

func (c *child) emitStats() {
	var reopens uint64
	if s, ok := c.stream.Load().(Stream); ok {
		reopens = s.Reopens()
	}
	c.out.emit(event{Type: evStats, Packets: c.packets.Load(), SilentPackets: c.silent.Load(), AudiblePackets: c.audible.Load(),
		Frames: c.frames.Load(), Reopens: reopens})
}

// childObserver turns source reports into events.
type childObserver struct{ out *eventWriter }

func (o *childObserver) Attempt(a Attempt) {
	o.out.emit(event{Type: evAttempt, Mode: string(a.Mode), OK: a.OK, HResult: shortText(a.HResult), Error: shortText(a.Error),
		Device: shortText(a.Device)})
}

func (o *childObserver) Switch(s Switch) {
	o.out.emit(event{Type: evSwitch, From: string(s.From), To: string(s.To), Cause: string(s.Cause), Reason: shortText(s.Reason)})
}

// eventWriter writes event lines; it is safe for concurrent use. A failed write (the parent is gone) is dropped:
// the closed stdin stops the child.
type eventWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (e *eventWriter) emit(ev event) {
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	_, _ = e.w.Write(append(b, '\n'))
}
