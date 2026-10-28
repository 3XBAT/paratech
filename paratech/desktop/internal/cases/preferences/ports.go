package preferences

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/settings/settings"
)

// SettingsRepository stores the settings singleton (PRD sections 6.1 and 7.4). Implemented by the sqlite adapter.
type SettingsRepository interface {
	// Load returns the stored settings, or the defaults with Version() == 0 while nothing is stored.
	Load(ctx context.Context) (*settings.Settings, error)

	// Save is the version compare-and-swap of ADR-0004(d): a stale version yields shared.ErrVersionConflict. On
	// success the adapter calls Persisted on the aggregate.
	Save(ctx context.Context, s *settings.Settings) error
}

// RecognitionLanguages tells whether the photo recognition engine can read Russian (TASK-294). Implemented in app over
// the winocr adapter; it only reads what Windows has installed.
type RecognitionLanguages interface {
	// RussianAvailable reports whether a Russian recognizer is installed. An error means it is not known: another OS,
	// a Windows without OCR, or a failure.
	RussianAvailable(ctx context.Context) (bool, error)
}

// Clock supplies the change time of a setting (ARCH-24). Implemented by the clock adapter and by a fake in tests.
type Clock interface {
	Now() time.Time
}
