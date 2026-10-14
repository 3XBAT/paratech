// Package lesson is the aggregate of one schedule entry, a class occurrence (PRD section 11).
//
// A lesson has a Content (title, start, end and the IANA time zone name of the source record), an optional source
// uid that identifies it in imported files, the state of its calendar projection (two revision counters, ADR-0004(e))
// and a soft-delete mark. The time zone is validated only syntactically here: loading zone data is I/O and belongs to
// the parsers (adapters). The "lesson is starting" mark (start_notified_at) is restorable state; the scheduler
// behaviour around it belongs to TASK-018.
package lesson
