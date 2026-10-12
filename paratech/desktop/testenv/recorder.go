package testenv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

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
// the Storage Manager as <dir>/chunk-NNNN, and Finalize concatenates the chunks into the output file.
type FakeRecorder struct {
	files ArtifactFiles
	chunk time.Duration // audio length of one chunk, for the duration that Finalize reports

	mu           sync.Mutex
	captures     map[shared.RecordingID]*fakeCapture
	startErr     error
	stopErr      error
	finalizeErr  error
	startHold    *hold // when set, Start waits at it
	finalizeHold *hold // when set, Finalize waits at it
	onStarted    func(id shared.RecordingID)
	starts       int
	finalizes    int
}

type fakeCapture struct {
	dir     shared.ArtifactRef
	next    int
	running bool
	ended   chan struct{}
	err     error
}

// NewFakeRecorder returns a recorder that writes through files; each chunk stands for chunk of audio.
func NewFakeRecorder(files ArtifactFiles, chunk time.Duration) *FakeRecorder {
	return &FakeRecorder{files: files, chunk: chunk, captures: map[shared.RecordingID]*fakeCapture{}}
}

// FailStart makes later Start calls fail with err; nil heals it.
func (f *FakeRecorder) FailStart(err error) { f.set(func() { f.startErr = err }) }

// FailStop makes later Stop calls fail with err; nil heals it.
func (f *FakeRecorder) FailStop(err error) { f.set(func() { f.stopErr = err }) }

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

// Start begins a capture into dir.
func (f *FakeRecorder) Start(ctx context.Context, id shared.RecordingID, dir shared.ArtifactRef) error {
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
	f.captures[id] = &fakeCapture{dir: dir, next: 1, running: true, ended: make(chan struct{})}
	hook := f.onStarted
	f.mu.Unlock()

	if hook != nil {
		hook(id)
	}
	return nil
}

// Feed writes data as the next chunk of the running capture of id. A failed write ends the capture with that error,
// as a real recorder that cannot write must stop.
func (f *FakeRecorder) Feed(ctx context.Context, id shared.RecordingID, data []byte) (shared.ArtifactRef, error) {
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

	if err := f.files.Write(ctx, ref, bytes.NewReader(data)); err != nil {
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

// Finalize concatenates the chunks of dir into output; the duration is one chunk length per chunk.
func (f *FakeRecorder) Finalize(ctx context.Context, dir, output shared.ArtifactRef) (time.Duration, error) {
	f.mu.Lock()
	f.finalizes++
	err, h := f.finalizeErr, f.finalizeHold
	f.mu.Unlock()
	if err != nil {
		return 0, err
	}
	if err := h.wait(ctx); err != nil {
		return 0, err
	}

	chunks, err := f.files.List(ctx, dir)
	if err != nil && !errors.Is(err, shared.ErrNotFound) {
		return 0, err
	}
	if len(chunks) == 0 {
		return 0, errors.New("fake recorder: no audio chunks")
	}

	var track bytes.Buffer
	for _, ref := range chunks {
		r, err := f.files.Open(ctx, ref)
		if err != nil {
			return 0, err
		}
		_, err = io.Copy(&track, r)
		_ = r.Close()
		if err != nil {
			return 0, err
		}
	}
	if err := f.files.Write(ctx, output, &track); err != nil {
		return 0, err
	}
	return time.Duration(len(chunks)) * f.chunk, nil
}

// Running reports whether the capture of id runs.
func (f *FakeRecorder) Running(id shared.RecordingID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.captures[id]
	return c != nil && c.running
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
