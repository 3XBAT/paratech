// Package schedulesync is the use case of the local schedule: importing ICS, CSV and XLSX with
// parse, validate, preview and commit, announcing that a lesson starts, and controlled one-way synchronization
// of lessons to the app-created Google calendar (PRD sections 11 and 14 cases 8 and 9, AC-16, AC-17, AC-20).
//
// TASK-003 contains only the ports (ports.go). The Service arrives with TASK-018 (scheduler), TASK-046 to TASK-048
// (import) and TASK-051 (calendar sync job). The local schedule stays the system of record; nothing is read back
// from the calendar.
//
// Rules that apply: ARCH-20..27. File parsers are adapters or domain services added by those tasks.
package schedulesync
