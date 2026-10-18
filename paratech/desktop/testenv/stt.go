package testenv

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
)

// STTCall is one call of FakeSTT.Transcribe.
type STTCall struct {
	Audio, Model, Output shared.ArtifactRef
}

// FakeSTT is an STT engine that reports a fixed progress sequence and writes a fixed document, like the whispercli
// adapter: only on success and in one write.
type FakeSTT struct {
	out interface {
		Write(ctx context.Context, ref shared.ArtifactRef, content io.Reader) error
	}

	mu       sync.Mutex
	doc      transcript.Document
	segments []transcript.Segment
	progress []int
	err      error
	block    bool
	calls    []STTCall
}

// NewFakeSTT returns an engine that writes doc to out, reporting 0, 50 and 100.
func NewFakeSTT(out interface {
	Write(ctx context.Context, ref shared.ArtifactRef, content io.Reader) error
}, doc transcript.Document) *FakeSTT {
	return &FakeSTT{out: out, doc: doc, progress: []int{0, 50, 100}}
}

// SetProgress replaces the reported percentages.
func (f *FakeSTT) SetProgress(p ...int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.progress = append([]int(nil), p...)
}

// SetSegments replaces the live segments reported before the progress (none by default).
func (f *FakeSTT) SetSegments(s ...transcript.Segment) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.segments = append([]transcript.Segment(nil), s...)
}

// FailWith makes later calls fail with err after reporting progress; nil heals the engine.
func (f *FakeSTT) FailWith(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// Block makes later calls wait for their context after reporting progress, like a long recognition.
func (f *FakeSTT) Block(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.block = on
}

// Calls returns the calls so far.
func (f *FakeSTT) Calls() []STTCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]STTCall(nil), f.calls...)
}

// Transcribe follows the STTEngine port: live segments, progress, then the document or the configured error. A
// cancelled context stops it between reports, without output.
func (f *FakeSTT) Transcribe(ctx context.Context, audio, model, output shared.ArtifactRef, progress func(percent int),
	segment func(transcript.Segment)) error {
	f.mu.Lock()
	f.calls = append(f.calls, STTCall{Audio: audio, Model: model, Output: output})
	steps, err, block, doc := append([]int(nil), f.progress...), f.err, f.block, f.doc
	segs := append([]transcript.Segment(nil), f.segments...)
	f.mu.Unlock()

	for _, s := range segs {
		if ctx.Err() != nil {
			break
		}
		if segment != nil {
			segment(s)
		}
	}

	for _, p := range steps {
		if ctx.Err() != nil {
			break
		}
		if progress != nil {
			progress(p)
		}
	}
	if block {
		<-ctx.Done()
	}
	if ctx.Err() != nil {
		return fmt.Errorf("fake stt: stopped: %w", ctx.Err())
	}
	if err != nil {
		return err
	}

	data, err := doc.Encode()
	if err != nil {
		return err
	}
	return f.out.Write(ctx, output, bytes.NewReader(data))
}
