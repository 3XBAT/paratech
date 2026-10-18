package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/adapters/googleauth"
	"paratech/desktop/internal/adapters/googlecal"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/googlesignin"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// The adapter fits both calendar ports; the bridges only map its sign-in classes to the sentinels of the use cases.
var (
	_ schedulesync.CalendarAdapter  = (*googlecal.Client)(nil)
	_ homeworkflow.DeadlineCalendar = (*googlecal.Client)(nil)
	_ schedulesync.CalendarAdapter  = lessonCalendar{}
	_ homeworkflow.DeadlineCalendar = deadlineCalendar{}
	_ schedulesync.CalendarAccount  = googleAccount{}
	_ jobrunner.Handler             = (*schedulesync.CalendarSync)(nil)
	_ googlecal.Auth                = (*googleauth.Authorizer)(nil)
	_ googlecal.AuthClassifier      = classifyGoogleAuth
)

// errGoogleStoreUnavailable means the google credential store could not say whether a sign-in is stored.
var errGoogleStoreUnavailable = errors.New("app: google credential store unavailable")

// wireCalendar builds the calendar sync of the schedule (TASK-051, PRD section 11.3) on the googlecal client of
// wireGoogle: the handler of the calendar_sync jobs, asked for a run after an import, a sign-in, at start and by
// «Синхронизировать». Deadlines do not go through it: only a confirmed proposal writes one (deadlineCalendar).
func (c *core) wireCalendar(e env, jobs *sqlite.JobRepository, lessons *sqlite.LessonRepository, log *slog.Logger,
	maxAttempts int) error {
	var err error
	c.calendarSync, err = schedulesync.NewCalendarSync(schedulesync.CalendarDeps{
		Lessons: lessons, Calendar: lessonCalendar{c.calendar}, Account: googleAccount{c.googleSignIn}, Jobs: jobs,
		Clock: e.clock, IDs: e.ids, Tx: c.db, Log: log, MaxAttempts: maxAttempts,
	})
	if err != nil {
		return fmt.Errorf("app: calendar sync: %w", err)
	}
	return nil
}

// calendarSignedIn runs after a successful Google sign-in: the outcome of the earlier run (an expired sign-in, no
// access) no longer asks the user anything, and a sync is requested.
func (c *core) calendarSignedIn(ctx context.Context, log *slog.Logger) {
	if c.calendarSync != nil {
		c.calendarSync.ForgetOutcome()
	}
	c.requestCalendarSync(ctx, log, "sign-in")
}

// requestCalendarSync asks for a calendar sync after the trigger why; without a sign-in there is nothing to do. A
// failure is only logged: the lessons stay pending and the next trigger picks them up.
func (c *core) requestCalendarSync(ctx context.Context, log *slog.Logger, why string) {
	if c.calendarSync == nil {
		return
	}
	req, err := c.calendarSync.RequestSync(ctx)
	switch {
	case errors.Is(err, schedulesync.ErrCalendarNotConnected):
		log.Debug("calendar sync not requested: not signed in to google", "after", why)
	case err != nil:
		log.Warn("calendar sync not requested", "after", why, "err", err)
	case req.Created:
		log.Info("calendar sync requested", "after", why, "job", req.JobID.String())
	}
}

// classifyGoogleAuth is the googlecal.AuthClassifier of the googleauth token source: no sign-in or no client is "not
// signed in", invalid_grant is "sign in again", a refused refresh is "rejected"; the rest (network, Google down, the
// credential store) is retried.
func classifyGoogleAuth(err error) googlecal.AuthFailure {
	switch {
	case errors.Is(err, googleauth.ErrReauthRequired):
		return googlecal.AuthExpired
	case errors.Is(err, googleauth.ErrNotSignedIn), errors.Is(err, googleauth.ErrNotConfigured):
		return googlecal.AuthNotSignedIn
	case errors.Is(err, googleauth.ErrInvalidClient), errors.Is(err, googleauth.ErrRejected),
		errors.Is(err, googleauth.ErrBadResponse):
		return googlecal.AuthRejected
	}
	return googlecal.AuthNone
}

