package schedulesync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// Operation names, for error texts and logs.
const (
	opSync    = "schedulesync: calendar sync"
	opRequest = "schedulesync: request calendar sync"
	opStatus  = "schedulesync: calendar status"
)

// Bounds of one sync run.
const (
	// syncBatch is the page of pending lessons read at a time.
	syncBatch = 50
	// maxLessonsPerRun bounds the lessons one job writes; with more pending it asks for a follow-up job, so one attempt
	// stays well inside its timeout.
	maxLessonsPerRun = 1000
	// maxRefusedPerRun stops a run whose writes keep being refused: that is no longer one bad lesson.
	maxRefusedPerRun = 5
	// saveRetries bounds the reloads of a lesson that changed while the sync recorded its calendar state.
	saveRetries = 3

	// MaxPendingShown caps the pending count of Status.
	MaxPendingShown = 999
)

// CalendarSync is the controlled one-way synchronization of the local schedule into the app calendar (PRD sections
// 11.3 and 14 case 8, AC-17, AC-20). It is the handler of CalendarSyncJobType jobs; the local schedule stays the
// system of record and nothing is read back. Deadlines are not synchronized here: they reach the calendar only through
// a confirmed proposal (hard rule 3).
//
// A run: EnsureCalendar; for a fresh calendar every live lesson is marked never written (one lesson per transaction)
// and then ConfirmCalendar, so a crash in between sees the calendar fresh again; then the pending lessons page by page:
// a live one is upserted and marked synced at the revision written, a deleted one has its event deleted and its row
// purged. No call to the calendar runs inside a transaction. Every step is repeatable: a crash after an upsert and
// before the mark upserts the same event again, a crash after a delete and before the purge deletes again (gone is
// success).
//
// Jobs: RequestSync creates a job only when no sync job waits to start (QUEUED or any waiting status), so at most one
// is queued; a request while a job runs creates the next one, which runs after it and sees what the running one may
// have missed. The key is "calendar_sync:<job id>": job keys are unique for ever (the store keeps finished jobs), so a
// fixed key would make every later request a no-op. Runs of this process never overlap.
type CalendarSync struct {
	lessons     LessonRepository
	calendar    CalendarAdapter
	account     CalendarAccount
	jobs        JobEnqueuer
	clock       Clock
	ids         IDGenerator
	tx          Tx
	log         *slog.Logger
	maxAttempts int

	running chan struct{} // holds one token while a run is in progress

	mu      sync.Mutex
	outcome SyncOutcome
	ranAt   time.Time
}

// NewCalendarSync checks the ports.
func NewCalendarSync(d CalendarDeps) (*CalendarSync, error) {
	if d.Lessons == nil || d.Calendar == nil || d.Account == nil || d.Jobs == nil || d.Clock == nil || d.IDs == nil ||
		d.Tx == nil {
		return nil, fmt.Errorf("%w: every calendar sync port except the logger is required", ErrInvalidConfig)
	}
	if d.MaxAttempts < 0 {
		return nil, fmt.Errorf("%w: max attempts %d", ErrInvalidConfig, d.MaxAttempts)
	}
	c := &CalendarSync{
		lessons: d.Lessons, calendar: d.Calendar, account: d.Account, jobs: d.Jobs, clock: d.Clock, ids: d.IDs, tx: d.Tx,
		log: d.Log, maxAttempts: d.MaxAttempts, running: make(chan struct{}, 1),
	}
	if c.maxAttempts == 0 {
		c.maxAttempts = backoff.DefaultSpec().MaxAttempts
	}
	if c.log == nil {
		c.log = slog.New(slog.DiscardHandler)
	}
	return c, nil
}

// RequestSync asks for a sync run (after an import, a sign-in, at start, or «Синхронизировать»). Without a sign-in it
// returns ErrCalendarNotConnected and creates nothing. The check and the enqueue run in one transaction, so two
// requests at the same time create one job.
func (c *CalendarSync) RequestSync(ctx context.Context) (SyncRequest, error) {
	signedIn, err := c.account.SignedIn(ctx)
	switch {
	case err != nil:
		return SyncRequest{}, fmt.Errorf("%s: %w", opRequest, err)
	case !signedIn:
		return SyncRequest{}, fmt.Errorf("%s: %w", opRequest, ErrCalendarNotConnected)
	}

	var out SyncRequest
	err = c.tx.Do(ctx, func(ctx context.Context) error {
		waiting, err := c.jobs.ListByStatus(ctx, notStarted()...)
		if err != nil {
			return err
		}
		for _, j := range waiting {
			if j.Type() == CalendarSyncJobType {
				out = SyncRequest{JobID: j.ID()}
				return nil
			}
		}

		id := c.ids.NewJobID()
		j, err := job.New(job.Spec{
			ID: id, Type: CalendarSyncJobType, Key: string(CalendarSyncJobType) + ":" + id.String(), MaxAttempts: c.maxAttempts,
		}, c.clock.Now())
		if err != nil {
			return err
		}
		jobID, created, err := c.jobs.Enqueue(ctx, j)
		if err != nil {
			return err
		}
		out = SyncRequest{JobID: jobID, Created: created}
		return nil
	})
	if err != nil {
		return SyncRequest{}, fmt.Errorf("%s: %w", opRequest, err)
	}
	return out, nil
}

