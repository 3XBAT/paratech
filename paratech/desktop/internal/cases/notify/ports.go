package notify

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/jobs/notice"
	"paratech/desktop/internal/domain/settings/settings"
)

// SettingsReader gives read access to the user settings: the "notify on processing complete" switch (PRD section 15).
// Implemented by the sqlite adapter.
type SettingsReader interface {
	// Load returns the current settings, defaults applied (PRD section 15).
	Load(ctx context.Context) (*settings.Settings, error)
}

// NoticeRepository records which job states the user was notified about (PRD section 15: no repeated notifications
// for the same job state). Implemented by the sqlite adapter.
type NoticeRepository interface {
	// Record stores n unless a notice for the same job and status exists (PRD section 15). first is true only for the
	// call that stored it, also when calls race or come after a restart; only that caller notifies.
	Record(ctx context.Context, n *notice.Notice) (first bool, err error)
}

// Notifier shows the notification to the user (PRD section 15). Implemented by the Wails shell on Windows (toasts
// through the Windows notification platform, which applies the user's Windows notification settings) and by a fake.
type Notifier interface {
	// Notify shows a notification about n with fixed texts for its job type and status, never user content
	// (PRD section 15). It must honour ctx.
	Notify(ctx context.Context, n *notice.Notice) error
}

// Clock supplies the time of a notice (ARCH-24, PRD section 15). Implemented by the clock adapter and by a fake.
type Clock interface {
	// Now returns the current time (PRD section 15).
	Now() time.Time
}
