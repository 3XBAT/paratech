package soundcheck

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/settings/settings"
)

// LevelChecker captures source for d without recording and reports each level it hears (the recorder adapter, in a
// recorder child). end is how the capture ended: values.SoundOK after d, values.SoundBusy when a recording runs or
// started, or the cause of a capture that could not run. An app that does not run fails with an error wrapping
// recording.ErrSourceNotRunning, a build without capture with one wrapping recording.ErrCaptureUnavailable; when ctx
// ends it returns soon with an error. levels is called from the calling goroutine.
type LevelChecker interface {
	CheckLevels(ctx context.Context, source values.Source, d time.Duration,
		levels func(values.SoundLevel)) (end values.SoundCause, err error)
}

// SettingsReader reads the remembered recording source (TASK-059). Implemented by the sqlite adapter.
type SettingsReader interface {
	Load(ctx context.Context) (*settings.Settings, error)
}

// Recordings tells whether a recording is in progress (the recording session).
type Recordings interface {
	Recording() bool
}

// Clock tells the time of the session's progress (ARCH-24).
type Clock interface {
	Now() time.Time
}
