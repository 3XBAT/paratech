// Package schedulesync is the use case of the local schedule: importing ICS, CSV and XLSX with
// parse, validate, preview and commit, announcing that a lesson starts, and controlled one-way synchronization
// of lessons to the app-created Google calendar (PRD sections 11 and 14 cases 8 and 9, AC-16, AC-17, AC-20).
//
// Import (TASK-046, import.go): PreviewImport parses the file through a ScheduleParser, validates it and builds the
// preview with domain/schedule/services/importplan; it stores nothing. CommitImport needs the confirm token of that
// preview: it parses the same bytes again, rebuilds the plan against the current schedule inside one transaction and
// writes it with one LessonRepository.ApplyChanges only if the plan is unchanged (same fingerprint) and has no errors.
// After a commit that changed the schedule it asks CalendarSync for a run, outside the transaction.
//
// Calendar (TASK-051, calendar.go): CalendarSync is the handler of the calendar_sync job. It writes the pending lessons
// into the app calendar through the CalendarAdapter with stable event ids and idempotent upserts, deletes the events
// of deleted lessons and then purges them, and rewrites everything into a fresh calendar. The local schedule stays the
// system of record; nothing is read back from the calendar. Deadlines reach the calendar only through confirmed
// proposals (homeworkflow), never through this job.
//
// Rules that apply: ARCH-20..27. File parsers are adapters (one per format) behind the ScheduleParser port.
package schedulesync
