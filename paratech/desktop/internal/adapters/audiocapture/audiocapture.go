// Package audiocapture captures system audio into chunk files (PRD sections 6.2 and 6.3); it satisfies the Recorder
// port of the recording session structurally. This build has no capture yet (the WASAPI loopback recorder comes with
// TASK-017): Unavailable refuses every call.
package audiocapture

import (
	"context"
	"errors"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// ErrUnavailable means this build cannot capture or assemble audio.
var ErrUnavailable = errors.New("audiocapture: system audio recorder is not available in this build")

// Unavailable is the recorder of a build without audio capture. Starting a recording fails, and an interrupted
// recording is not assembled: Finalize returns a transient error (never recording.ErrNoAudio), so the recording keeps
// its chunks and the recovery of a later start with a real recorder assembles it. The zero value is ready to use.
type Unavailable struct{}

// Start refuses to capture.
func (Unavailable) Start(context.Context, shared.RecordingID, shared.ArtifactRef) error {
	return ErrUnavailable
}

// Stop has no capture to stop.
func (Unavailable) Stop(context.Context, shared.RecordingID) error { return ErrUnavailable }

// Wait has no capture to wait for.
func (Unavailable) Wait(context.Context, shared.RecordingID) error { return ErrUnavailable }

// Finalize cannot assemble the chunks; the recording stays for a later recovery.
func (Unavailable) Finalize(context.Context, shared.ArtifactRef, shared.ArtifactRef) (time.Duration, error) {
	return 0, ErrUnavailable
}
