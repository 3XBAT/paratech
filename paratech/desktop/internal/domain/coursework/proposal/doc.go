// Package proposal is the aggregate of one action the LLM proposes, and the side-effect boundary of the app
// (PRD section 13.3, AC-14, AC-18, hard rule 3):
//
//	LLM proposes structured result -> Core validates against trusted local state -> user confirmation -> adapter acts.
//
// New parses the untrusted model output strictly: a closed set of kinds, a closed set of fields per kind, bounded text.
// The model can only name an assignment by id and supply data (a title, a due time, a text body). It can never name a
// target: recipients, file paths, URLs and calendars are not fields of the schema, so an output that tries to set them
// is malformed. Targets are derived by the core: the recipient comes from local settings at validation (Facts), the
// export file is ExportRef of the assignment, the calendar is the app's own one.
//
// Status machine (transitions.go): PROPOSED -> VALIDATED | REJECTED; VALIDATED -> CONFIRMED | DECLINED;
// CONFIRMED -> EXECUTED. REJECTED, DECLINED and EXECUTED are terminal.
//
// The commands that side-effect ports accept (EmailCommand, ExportCommand, CalendarCommand) can only be obtained from a
// CONFIRMED proposal; they have no exported constructor. Each carries an idempotency key, so a repeated execution is
// safe (hard rule 7).
package proposal
