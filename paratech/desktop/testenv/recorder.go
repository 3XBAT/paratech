package testenv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// ErrNotCapturing is returned by FakeRecorder for a recording without a running capture.
var ErrNotCapturing = errors.New("fake recorder: no running capture")

// ArtifactFiles is the part of the Storage Manager the fake recorder writes and reads chunks with. *MemArtifacts and
// the filestore adapter satisfy it.
type ArtifactFiles interface {
	Write(ctx context.Context, ref shared.ArtifactRef, content io.Reader) error
	Open(ctx context.Context, ref shared.ArtifactRef) (io.ReadCloser, error)
	List(ctx context.Context, dir shared.ArtifactRef) ([]shared.ArtifactRef, error)
}

// FakeRecorder is a Recorder whose audio source is the test: Feed writes the next chunk of a running capture through
// the Storage Manager as <dir>/chunk-NNNN (format of EncodeChunk), and Finalize concatenates the payloads of the
// readable chunks into the output file.
type FakeRecorder struct {
	files ArtifactFiles
	chunk time.Duration // audio length of one chunk, for the duration that Finalize reports

	mu           sync.Mutex
	captures     map[shared.RecordingID]*fakeCapture
	startErr     error
	stopErr      error
	finalizeErr  error
	switchErr    error
	startHold    *hold // when set, Start waits at it
	finalizeHold *hold // when set, Finalize waits at it
	onStarted    func(id shared.RecordingID)
	starts       int
	finalizes    int
	levels       []values.SoundLevel
	levelsSet    bool
	levelEnd     values.SoundCause
	levelErr     error
	levelHold    *hold // when set, CheckLevels waits at it after reporting the levels
	soundChecks  int
}

type fakeCapture struct {
	dir      shared.ArtifactRef
	source   values.Source
	next     int
	running  bool
	ended    chan struct{}
	err      error
	switches []values.CaptureSwitch
	stats    values.CaptureStats
	sourceSw int // switches of the user (Switch)
}

// NewFakeRecorder returns a recorder that writes through files; each chunk stands for chunk of audio.
func NewFakeRecorder(files ArtifactFiles, chunk time.Duration) *FakeRecorder {
	return &FakeRecorder{files: files, chunk: chunk, captures: map[shared.RecordingID]*fakeCapture{}}
}

// FailStart makes later Start calls fail with err; nil heals it.
func (f *FakeRecorder) FailStart(err error) { f.set(func() { f.startErr = err }) }

// FailStop makes later Stop calls fail with err; nil heals it.
func (f *FakeRecorder) FailStop(err error) { f.set(func() { f.stopErr = err }) }

// FailSwitch makes later Switch calls fail with err; nil heals it.
func (f *FakeRecorder) FailSwitch(err error) { f.set(func() { f.switchErr = err }) }

// FailFinalize makes later Finalize calls fail with err; nil heals it.
func (f *FakeRecorder) FailFinalize(err error) { f.set(func() { f.finalizeErr = err }) }

// OnStarted registers a hook that runs inside Start after the capture runs, just before Start returns.
func (f *FakeRecorder) OnStarted(hook func(id shared.RecordingID)) {
	f.set(func() { f.onStarted = hook })
}

// HoldStart makes later Start calls wait until release is called or their context ends. entered is closed when the
// first one waits.
func (f *FakeRecorder) HoldStart() (entered <-chan struct{}, release func()) {
	h := newHold()
	f.set(func() { f.startHold = h })
	return h.entered, h.release
}

// HoldFinalize makes later Finalize calls wait until release is called or their context ends. entered is closed when
// the first one waits.
func (f *FakeRecorder) HoldFinalize() (entered <-chan struct{}, release func()) {
	h := newHold()
	f.set(func() { f.finalizeHold = h })
	return h.entered, h.release
}

// hold makes calls wait until it is released or their context ends.
type hold struct {
	gate, entered      chan struct{}
	enterOnce, relOnce sync.Once
}

func newHold() *hold { return &hold{gate: make(chan struct{}), entered: make(chan struct{})} }

func (h *hold) release() { h.relOnce.Do(func() { close(h.gate) }) }

