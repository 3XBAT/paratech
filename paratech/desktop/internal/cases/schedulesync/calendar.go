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
	// maxSkippedInARow stops a run whose writes fail one lesson after another: that is no longer a few bad lessons.
	// A few stuck lessons at the head of the order must not reach it, or they would stop every run before the rest.
	maxSkippedInARow = 20
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
// fixed key would make every later request a no-op. Runs of this process never overlap: a job that starts while a
// run is in progress ends at once, and the running one requests the next job when it ends.
//
// A lesson whose write fails for a reason of its own (ErrLessonRejected, ErrLessonBusy) is skipped for the rest of
// the run; the job then fails with ErrLessonsSkipped, transient when a skip may pass on a retry. Any other failure
// ends the run at once.
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

	mu      sync.Mutex
	busy    bool // a run is in progress
	rerun   bool // a job started during that run
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
		log: d.Log, maxAttempts: d.MaxAttempts,
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
// A job that starts while a run is in progress ends at once without holding a runner worker.
func (c *CalendarSync) Handle(ctx context.Context, snap job.Snapshot, _ func(context.Context, job.Progress) error) error {
	if snap.Type() != CalendarSyncJobType {
		return backoff.Permanent(fmt.Errorf("%s: %w", opSync, ErrUnknownJobType))
	}
	if !c.begin() {
		c.log.Info(opSync, "skipped", "a run is in progress and requests the next job")
		return nil
	}

	st, rerun, err := c.runClaimed(ctx)
	if rerun {
		// Not lost on a cancel either: the request came from another job.
		c.followUp(context.WithoutCancel(ctx))
	}
	if ctx.Err() != nil {
		return fmt.Errorf("%s: %w", opSync, ctx.Err()) // cancelled or stopped: the runner decides, nothing to report
	}
	if st.more {
		c.followUp(ctx)
	}
	outcome := outcomeOf(err)
	c.record(outcome)
	switch outcome {
	case OutcomeOK:
		c.log.Info(opSync, "written", st.written, "deleted", st.deleted, "fresh", st.fresh)
		return nil
	case OutcomeNotSignedIn, OutcomeSignInExpired:
		c.log.Info(opSync, "outcome", string(outcome))
		return nil
	}
	c.log.Warn(opSync, "written", st.written, "deleted", st.deleted, "err", err)
	return fmt.Errorf("%s: %w", opSync, err)
}

// begin claims the run of this process. When one is in progress it reports false and that run requests the next job.
func (c *CalendarSync) begin() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.busy {
		c.rerun = true
		return false
	}
	c.busy = true
	return true
}

// runClaimed is run after a successful begin; the claim is released also on a panic.
func (c *CalendarSync) runClaimed(ctx context.Context) (st runStats, rerun bool, err error) {
	defer func() { rerun = c.end() }()
	st, err = c.run(ctx)
	return st, false, err
}

// end releases the run and reports whether a job started meanwhile.
func (c *CalendarSync) end() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	rerun := c.rerun
	c.busy, c.rerun = false, false
	return rerun
}

// followUp asks for the next job: a job started during the run, or the run stopped at its bound with lessons still
// pending.
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

	// Every page holds at least one lesson not tried yet, or the loop ends; maxLessonsPerRun bounds the rest.
	var sk skips
	inARow := 0
	for processed := 0; ; {
		// Skipped lessons stay pending; read past them.
		batch, err := c.lessons.ListPendingCalendarSync(ctx, len(sk.ids)+syncBatch)
		if err != nil {
			return st, err
		}
		todo := 0
		for _, l := range batch {
			if sk.ids[l.ID()] {
				continue
			}
			if processed >= maxLessonsPerRun {
				st.more = true
				return st, sk.err()
			}
			todo++
			processed++
			err := c.syncOne(ctx, calendarID, l, &st)
			switch {
			case err == nil:
				inARow = 0
			case ctx.Err() != nil, !lessonOnly(err):
				return st, err
			default:
				sk.add(l.ID(), err)
				if inARow++; inARow >= maxSkippedInARow {
					return st, sk.err()
				}
			}
		}
		if todo == 0 {
			break
		}
	}
	return st, sk.err()
}

// lessonOnly reports whether err concerns only the lesson being synced, so the run goes on with the others.
func lessonOnly(err error) bool {
	return !errors.Is(err, ErrCalendarNotConnected) && (errors.Is(err, ErrLessonRejected) ||
		errors.Is(err, ErrLessonBusy) || errors.Is(err, lesson.ErrInvalidLesson))
}

// skips are the lessons a run skipped and the reasons the job reports.
type skips struct {
	ids            map[shared.LessonID]bool
	first          error
	firstTransient error
}

func (s *skips) add(id shared.LessonID, err error) {
	if s.ids == nil {
		s.ids = map[shared.LessonID]bool{}
	}
	s.ids[id] = true
	if s.first == nil {
		s.first = err
	}
	if s.firstTransient == nil && backoff.Classify(err) == backoff.ClassTransient {
		s.firstTransient = err
	}
}

// err is nil without skips. With one that may pass on a retry it is transient, so the runner tries the job again;
// otherwise permanent. The wrapped reason is of that class: a permanent one inside would make any wrapper permanent.
func (s *skips) err() error {
	switch {
	case len(s.ids) == 0:
		return nil
	case s.firstTransient != nil:
		return backoff.Transient(fmt.Errorf("%d: %w: %w", len(s.ids), ErrLessonsSkipped, s.firstTransient))
	}
	return backoff.Permanent(fmt.Errorf("%d: %w: %w", len(s.ids), ErrLessonsSkipped, s.first))
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
