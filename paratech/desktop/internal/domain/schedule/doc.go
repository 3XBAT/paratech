// Package schedule is the bounded context of the local class schedule, the canonical copy of which lives in
// SQLite (PRD sections 11 and 5.3, AC-16, AC-20).
// Aggregates: domain/schedule/lesson. Domain services: domain/schedule/services/importplan (validation, preview and
// conflicts of an import).
//
// Layout of a context (docs/architecture.md section 3.1):
//
//	schedule/doc.go                  this file: purpose and glossary
//	schedule/<aggregate>/            one package per aggregate
//	schedule/values/                 value objects shared by several aggregates of the context
//	schedule/services/<service>/     domain services and policies
//
// Rules that apply: ARCH-10..19. The context imports only std, itself and domain/shared.
//
// # Glossary
//
//   - Lesson: one schedule entry (a class occurrence) with start, end and the time zone of the source record.
//   - Import: ICS, CSV or XLSX file parsed into lessons through parse, validate, preview, commit; a malformed
//     file never changes the local schedule.
//   - Source UID: the identity of an entry in the imported file, used to diff a new import against the stored schedule.
//   - Start announcement: the "lesson is starting" event that offers the recording prompt, raised once per start
//     time, from the lead time before the start until a short grace (lesson.StartLateGrace) after it.
//   - Calendar projection: the copy of a lesson in the app-created Google calendar; an external view, never the source of truth.
package schedule
