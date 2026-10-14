package activity

import (
	"context"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
)

// RecordingHistory lists stored recordings for the recordings screen (PRD section 6.2). Implemented by the sqlite
// adapter.
type RecordingHistory interface {
	// ListRecent returns at most limit recordings, newest first (PRD section 6.2); a limit below 1 lists nothing.
	ListRecent(ctx context.Context, limit int) ([]*recording.Recording, error)
}

// JobHistory lists stored jobs for the jobs screen (PRD section 7.2). Implemented by the sqlite adapter.
type JobHistory interface {
	// ListRecent returns at most limit jobs, newest first (PRD section 7.2); a limit below 1 lists nothing.
	ListRecent(ctx context.Context, limit int) ([]*job.Job, error)
}
