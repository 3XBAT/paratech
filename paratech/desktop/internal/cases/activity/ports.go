package activity

import (
	"context"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

// RecordingHistory lists stored recordings for the recordings screen (PRD section 6.2). Implemented by the sqlite
// adapter.
type RecordingHistory interface {
	// ListActivity returns every non-terminal recording plus at most maxTerminal of the newest terminal ones, newest
	// first by creation time, then id; maxTerminal below 1 adds no terminal recording. Rows that cannot be read are
	// left out and their ids returned in unreadable; only other failures are errors.
	ListActivity(ctx context.Context, maxTerminal int) (recs []*recording.Recording, unreadable []shared.RecordingID, err error)
}

// JobHistory lists stored jobs for the jobs screen (PRD section 7.2). Implemented by the sqlite adapter.
type JobHistory interface {
	// ListActivity returns every non-terminal job (so "Run now" of a waiting job stays reachable, AC-22) plus at most
	// maxTerminal of the newest terminal ones, newest first by creation time, then id; maxTerminal below 1 adds no
	// terminal job. Jobs of the hidden types are left out of both parts and do not count against maxTerminal. Rows
	// that cannot be read are left out and their ids returned in unreadable; only other failures are errors.
	ListActivity(ctx context.Context, maxTerminal int, hidden []job.Type) (jobs []*job.Job, unreadable []shared.JobID, err error)
}
