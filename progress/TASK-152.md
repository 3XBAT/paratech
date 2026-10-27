# TASK-152 — notes in «База знаний»

## Done
- Domain `services/knowledgebase`: keys `recording:<id>`, `lesson:<id>`, `day:<subject>:<date>`, `note:<id>`; `WithNotes` joins notes to recordings (a schedule-lesson note joins the newest recording of that lesson), else own lecture, lost lesson falls back to its day, undated notes are one lecture each, last in `Newest`/`Oldest`. `Document.Notes` fragments, section `ownNote` (hits carry note, material, locator), ADR-0019 limits kept. `RenderSubject` adds «Мой конспект» with a student-text source line; student text escaped line by line (`cleanText` + `EscapeMarkdown`).
- Use case: ports `NoteStore`, `NoteTextReader`, `SubjectLister` (all three or none); `Lectures`, and notes in `Subjects`, `Search`, `SubjectFileName`, `ExportSubject`. Removed notes/materials left out; damaged/foreign-subject parts skipped and logged by ids. A failed notes/subjects read is logged and the base falls back to recorded lessons (only cancel fails).
- SQLite: read query `ListNotes`, `NoteRepository.ListAll` (unreadable rows reported by id). Wiring in `app/core.go`.
- Wails layer still hides note-only lectures and `ownNote` hits (current page cannot show them).

## Decisions to confirm
- ADR-0027 Open 6 NOT decided: `ownNote` is a separate section scanned after the transcript in a lesson; under the 50-hit cap notes are cut first. `Passages`/answers untouched.
- `Texts` (answers) stays recorded-only until TASK-153 (review medium).
- `Lessons` stays recorded-only; note-only lectures come via `Lectures`.
- Undated order = note creation time (no ordering field; TASK-149 question).
- A note on a recording of another subject shows under that recording's subject (ADR wording); its parts are still checked against the note's subject.
- A note on a recording still processing shows as a `day:` lecture until it lands.
- `kb.RenderSubject` renders recorded lessons via `summaryexport.RenderSubject` and trims its header (summaryexport outside scope).

## Follow-ups
- TASK-155: needs Wails/TS bindings for `Lectures` and `ownNote` hits (its allowed files must include `input/wails/knowledgebase.go` and `bindings/knowledgeBase.ts`); the binding drops note hits after the caps, so the current page can show fewer recording hits.
- TASK-153: `Passages` indexes by recording id; switch to `Lesson.Key` (note-only lectures share key "").
- Optional: per-lesson renderer in `summaryexport`.

## Changed existing tests
None (new tests only). Within this task: one test was inverted after review (Texts leaves notes out).

## Security
Auditor: no findings. Reviewer: 2 mediums (Texts depends on notes; failure isolation) fixed in ae6426a1, nit fixed; lows above.
