// Package lesson is the aggregate of one schedule entry, a class occurrence (PRD section 11).
//
// A lesson has a Content (title, start, end and the IANA time zone name of the source record), an optional source
// uid that identifies it in imported files, the state of its calendar projection (two revision counters, ADR-0004(e))
// and a soft-delete mark. The time zone is validated only syntactically here: loading zone data is I/O and belongs to
// the parsers (adapters). The "lesson is starting" mark (start_notified_at) is set by AnnounceStart in the window
// [start-lead, end) and only once per start time (announce.go, PRD section 6.1), which returns the StartAnnounced event.
package lesson
