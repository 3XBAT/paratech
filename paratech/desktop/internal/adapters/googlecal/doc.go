// Package googlecal writes the schedule and confirmed deadlines into the app's own Google calendar (PRD sections 11.3
// and 14 case 8, AC-17, AC-20; TASK-051) through the Calendar API v3 with net/http and encoding/json. It satisfies
// schedulesync.CalendarAdapter and homeworkflow.DeadlineCalendar structurally.
//
//   - Only one calendar is ever written: the secondary calendar «paratech» the app created itself (scope
//     calendar.app.created). Its id lives in the google credential store (CalendarIDName), next to the OAuth client,
//     and is the only binding: the scope does not allow calendarList.list. Nothing is read back into the core: local
//     data is the system of record. A stored calendar still named «Learning Assistant» (the name before ADR-0015) is
//     renamed with calendars.patch; a name the user chose stays.
//   - Event ids are derived from the lesson or assignment id (LessonEventID, DeadlineEventID), so no event id is
//     stored (ADR-0004(e)) and a repeated upsert updates the same event: PUT, then insert with the id on 404, then
//     PUT again on 409. A retry after a lost answer never makes a second event (hard rule 7). An update whose answer
//     is still cancelled is not reported as written (ErrNotRestored).
//   - A lesson event has the lesson title as its summary and names the kind and the teacher of the class, when
//     known, in its description (TASK-089); both are part of the lesson content, so a change of them updates it.
//   - Requests go only to the base URL (https://www.googleapis.com/calendar/v3), with the bearer client of
//     adapters/googleauth (wired in app; this package does not import it), no redirects, a timeout per request and
//     bounded answers. A 401 drops the cached access token and is retried once.
//   - Errors are *Error values matching a reason sentinel and backoff.ErrTransient or backoff.ErrPermanent (ARCH-32).
//     Their texts are fixed: never a token, a title, a calendar id or a Google answer.
//
// Facts about the Calendar API, and which of them the docs do not state: docs/refs/google-calendar.md.
package googlecal
