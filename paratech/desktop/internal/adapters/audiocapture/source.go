package audiocapture

import (
	"context"
	"errors"
	"fmt"
	"time"

	"paratech/desktop/internal/domain/recording/values"
)

// Mode selects what a capture records (ADR-0002 Decision 1): the domain capture mode.
type Mode = values.CaptureMode

// Capture modes.
const (
	// ModeInclude is process loopback of a process tree: the lecture app the user picked.
	ModeInclude = values.CaptureInclude

	// ModeExclude is process loopback of everything except a process tree (our own app): the whole computer.
	ModeExclude = values.CaptureExclude

	// ModeSystem is classic loopback of the default output device: the fallback of both process loopback modes.
	ModeSystem = values.CaptureSystem
)

// ErrInvalidTarget means a capture target cannot be used as given.
var ErrInvalidTarget = errors.New("audiocapture: invalid capture target")

// Target is what to capture: a mode and, for process loopback, the root PID of the process tree.
type Target struct {
	Mode Mode
	PID  uint32
}

// Validate checks that process loopback has a target process and classic loopback has none.
func (t Target) Validate() error {
	switch t.Mode {
	case ModeInclude, ModeExclude:
		if t.PID == 0 {
			return fmt.Errorf("%w: %s needs a process", ErrInvalidTarget, t.Mode)
		}
	case ModeSystem:
		if t.PID != 0 {
			return fmt.Errorf("%w: system loopback takes no process", ErrInvalidTarget)
		}
	default:
		return fmt.Errorf("%w: unknown mode %q", ErrInvalidTarget, t.Mode)
	}
	return nil
}

// Source opens the platform capture inside the recorder child. The Windows implementation (WASAPI, ADR-0002) tries
// the fallback chain itself: process loopback of the target, then classic loopback; a ModeSystem target opens only
// classic loopback (a recording that fell back restarts in system mode).
type Source interface {
	// Open starts capturing target and returns the running stream. It reports every activation try through obs and
	// returns when a mode runs, or with an error when none does (one wrapping recording.ErrCaptureUnavailable if this
	// build has no capture). It returns soon after ctx ends.
	Open(ctx context.Context, target Target, obs Observer) (Stream, error)
}

// SourceConfig tunes the platform Source (PlatformSource); DefaultSourceConfig gives the production values.
type SourceConfig struct {
	// MuteThreshold is how long a process loopback stream may deliver only zeros while the Windows meters show sound on
	// its target before it falls back to classic loopback (healthy but mute, ADR-0002 Decision 3). Zero takes
	// DefaultMuteThreshold; a negative value turns the detector off.
	MuteThreshold time.Duration
}

// DefaultSourceConfig returns the 15 s threshold of the healthy-but-mute detector.
func DefaultSourceConfig() SourceConfig { return SourceConfig{MuteThreshold: DefaultMuteThreshold} }

// muteAfter is the threshold the stream uses; 0 means off.
func (c SourceConfig) muteAfter() time.Duration {
	switch {
	case c.MuteThreshold < 0:
		return 0
	case c.MuteThreshold == 0:
		return DefaultMuteThreshold
	}
	return c.MuteThreshold
}

// Observer receives what a Source does, for the diagnostics (TASK-058). Its methods are safe for concurrent use.
type Observer interface {
	// Attempt reports one activation try (ok or failed with its HRESULT).
	Attempt(a Attempt)

	// Switch reports a mode change of a running stream, for example process loopback that only returns zeros
	// falling back to system mode. The packets that follow carry the new format.
	Switch(s Switch)
}

// Attempt is one activation try of a capture mode (the domain value): its HRESULT on a failure, the output device.
type Attempt = values.CaptureAttempt

// Switch is a mode change of a running capture (the domain value), with its cause.
type Switch = values.CaptureSwitch

// StreamInfo describes a running stream.
type StreamInfo struct {
	Mode    Mode
	Format  Format
	Windows values.WindowsVersion // the Windows the stream runs on
	Device  string                // friendly name of the default output device; "" when unknown
}

// Packet is a piece of captured audio in Format.
type Packet struct {
	Format Format
	Frames int

	// Data holds Frames interleaved frames; it is ignored when Silent and valid only until the next Read.
	Data []byte

	// Silent marks a packet the device flagged as silence (AUDCLNT_BUFFERFLAGS_SILENT): Frames of zeros are written.
	Silent bool

	// Discontinuity marks a packet after a gap (AUDCLNT_BUFFERFLAGS_DATA_DISCONTINUITY, a reopened device): the child
	// fills the missing time with silence, measured on the monotonic clock.
	Discontinuity bool
}

// Stream is a running capture. Read is called from one goroutine; Stop and Close may be called from another.
type Stream interface {
	// Info describes the stream now (the mode and format change after a Switch).
	Info() StreamInfo

	// Read blocks until the next packet. After Stop it returns the packets still buffered (the drained endpoint
	// buffer), then io.EOF. Another error means the capture ended by itself (device lost and reopening failed).
	// It returns soon after ctx ends.
	Read(ctx context.Context) (Packet, error)

	// Reopens counts how often the device was reopened (for example after a default device change).
	Reopens() uint64

	// Stop ends the capture; it is safe to call more than once and concurrently with Read.
	Stop()

	// Close releases the device and every resource of the stream; Read must have returned.
	Close() error
}
