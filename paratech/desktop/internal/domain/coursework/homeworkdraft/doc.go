// Package homeworkdraft is the aggregate of the draft history of one assignment (PRD section 10, AC-13): the homework
// text the student generates with the LLM, edits locally and refines with additional instructions before exporting
// and sending it.
//
// A Draft is identified by its assignment and holds its versions (Revision, numbered 1..n, append-only). A version is
// generated (by a homework_draft job, with the instructions and prompts version used) or edited (by the student).
// Every change names the version it starts from, its base, and is refused with ErrStaleBase unless that is still the
// latest version, so a generation that finishes after a user edit never becomes the latest version over the edit, and
// an edit made on an old version is not taken silently. A generation is applied at most once per job.
//
// Generated text is model output and is data only (hard rule 3); exporting or sending it is a separate user action.
package homeworkdraft
