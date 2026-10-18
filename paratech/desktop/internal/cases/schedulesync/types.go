package schedulesync

import (
	"context"
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/schedule/services/importplan"
	"paratech/desktop/internal/domain/shared"
)

// Deps are the ports the Service needs. Lessons, Parsers (at least one entry), Clock, IDs and Tx are required.
type Deps struct {
	Lessons LessonRepository
	Parsers map[importplan.Source]ScheduleParser
	Clock   Clock
	IDs     IDGenerator
	Tx      Tx

	// Sync, if set, is asked for a calendar synchronization after a commit that changed the schedule (the
	// CalendarSync of the app). Log is optional.
	Sync SyncRequester
	Log  *slog.Logger
}

// SyncRequester asks for a calendar synchronization; *CalendarSync implements it.
type SyncRequester interface {
	RequestSync(ctx context.Context) (SyncRequest, error)
}

// ImportRequest is a file to preview. Data is the whole file as read by the input layer.
type ImportRequest struct {
	Source importplan.Source
	Data   []byte
}

// Preview is what the user sees before the commit: errors, or the added, changed, unchanged and removed lessons
// and the conflicts. ConfirmToken is empty when the plan has errors.
type Preview struct {
	Plan         importplan.Plan
	ConfirmToken string
}

// CommitRequest commits a previewed file. ConfirmToken is the token of the preview the user confirmed, and Data the
// same bytes.
type CommitRequest struct {
	Source       importplan.Source
	Data         []byte
	ConfirmToken string
}

// CommitResult counts what the commit wrote.
type CommitResult struct {
	Added   int
	Changed int
	Removed int
}

// CalendarSyncJobType is the job type of the calendar synchronization (PRD section 11.3).
const CalendarSyncJobType = job.Type("calendar_sync")

// CalendarDeps are the ports of CalendarSync; all but Log are required. MaxAttempts bounds the attempts of a sync job;
// 0 takes the backoff default.
type CalendarDeps struct {
	Lessons  LessonRepository
	Calendar CalendarAdapter
	Account  CalendarAccount
	Jobs     JobEnqueuer
	Clock    Clock
	IDs      IDGenerator
	Tx       Tx
	Log      *slog.Logger

	MaxAttempts int
}

// SyncRequest is the answer to RequestSync: the job that will run, and whether this request created it (false: a
// sync job that has not started yet covers the request).
type SyncRequest struct {
	JobID   shared.JobID
	Created bool
}

// SyncOutcome is how the last sync run of this process ended.
type SyncOutcome string

// Outcomes of a sync run. The zero value means no run ended since the start.
const (
	OutcomeNone          SyncOutcome = ""
	OutcomeOK            SyncOutcome = "ok"
	OutcomeNotSignedIn   SyncOutcome = "notSignedIn"
	OutcomeSignInExpired SyncOutcome = "signInExpired"
	OutcomeNoAccess      SyncOutcome = "noAccess"
	OutcomeFailed        SyncOutcome = "failed"
)

// CalendarStatus is the calendar synchronization as the page shows it.
type CalendarStatus struct {
	SignedIn bool

	// Pending counts the lessons the calendar does not show as they are (deleted ones included), at most
	// MaxPendingShown; PendingMore says there are more.
	Pending     int
	PendingMore bool

	// LastWrittenAt is the last time a live lesson was written to the calendar; zero if none was.
	LastWrittenAt time.Time

	// Job is the status of the sync job that has not finished (RUNNING first, then a waiting one); "" if none.
	Job job.Status

	// LastOutcome and LastRunAt describe the last run that ended in this process.
	LastOutcome SyncOutcome
	LastRunAt   time.Time

	// LastRun is the last sync job that finished, of this process or an earlier one; zero if none did.
	LastRun LastRun
}

// LastRun is a finished sync job as the Google section shows it (TASK-074).
type LastRun struct {
	// At is when the job finished (its last change).
	At time.Time
	// Attempts the job took.
	Attempts int
	// Outcome is how the run ended: the outcome this process recorded for the job, otherwise what the job record
	// tells: OutcomeOK for a COMPLETED job, OutcomeFailed for a FAILED or CANCELLED one. A job that ended without a
	// sign-in completes too, so a COMPLETED record read while signed out is OutcomeNone: not claimed as a success.
	Outcome SyncOutcome
}

// OK reports whether the run synchronized the calendar.
func (r LastRun) OK() bool { return r.Outcome == OutcomeOK }
