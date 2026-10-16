package soundcheck

import (
	"context"

	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/settings/settings"
)

// SoundChecker plays the tone and captures it (the recorder adapter, in a recorder child). Its result is Busy when a
// capture of a recording runs; a build without capture fails with an error wrapping recording.ErrCaptureUnavailable.
type SoundChecker interface {
	CheckSound(ctx context.Context) (values.SoundCheck, error)
}

// SettingsReader reads the remembered recording source (TASK-059), which tells which mode decides. Implemented by the
// sqlite adapter.
type SettingsReader interface {
	Load(ctx context.Context) (*settings.Settings, error)
}

// Recordings tells whether a recording is in progress (the recording session).
type Recordings interface {
	Recording() bool
}
