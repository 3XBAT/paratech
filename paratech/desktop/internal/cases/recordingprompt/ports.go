package recordingprompt

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

// SettingsStore gives access to the user settings: whether automatic recording prompts are on (PRD sections 6.1 and
// 15, AC-02) and the remembered recording source (PRD section 6.3, TASK-059). Implemented by the sqlite adapter.
type SettingsStore interface {
	// Load returns the current settings, defaults applied (PRD section 15).
	Load(ctx context.Context) (*settings.Settings, error)

	// Save stores the settings with the version compare-and-swap of ADR-0004(d) (PRD section 15): a stale version
	// yields shared.ErrVersionConflict and nothing is written.
	Save(ctx context.Context, s *settings.Settings) error
}

// AudioApps lists the apps the user may record (PRD section 6.3, TASK-059, ADR-0002 Decision 1). Implemented by the
// audiocapture adapter (Windows; elsewhere it fails).
type AudioApps interface {
	// List returns the apps with a visible window in this Windows session, one per executable name, except this app
	// (PRD section 6.3). It returns soon after ctx ends, and within a few seconds anyway.
	List(ctx context.Context) ([]values.AudioApp, error)
}

// Popup is the non-modal recording popup (PRD section 6.1, AC-01). Implemented by the Wails shell; the popup page
// reads the current prompt from the binding. The window commands are quick and take no context.
type Popup interface {
	// ShowPopup shows the popup without activating it or the main window (PRD section 6.1, AC-01). It fails while the
	// shell does not run.
	ShowPopup() error

	// HidePopup hides the popup (PRD section 6.1).
	HidePopup() error
}

// Recordings starts the recording a prompt offers (PRD sections 6.1 and 6.2). Implemented in app over the recording
// session.
type Recordings interface {
	// Ready reports whether a recording may start now: the startup cleanups ran and the recovery has marked the
	// interrupted recordings (PRD section 6.2, ADR-0009). It is cheap and never blocks.
	Ready() bool

	// StartRecording starts recording source for the lesson, or without one when lesson is empty, and returns once
	// the capture runs (PRD section 6.2: STARTING -> RECORDING). An error wrapping recording.ErrCaptureUnavailable
	// means this build cannot capture; one wrapping limits.ErrRecordingInProgress means another recording runs; one
	// wrapping recording.ErrSourceNotRunning means the chosen app does not run.
	StartRecording(ctx context.Context, lesson shared.LessonID, source values.Source) (shared.RecordingID, error)
}

// Clock supplies the current time (ARCH-24, PRD section 6.1). Implemented by the clock adapter and by a fake.
type Clock interface {
	// Now returns the current time (PRD section 6.1).
	Now() time.Time
}

// Sleeper waits on the same time source as Clock (ARCH-24) between two checks of the prompt (PRD section 6.1).
type Sleeper interface {
	// Sleep blocks until d has passed and returns nil, or until ctx ends and returns its error (PRD section 6.1).
	Sleep(ctx context.Context, d time.Duration) error
}
