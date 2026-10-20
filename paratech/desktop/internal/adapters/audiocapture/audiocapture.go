package audiocapture

import (
	"context"
	"fmt"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// ErrUnavailable means this build cannot capture or assemble audio. It wraps recording.ErrCaptureUnavailable.
var ErrUnavailable = fmt.Errorf("audiocapture: %w in this build", recording.ErrCaptureUnavailable)

// Unavailable is the recorder of a build without audio capture. Starting a recording fails, and an interrupted
// recording is not assembled: Finalize returns a transient error (never recording.ErrNoAudio), so the recording keeps
// its chunks and the recovery of a later start with a real recorder assembles it. The zero value is ready to use.
type Unavailable struct{}

// Start refuses to capture.
func (Unavailable) Start(context.Context, shared.RecordingID, shared.ArtifactRef, values.Source) error {
	return ErrUnavailable
}

// Stop has no capture to stop.
func (Unavailable) Stop(context.Context, shared.RecordingID) error { return ErrUnavailable }

// Wait has no capture to wait for.
func (Unavailable) Wait(context.Context, shared.RecordingID) error { return ErrUnavailable }

// Switch has no capture to switch.
func (Unavailable) Switch(context.Context, shared.RecordingID, values.Source) error {
	return ErrUnavailable
}

// Diagnostics knows no capture.
func (Unavailable) Diagnostics(context.Context, shared.RecordingID) (values.CaptureDiagnostics, bool) {
	return values.CaptureDiagnostics{}, false
}

// Sound knows no capture.
func (Unavailable) Sound(context.Context, shared.RecordingID) (values.LiveSound, bool) {
	return values.LiveSound{}, false
}

// CheckLevels cannot capture anything.
func (Unavailable) CheckLevels(context.Context, values.Source, time.Duration, func(values.SoundLevel)) (values.SoundCause, error) {
	return "", ErrUnavailable
}

// Finalize cannot assemble the chunks; the recording stays for a later recovery.
func (Unavailable) Finalize(context.Context, shared.ArtifactRef, shared.ArtifactRef) (values.AssembledTrack, error) {
	return values.AssembledTrack{}, ErrUnavailable
}
