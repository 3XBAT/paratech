package app

// Stand-ins for the ports that this build cannot configure. Each one fails with a fixed text that wraps
// errNotConfigured and with the error class that keeps the jobs safe.

import (
	"context"
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
)

// errNotConfigured marks a call into a part of the core that this build does not configure.
var errNotConfigured = errors.New("not configured in this build")

// sttNotConfigured is the speech recognition engine of a build whose whisper-cli cannot be used (missing next to the
// executable, or refused by the adapter's checks; the log says why). An STT job fails at once instead of burning CPU
// on retries.
type sttNotConfigured struct{}

func (sttNotConfigured) Transcribe(context.Context, shared.ArtifactRef, shared.ArtifactRef, shared.ArtifactRef, func(int)) error {
	return backoff.Permanent(fmt.Errorf("speech recognition: %w", errNotConfigured))
}