// notStarted are the statuses of a job that will still run: every active status but RUNNING.
func notStarted() []job.Status {
	var out []job.Status
	for _, s := range job.ActiveStatuses() {
		if s != job.StatusRunning {
			out = append(out, s)
		}
	}
	return out
}

// Handle runs one sync job (jobrunner.Handler). Without a sign-in, or with one Google no longer accepts, the job ends
// successfully without writing and the status says so. A transient failure is returned for a retry with backoff (one
// that wraps shared.ErrNetworkUnavailable waits for the network), a permanent one fails the job; the texts are fixed.
func (c *CalendarSync) Handle(ctx context.Context, snap job.Snapshot, _ func(context.Context, job.Progress) error) error {
	if snap.Type() != CalendarSyncJobType {
		return backoff.Permanent(fmt.Errorf("%s: %w", opSync, ErrUnknownJobType))
	}
	select {
	case c.running <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", opSync, ctx.Err())
	}
	defer func() { <-c.running }()

	st, err := c.run(ctx)
	if ctx.Err() != nil {
		return fmt.Errorf("%s: %w", opSync, ctx.Err()) // cancelled or stopped: the runner decides, nothing to report
	}
	outcome := outcomeOf(err)
	c.record(outcome)
	switch outcome {
	case OutcomeOK:
		c.log.Info(opSync, "written", st.written, "deleted", st.deleted, "fresh", st.fresh)
		if st.more {
			c.followUp(ctx)
		}
		return nil
	case OutcomeNotSignedIn, OutcomeSignInExpired:
		c.log.Info(opSync, "outcome", string(outcome))
		return nil
	}
	c.log.Warn(opSync, "written", st.written, "deleted", st.deleted, "err", err)
	return fmt.Errorf("%s: %w", opSync, err)
}

// followUp asks for the next job when the run stopped at its bound with lessons still pending.
func (c *CalendarSync) followUp(ctx context.Context) {
	if _, err := c.RequestSync(ctx); err != nil && !errors.Is(err, ErrCalendarNotConnected) {
		c.log.Warn(opSync, "follow-up", "not requested", "err", err)
	}
}

func outcomeOf(err error) SyncOutcome {
	switch {
	case err == nil:
		return OutcomeOK
	case errors.Is(err, ErrCalendarSignInExpired):
		return OutcomeSignInExpired
	case errors.Is(err, ErrCalendarNotConnected):
		return OutcomeNotSignedIn
	}
	return OutcomeFailed
}

func (c *CalendarSync) record(outcome SyncOutcome) {
	at := c.clock.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.outcome, c.ranAt = outcome, at
}

// runStats counts what one run did.
type runStats struct {
	written, deleted int
	fresh            bool
	more             bool // stopped at maxLessonsPerRun with lessons left
}

// run is one sync run. Errors of the calendar keep their classes.
func (c *CalendarSync) run(ctx context.Context) (runStats, error) {
	var st runStats
	calendarID, fresh, err := c.calendar.EnsureCalendar(ctx)
	if err != nil {
		return st, err
	}
	if fresh {
		st.fresh = true
		if err := c.forgetAll(ctx); err != nil {
			return st, err
		}
		if err := c.calendar.ConfirmCalendar(ctx, calendarID); err != nil {
			return st, err
		}
	}

	refused := map[shared.LessonID]bool{}
	var firstRefusal error
	for processed := 0; ; {
		// Refused lessons stay pending; read past them.
		batch, err := c.lessons.ListPendingCalendarSync(ctx, len(refused)+syncBatch)
		if err != nil {
			return st, err
		}
		todo := 0
		for _, l := range batch {
			if refused[l.ID()] {
				continue
			}
			if processed >= maxLessonsPerRun {
				st.more = true
				return st, nil
			}
			todo++
			processed++
			err := c.syncOne(ctx, calendarID, l, &st)
			switch {
			case err == nil:
			case ctx.Err() != nil, errors.Is(err, ErrCalendarNotConnected), backoff.Classify(err) != backoff.ClassPermanent:
				return st, err
			default:
				refused[l.ID()] = true
				if firstRefusal == nil {
					firstRefusal = err
				}
				if len(refused) >= maxRefusedPerRun {
					return st, refusedError(len(refused), firstRefusal)
				}
			}
		}
		if todo == 0 {
			break
		}
	}
	if firstRefusal != nil {
		return st, refusedError(len(refused), firstRefusal)
	}
	return st, nil
}

func refusedError(n int, first error) error {
	return backoff.Permanent(fmt.Errorf("%d: %w: %w", n, ErrLessonsRefused, first))
}

