// Package wasapi is a minimal pure-Go WASAPI capture prototype (spike of TASK-015; no CGO, only x/sys,
// syscall and unsafe). COM interfaces are called through their vtables with syscall.SyscallN.
//
// Modes: process loopback INCLUDE of a process tree (the app the user picked), process loopback EXCLUDE of a
// process tree (whole system without our own sound) and classic loopback on the default render endpoint with a
// silent keep-alive render stream. OpenChain tries modes in order and reports every attempt.
//
// The portable files hold the pure logic (mode chain, WAVEFORMATEX codec, HRESULT errors, process tree root),
// tested on every OS; the COM code is in *_windows.go.
package wasapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrUnsupported means the capture cannot run on this platform (not Windows, or not a 64-bit build).
var ErrUnsupported = errors.New("wasapi: unsupported platform")

// Format describes the interleaved frames delivered to a Sink.
type Format struct {
	SampleRate    uint32
	Channels      uint16
	BitsPerSample uint16
	Float         bool
}

// BlockAlign is the size of one frame in bytes.
func (f Format) BlockAlign() int { return int(f.Channels) * int(f.BitsPerSample) / 8 }

func (f Format) String() string {
	kind := "PCM"
	if f.Float {
		kind = "float"
	}
	return fmt.Sprintf("%d Hz, %d ch, %d-bit %s", f.SampleRate, f.Channels, f.BitsPerSample, kind)
}

// Sink receives audio on the capture goroutine.
type Sink interface {
	// SetFormat is called after the stream started or was reopened, before the first Write in that format.
	SetFormat(f Format) error

	// Write gets whole frames in the current format; silent buffers arrive as zeros. p is valid only during the call.
	Write(p []byte) error
}

// Stats counts what the capture loop saw.
type Stats struct {
	Packets         uint64 // GetBuffer calls that returned frames
	SilentPackets   uint64 // packets flagged AUDCLNT_BUFFERFLAGS_SILENT (written as zeros)
	Frames          uint64
	Discontinuities uint64 // packets flagged AUDCLNT_BUFFERFLAGS_DATA_DISCONTINUITY
	EmptyWaits      uint64 // event waits that timed out without a packet
	Reopens         uint64
}

func (s Stats) String() string {
	return fmt.Sprintf("packets=%d silent=%d frames=%d discontinuities=%d empty_waits=%d reopens=%d",
		s.Packets, s.SilentPackets, s.Frames, s.Discontinuities, s.EmptyWaits, s.Reopens)
}

// Stream is a running capture.
type Stream interface {
	Spec() Spec
	Format() Format
	Stats() Stats

	// Done is closed when the capture goroutine has exited (after Stop or a failure).
	Done() <-chan struct{}

	// Err is nil after a Stop and the cause after the capture ended by itself. Valid after Done.
	Err() error

	// Stop asks the capture goroutine to exit and waits for it until ctx ends.
	Stop(ctx context.Context) error
}

// Opener starts a capture in the given mode.
type Opener func(ctx context.Context, s Spec, sink Sink) (Stream, error)

// Attempt is one try of OpenChain; Err is nil for the attempt that started.
type Attempt struct {
	Spec Spec
	Err  error
}

func (a Attempt) String() string {
	if a.Err == nil {
		return a.Spec.String() + ": ok"
	}
	var b strings.Builder
	b.WriteString(a.Spec.String())
	b.WriteString(": ")
	if hr, ok := HRESULTOf(a.Err); ok {
		fmt.Fprintf(&b, "hresult=%s ", hr)
	}
	b.WriteString(a.Err.Error())
	return b.String()
}

// OpenChain tries specs in order and returns the first stream that starts, together with every attempt. It
// stops early when ctx ends. When nothing starts, the error joins the errors of all attempts.
func OpenChain(ctx context.Context, open Opener, sink Sink, specs ...Spec) (Stream, []Attempt, error) {
	if len(specs) == 0 {
		return nil, nil, errors.New("wasapi: no capture mode given")
	}

	var (
		attempts []Attempt
		errs     []error
	)
	for _, s := range specs {
		if err := ctx.Err(); err != nil {
			return nil, attempts, errors.Join(append(errs, err)...)
		}

		st, err := open(ctx, s, sink)
		attempts = append(attempts, Attempt{Spec: s, Err: err})
		if err == nil {
			return st, attempts, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", s, err))
	}
	return nil, attempts, errors.Join(errs...)
}
