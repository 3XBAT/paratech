// Package icsschedule reads a class schedule from an iCalendar (.ics) file (PRD sections 4.3 and 11.2, AC-16). It
// implements the schedulesync.ScheduleParser port for importplan.SourceICS. golang-ical parses the file (unfolding,
// unescaping, properties and parameters, RRULE values); it does not expand recurrences, so the expansion is here,
// with the standard library. The format is also described in docs/refs/schedule-ics.md.
//
// Bounds (import dates): only occurrences that start in Window(clock.Now()) become rows, for single events too: from
// the start of yesterday in UTC, 366 days long. The window moves once a day, so a preview and its commit on the same
// day read the same rows (the commit parses again and compares; across midnight UTC it may report a stale preview).
// The rows carry the window (importplan.Parsed.Window): live "ics:" lessons missing from the file are removed only
// inside it, so past lessons stay when a later import no longer covers them.
//
// Events:
//
//   - UTF-8, an optional byte order mark, at most MaxFileBytes, MaxEvents VEVENTs and importplan.MaxRows lessons
//     in the bounds. One complete VCALENDAR (a cut file is refused). A logical line has at most 64 KiB and
//     32 parameter separators (golang-ical needs quadratic time for more); components nest at most 16 deep.
//   - Title: SUMMARY; escaped line breaks and tabs become spaces. LOCATION, DESCRIPTION, alarms and attendees are
//     ignored (a lesson has no field for them).
//   - Times: DTSTART with TZID (an IANA name, or a Windows name such as "Russian Standard Time" mapped through
//     golang-ical's CLDR table), in UTC ("Z"), or floating. A floating time uses X-WR-TIMEZONE of the calendar;
//     without it the event is a row error. A UTC event recurs in UTC and its lessons show X-WR-TIMEZONE when the
//     calendar has one (Google writes single events in UTC), else UTC. Unknown zones are row errors; VTIMEZONE
//     blocks are not read (the zone database is compiled in). A time skipped by a DST change is a row error.
//   - Length: DTEND or DURATION (one of them, not both). The lesson rules (title, at most 24 h, end after start)
//     are checked by importplan, not here.
//   - All-day events (VALUE=DATE) are not lessons and are skipped. STATUS:CANCELLED drops the event, a whole
//     series when set on the master.
//
// Recurrence: one RRULE with FREQ=DAILY, WEEKLY, MONTHLY or YEARLY, INTERVAL (1..1000), COUNT or UNTIL (UTC,
// floating or a date, inclusive), BYDAY without numbers for DAILY and WEEKLY, WKST. Other parts (BYMONTHDAY,
// BYSETPOS, 2MO, ...), several RRULEs, EXRULE and RANGE are row errors, never approximated. EXDATE (with TZID,
// several values, or dates) removes occurrences; RDATE date-times add them. Expansion keeps the wall-clock time of
// DTSTART in the event zone, so a 09:00 class stays at 09:00 across DST. DTSTART is always the first occurrence. A
// VEVENT with the same UID and a RECURRENCE-ID replaces (or, when cancelled, removes) that occurrence; it is a row
// when its own start is in the bounds. Series whose DTSTART is at or after To, or whose UNTIL is before From, are not
// checked against the subset. MaxExpansionSteps bounds the work for the whole file.
//
// Identity: a one-off event with a UID (no RRULE, RDATE or changed occurrences) has the key "<id>", so moving it
// changes the lesson. A series occurrence has "<id>/<original start in UTC, RFC 3339>", and so has an event without a
// UID. <id> is the UID, its "sha256-<hex>" when longer than 255 bytes, not printable or containing '/', and
// "auto-<hash of the title>" without a UID. A changed occurrence keeps the key of the start it replaces. The stored
// source uid is "ics:<key>" (importplan). One master per UID and one change per RECURRENCE-ID; repeats are row errors.
// A date-only EXDATE is a date in the zone the lessons are shown in.
//
// Line numbers are lines of the file: a row and its errors point at the BEGIN:VEVENT of the event (golang-ical has
// no line numbers; a pre-scan computes them). A row problem is returned as data; a file that cannot be read at all
// gives an *Error wrapping importplan.ErrUnreadableFile. Messages never quote values of the file.
package icsschedule