// lessonCalendar is the schedulesync.CalendarAdapter of the googlecal client: mapping only.
type lessonCalendar struct{ c *googlecal.Client }

func (l lessonCalendar) EnsureCalendar(ctx context.Context) (string, bool, error) {
	id, fresh, err := l.c.EnsureCalendar(ctx)
	return id, fresh, lessonCalendarError(err)
}

func (l lessonCalendar) ConfirmCalendar(ctx context.Context, calendarID string) error {
	return lessonCalendarError(l.c.ConfirmCalendar(ctx, calendarID))
}

func (l lessonCalendar) UpsertLesson(ctx context.Context, calendarID string, ls *lesson.Lesson) error {
	return lessonWriteError(l.c.UpsertLesson(ctx, calendarID, ls))
}

func (l lessonCalendar) DeleteEvent(ctx context.Context, calendarID string, id shared.LessonID) error {
	return lessonWriteError(l.c.DeleteEvent(ctx, calendarID, id))
}

// lessonWriteError is lessonCalendarError for the write of one lesson: the adapter reasons that concern only that
// event (a bad request, a 4xx refusal of it, an id Google keeps taken or deleted) also get
// schedulesync.ErrLessonRejected, so the sync skips the lesson and goes on. A 403 or 404 means the calendar itself
// (access or the calendar gone) and ends the run.
func lessonWriteError(err error) error {
	err = lessonCalendarError(err)
	switch {
	case errors.Is(err, schedulesync.ErrCalendarNotConnected):
		return err
	case errors.Is(err, googlecal.ErrInvalidRequest), errors.Is(err, googlecal.ErrRejected),
		errors.Is(err, googlecal.ErrConflict), errors.Is(err, googlecal.ErrNotRestored):
		return fmt.Errorf("%w: %w", schedulesync.ErrLessonRejected, err)
	}
	return err
}

// lessonCalendarError adds the sentinel of the use case to the adapter's sign-in and access classes; the adapter
// error stays wrapped, so its retry class is kept.
func lessonCalendarError(err error) error {
	switch {
	case errors.Is(err, googlecal.ErrReauthRequired):
		return fmt.Errorf("%w: %w", schedulesync.ErrCalendarSignInExpired, err)
	case errors.Is(err, googlecal.ErrNotSignedIn):
		return fmt.Errorf("%w: %w", schedulesync.ErrCalendarNotConnected, err)
	case errors.Is(err, googlecal.ErrForbidden), errors.Is(err, googlecal.ErrUnauthorized):
		// Not ErrAuthRejected: it also means a bad client or a broken token answer, where signing in again does not help.
		return fmt.Errorf("%w: %w", schedulesync.ErrCalendarNoAccess, err)
	}
	return err
}

// deadlineWriter is googlecal.Client as the deadline bridge uses it.
type deadlineWriter interface {
	UpsertDeadline(ctx context.Context, cmd proposal.CalendarCommand) error
}

// deadlineCalendar is the homeworkflow.DeadlineCalendar of the googlecal client. Without a sign-in (or with one Google
// no longer accepts) it is homeworkflow.ErrCalendarNotConnected, so the proposal stays CONFIRMED and can be executed
// after a sign-in.
type deadlineCalendar struct{ c deadlineWriter }

func (d deadlineCalendar) UpsertDeadline(ctx context.Context, cmd proposal.CalendarCommand) error {
	err := d.c.UpsertDeadline(ctx, cmd)
	if errors.Is(err, googlecal.ErrNotSignedIn) {
		return fmt.Errorf("%w: %w", homeworkflow.ErrCalendarNotConnected, err)
	}
	return err
}

// googleAccount is the schedulesync.CalendarAccount of the sign-in use case. A system without a credential store has
// no sign-in.
type googleAccount struct{ signIn *googlesignin.Service }

func (g googleAccount) SignedIn(ctx context.Context) (bool, error) {
	switch g.signIn.Status(ctx).SignedIn {
	case googlesignin.SecretSet:
		return true, nil
	case googlesignin.SecretUnset, googlesignin.SecretUnsupported:
		return false, nil
	}
	return false, errGoogleStoreUnavailable
}
