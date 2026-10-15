package notify

import (
	"log/slog"

	"paratech/desktop/internal/domain/jobs/job"
)

// Deps are the ports the Service needs.
type Deps struct {
	Settings SettingsReader
	Notices  NoticeRepository
	Notifier Notifier
	Clock    Clock
	Log      *slog.Logger // optional: nil discards
}

// Config tunes the Service.
type Config struct {
	// Types are the job types whose finish notifies the user, at least one. The app passes the user-visible
	// processing steps (transcription, summary, homework extraction, homework draft); email delivery has its own UI.
	Types []job.Type
}
