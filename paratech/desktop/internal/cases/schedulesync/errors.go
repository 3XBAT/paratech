package schedulesync

import (
	"errors"
	"fmt"
)

// Errors of the schedule use case (ARCH-18). Domain and port errors are returned wrapped, not translated.
var (
	// ErrInvalidConfig is returned by New when a port is missing.
	ErrInvalidConfig = errors.New("invalid schedule configuration")

	// ErrUnsupportedSource means no parser is configured for the import source.
	ErrUnsupportedSource = errors.New("unsupported schedule import source")

	// ErrNotConfirmed means a commit was requested without the confirm token of a preview.
	ErrNotConfirmed = errors.New("schedule import is not confirmed")

	// ErrInvalidImport means the file has row errors; nothing was written.
	ErrInvalidImport = errors.New("schedule import has errors")

	// ErrStalePreview means the plan differs from the confirmed preview (the file or the schedule changed meanwhile);
	// nothing was written and the user previews again.
	ErrStalePreview = errors.New("schedule import preview is out of date")

	// ErrCalendarNotConnected means there is no Google sign-in for the calendar: RequestSync refuses, and the
	// CalendarAdapter returns an error wrapping it (the app maps the adapter's class). A sync job ends without writing.
	ErrCalendarNotConnected = errors.New("calendar not connected")

	// ErrCalendarSignInExpired means Google no longer accepts the stored sign-in: the user signs in again. It wraps
	// ErrCalendarNotConnected.
	ErrCalendarSignInExpired = fmt.Errorf("%w: google sign-in expired", ErrCalendarNotConnected)

	// ErrCalendarNoAccess means Google refused access to the calendar itself (a 403, a token it rejects): the user
	// signs in again and allows the calendar. The CalendarAdapter returns an error wrapping it with its retry class;
	// the run ends. It is not ErrCalendarNotConnected: the job fails.
	ErrCalendarNoAccess = errors.New("no access to the google calendar")

	// ErrLessonRejected means the calendar did not take the write of one lesson for a reason of that lesson only (a
	// refused or malformed event, an event id Google keeps taken or deleted). The CalendarAdapter returns an error
	// wrapping it with its retry class; the sync skips the lesson and goes on with the others.
	ErrLessonRejected = errors.New("calendar rejected the lesson")

	// ErrLessonsSkipped means a sync run skipped some lessons (ErrLessonRejected, ErrLessonBusy); the others were
	// written and the skipped ones stay pending.
	ErrLessonsSkipped = errors.New("calendar sync skipped some lessons")

	// ErrLessonBusy means a lesson kept changing while the sync recorded its calendar state; the job tries again.
	ErrLessonBusy = errors.New("lesson changed during calendar sync")

	// ErrUnknownJobType means a job of another type reached the calendar sync handler.
	ErrUnknownJobType = errors.New("not a calendar sync job")
)