func (h *hold) wait(ctx context.Context) error {
	if h == nil {
		return nil
	}
	h.enterOnce.Do(func() { close(h.entered) })
	select {
	case <-h.gate:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func (f *FakeRecorder) set(change func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change()
}

// Start begins a capture of source into dir; Source tells which source it was.
func (f *FakeRecorder) Start(ctx context.Context, id shared.RecordingID, dir shared.ArtifactRef, source values.Source) error {
	f.mu.Lock()
	f.starts++
	err, h := f.startErr, f.startHold
	f.mu.Unlock()
	if err != nil {
		return err
	}
	if err := h.wait(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	f.mu.Lock()
	if c := f.captures[id]; c != nil && c.running {
		f.mu.Unlock()
		return fmt.Errorf("fake recorder: %s is already capturing", id)
	}
	f.captures[id] = &fakeCapture{dir: dir, source: source, next: 1, running: true, ended: make(chan struct{})}
	hook := f.onStarted
	f.mu.Unlock()

	if hook != nil {
		hook(id)
	}
	return nil
}

// Feed writes data as the next chunk of the running capture of id, in the format of EncodeChunk. A failed write ends
// the capture with that error, as a real recorder that cannot write must stop.
func (f *FakeRecorder) Feed(ctx context.Context, id shared.RecordingID, data []byte) (shared.ArtifactRef, error) {
	return f.feed(ctx, id, EncodeChunk(data))
}

// FeedTruncated writes data as the next chunk of the running capture of id but cuts the file short, as a crash in the
// middle of a write leaves the last chunk; Finalize skips such a chunk.
func (f *FakeRecorder) FeedTruncated(ctx context.Context, id shared.RecordingID, data []byte) (shared.ArtifactRef, error) {
	content := EncodeChunk(data)
	return f.feed(ctx, id, content[:len(content)-1])
}

func (f *FakeRecorder) feed(ctx context.Context, id shared.RecordingID, content []byte) (shared.ArtifactRef, error) {
	f.mu.Lock()
	c := f.captures[id]
	if c == nil || !c.running {
		f.mu.Unlock()
		return shared.ArtifactRef{}, fmt.Errorf("%w: %s", ErrNotCapturing, id)
	}
	ref, err := c.dir.Child(fmt.Sprintf("chunk-%04d", c.next))
	c.next++
	f.mu.Unlock()
	if err != nil {
		return shared.ArtifactRef{}, err
	}

	if err := f.files.Write(ctx, ref, bytes.NewReader(content)); err != nil {
		f.End(id, err)
		return shared.ArtifactRef{}, err
	}
	return ref, nil
}

// End ends the capture of id by itself, as a lost output device would; Wait returns err, or a generic error for nil.
func (f *FakeRecorder) End(id shared.RecordingID, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c := f.captures[id]; c != nil && c.running {
		c.running, c.err = false, err
		if c.err == nil {
			c.err = errors.New("fake recorder: capture ended")
		}
		close(c.ended)
	}
}

// Stop ends the capture of id; Wait then returns nil.
func (f *FakeRecorder) Stop(_ context.Context, id shared.RecordingID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopErr != nil {
		return f.stopErr
	}
	c := f.captures[id]
	if c == nil || !c.running {
		return fmt.Errorf("%w: %s", ErrNotCapturing, id)
	}
	c.running = false
	close(c.ended)
	return nil
}

// Wait blocks until the capture of id has ended.
func (f *FakeRecorder) Wait(ctx context.Context, id shared.RecordingID) error {
	f.mu.Lock()
	c := f.captures[id]
	f.mu.Unlock()
	if c == nil {
		return fmt.Errorf("%w: %s", ErrNotCapturing, id)
	}
	select {
	case <-c.ended:
		f.mu.Lock()
		defer f.mu.Unlock()
		return c.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Finalize concatenates the payloads of the readable chunks of dir into output, in name order, and skips the chunks
// that are not complete (Recorder contract). The duration is one chunk length per readable chunk; its seconds are
// audible unless the payload of the chunk is empty or only zero bytes (a silent chunk, TASK-082). Without a readable
// chunk it returns an error wrapping recording.ErrNoAudio and writes nothing.
func (f *FakeRecorder) Finalize(ctx context.Context, dir, output shared.ArtifactRef) (values.AssembledTrack, error) {
	f.mu.Lock()
	f.finalizes++
	err, h := f.finalizeErr, f.finalizeHold
	f.mu.Unlock()
	if err != nil {
		return values.AssembledTrack{}, err
	}
	if err := h.wait(ctx); err != nil {
		return values.AssembledTrack{}, err
	}

	chunks, err := f.files.List(ctx, dir)
	if err != nil && !errors.Is(err, shared.ErrNotFound) {
		return values.AssembledTrack{}, err
	}

	var track bytes.Buffer
	readable, audible := 0, 0
	for _, ref := range chunks {
		content, err := f.read(ctx, ref)
		if err != nil {
			return values.AssembledTrack{}, err
		}
		if payload, ok := ChunkPayload(content); ok {
			track.Write(payload)
			readable++
			if slices.ContainsFunc(payload, func(b byte) bool { return b != 0 }) {
				audible++
			}
		}
	}
	if readable == 0 {
		return values.AssembledTrack{}, fmt.Errorf("fake recorder: %w in %s", recording.ErrNoAudio, dir)
	}
	if err := f.files.Write(ctx, output, &track); err != nil {
		return values.AssembledTrack{}, err
	}
	perChunk := int((f.chunk + time.Second - 1) / time.Second)
	return values.AssembledTrack{
		Duration: time.Duration(readable) * f.chunk, Seconds: readable * perChunk, AudibleSeconds: audible * perChunk,
	}, nil
}

// Switch makes the running capture of id record source from now on, as the Windows recorder does (TASK-082); it
// fails as FailSwitch says, or with ErrNotCapturing when id does not capture.
func (f *FakeRecorder) Switch(_ context.Context, id shared.RecordingID, source values.Source) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.switchErr != nil {
		return f.switchErr
	}
	c := f.captures[id]
	if c == nil || !c.running {
		return fmt.Errorf("%w: %s", ErrNotCapturing, id)
	}
	if c.source != source {
		c.source = source
		c.sourceSw++
	}
	return nil
}

// SetStats makes the Diagnostics of the capture of id report stats, as the heartbeats of a recorder child do.
func (f *FakeRecorder) SetStats(id shared.RecordingID, stats values.CaptureStats) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.captures[id]
	if c == nil {
		return fmt.Errorf("%w: %s", ErrNotCapturing, id)
	}
	c.stats = stats
	return nil
}

func (f *FakeRecorder) read(ctx context.Context, ref shared.ArtifactRef) ([]byte, error) {
	r, err := f.files.Open(ctx, ref)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	return io.ReadAll(r)
}

// SwitchMode makes the capture of id report sw, as the recorder child does when process loopback fails to activate,
// fails while running or is healthy but mute (ADR-0002 Decision 3): Diagnostics then show the switch and its mode.
// The capture goes on.
func (f *FakeRecorder) SwitchMode(id shared.RecordingID, sw values.CaptureSwitch) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.captures[id]
	if c == nil || !c.running {
		return fmt.Errorf("%w: %s", ErrNotCapturing, id)
	}
	c.switches = append(c.switches, sw)
	return nil
}

// Diagnostics returns what the capture of id reports: include for an app, exclude for the whole computer, system
// after a switch; ok is false when id was never started.
func (f *FakeRecorder) Diagnostics(_ context.Context, id shared.RecordingID) (values.CaptureDiagnostics, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.captures[id]
	if c == nil {
		return values.CaptureDiagnostics{}, false
	}
	target := values.CaptureExclude
	if !c.source.IsWholeComputer() {
		target = values.CaptureInclude
	}
	d := values.CaptureDiagnostics{
		Running: c.running, TargetMode: target, App: c.source.App(), Mode: target, Windows: ToneWindows(), Device: ToneDevice,
		Switches: append([]values.CaptureSwitch(nil), c.switches...), Stats: c.stats, SourceSwitches: c.sourceSw,
	}
	if n := len(c.switches); n > 0 {
		d.Mode = c.switches[n-1].To
	}
	return d, true
}

// SetLevels makes later CheckLevels calls report levels in order and then answer end and err. By default they
// report one level of 0.5 and end ok.
func (f *FakeRecorder) SetLevels(levels []values.SoundLevel, end values.SoundCause, err error) {
	f.set(func() {
		f.levels, f.levelsSet, f.levelEnd, f.levelErr = append([]values.SoundLevel(nil), levels...), true, end, err
	})
}

// HoldLevels makes later CheckLevels calls wait after their levels until release is called or their context ends
// (then they return its cause, as the real recorder does). entered is closed when the first one waits.
func (f *FakeRecorder) HoldLevels() (entered <-chan struct{}, release func()) {
	h := newHold()
	f.set(func() { f.levelHold = h })
	return h.entered, h.release
}

// CheckLevels answers the sound check as SetLevels says; SoundBusy while a capture runs, as the real recorder.
func (f *FakeRecorder) CheckLevels(ctx context.Context, _ values.Source, _ time.Duration,
	report func(values.SoundLevel)) (values.SoundCause, error) {
	f.mu.Lock()
	f.soundChecks++
	for _, c := range f.captures {
		if c.running {
			f.mu.Unlock()
			return values.SoundBusy, nil
		}
	}
	levels, end, err, h := []values.SoundLevel{0.5}, values.SoundOK, error(nil), f.levelHold
	if f.levelsSet {
		levels, end, err = f.levels, f.levelEnd, f.levelErr
	}
	f.mu.Unlock()

	for _, l := range levels {
		if report != nil {
			report(l)
		}
	}
	if err := h.wait(ctx); err != nil {
		return "", err
	}
	return end, err
}

// SoundChecks returns how many times CheckLevels was called.
func (f *FakeRecorder) SoundChecks() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.soundChecks
}

// RecentCaptures returns the diagnostics of every capture, in no particular order.
func (f *FakeRecorder) RecentCaptures(ctx context.Context) []values.CaptureDiagnostics {
	f.mu.Lock()
	ids := make([]shared.RecordingID, 0, len(f.captures))
	for id := range f.captures {
		ids = append(ids, id)
	}
	f.mu.Unlock()
	out := make([]values.CaptureDiagnostics, 0, len(ids))
	for _, id := range ids {
		if d, ok := f.Diagnostics(ctx, id); ok {
			out = append(out, d)
		}
	}
	return out
}

// Running reports whether the capture of id runs.
func (f *FakeRecorder) Running(id shared.RecordingID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.captures[id]
	return c != nil && c.running
}

// Source returns the source of the last capture of id; ok is false when id was never started.
func (f *FakeRecorder) Source(id shared.RecordingID) (source values.Source, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.captures[id]
	if c == nil {
		return values.Source{}, false
	}
	return c.source, true
}

// Starts returns how many times Start was called.
func (f *FakeRecorder) Starts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts
}

// Finalizes returns how many times Finalize was called.
func (f *FakeRecorder) Finalizes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.finalizes
}