// syncOne writes one pending lesson: a deleted one loses its event and then its row, a live one is upserted and then
// marked synced at the revision it had when it was written.
func (c *CalendarSync) syncOne(ctx context.Context, calendarID string, l *lesson.Lesson, st *runStats) error {
	if l.Deleted() {
		if err := c.calendar.DeleteEvent(ctx, calendarID, l.ID()); err != nil {
			return err
		}
		if err := c.tx.Do(ctx, func(ctx context.Context) error { return c.lessons.PurgeDeleted(ctx, l.ID()) }); err != nil {
			return backoff.Transient(err)
		}
		st.deleted++
		return nil
	}

	revision := l.ContentRevision()
	if err := c.calendar.UpsertLesson(ctx, calendarID, l); err != nil {
		return err
	}
	if err := c.markSynced(ctx, l, revision); err != nil {
		return err
	}
	st.written++
	return nil
}

// markSynced records that revision is in the calendar. A lesson that changed meanwhile is reloaded and marked again:
// the aggregate keeps it pending when its content moved past revision. A deleted one stays pending as deleted.
func (c *CalendarSync) markSynced(ctx context.Context, l *lesson.Lesson, revision int64) error {
	now := c.clock.Now()
	for range saveRetries {
		if err := l.MarkCalendarSynced(revision, now); err != nil {
			return backoff.Permanent(err)
		}
		err := c.save(ctx, l)
		if !errors.Is(err, shared.ErrVersionConflict) {
			return err
		}
		if l, err = c.reload(ctx, l.ID()); err != nil || l == nil {
			return err
		}
	}
	return backoff.Transient(ErrLessonBusy)
}

// forgetAll marks every live lesson as never written: the calendar is fresh and holds none of their events.
func (c *CalendarSync) forgetAll(ctx context.Context) error {
	live, err := c.lessons.ListLive(ctx)
	if err != nil {
		return backoff.Transient(err)
	}
	for _, l := range live {
		if err := c.forget(ctx, l); err != nil {
			return err
		}
	}
	return nil
}

func (c *CalendarSync) forget(ctx context.Context, l *lesson.Lesson) error {
	for range saveRetries {
		if !l.ForgetCalendarProjection() {
			return nil
		}
		err := c.save(ctx, l)
		if !errors.Is(err, shared.ErrVersionConflict) {
			return err
		}
		if l, err = c.reload(ctx, l.ID()); err != nil || l == nil {
			return err
		}
	}
	return backoff.Transient(ErrLessonBusy)
}

// save stores one lesson in its own transaction; storage errors are transient.
func (c *CalendarSync) save(ctx context.Context, l *lesson.Lesson) error {
	err := c.tx.Do(ctx, func(ctx context.Context) error { return c.lessons.Save(ctx, l) })
	if err != nil && !errors.Is(err, shared.ErrVersionConflict) {
		return backoff.Transient(err)
	}
	return err
}

// reload returns the stored lesson, or nil when it is gone or deleted (it stays pending as deleted).
func (c *CalendarSync) reload(ctx context.Context, id shared.LessonID) (*lesson.Lesson, error) {
	l, err := c.lessons.Get(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, backoff.Transient(err)
	}
	return l, nil
}

// Status is the synchronization as the page shows it. It does not call Google.
func (c *CalendarSync) Status(ctx context.Context) (CalendarStatus, error) {
	signedIn, err := c.account.SignedIn(ctx)
	if err != nil {
		return CalendarStatus{}, fmt.Errorf("%s: %w", opStatus, err)
	}
	pending, err := c.lessons.ListPendingCalendarSync(ctx, MaxPendingShown+1)
	if err != nil {
		return CalendarStatus{}, fmt.Errorf("%s: %w", opStatus, err)
	}
	live, err := c.lessons.ListLive(ctx)
	if err != nil {
		return CalendarStatus{}, fmt.Errorf("%s: %w", opStatus, err)
	}
	active, err := c.jobs.ListByStatus(ctx, job.ActiveStatuses()...)
	if err != nil {
		return CalendarStatus{}, fmt.Errorf("%s: %w", opStatus, err)
	}

	st := CalendarStatus{SignedIn: signedIn, Pending: min(len(pending), MaxPendingShown), PendingMore: len(pending) > MaxPendingShown}
	for _, l := range live {
		if at := l.CalendarSyncedAt(); l.CalendarSyncedRevision() > 0 && at.After(st.LastWrittenAt) {
			st.LastWrittenAt = at
		}
	}
	for _, j := range active {
		if j.Type() == CalendarSyncJobType && jobRank(j.Status()) > jobRank(st.Job) {
			st.Job = j.Status()
		}
	}
	c.mu.Lock()
	st.LastOutcome, st.LastRunAt = c.outcome, c.ranAt
	c.mu.Unlock()
	return st, nil
}

// jobRank orders the statuses of unfinished sync jobs for Status: what runs now matters most, then why one waits.
func jobRank(s job.Status) int {
	switch s {
	case "":
		return 0
	case job.StatusRunning:
		return 5
	case job.StatusWaitingForNetwork:
		return 4
	case job.StatusRetryWait:
		return 3
	case job.StatusQueued:
		return 2
	}
	return 1
}
