# TASK-153 — answers cite the student's notes

## Done
- `answer.Source` section `ownNote`: note, material, locator (page/slide/section/fragment + N), heading (section only, one line, <=120 runes); recording empty for it and required for the rest. Idempotency key of recording fragments is unchanged; stored TASK-118 answers load (optional sqlite JSON fields, no migration).
- Label «ваш конспект» only in `answer.OwnNoteName` (`label.go`, `Source.OwnNoteLabel()`); «источник удалён» only in `answer.SourceRemovedLabel`.
- `knowledgebase.Passages` indexes by `Lesson.Key` (fallback to the recording key), scans note fragments. Ranking unchanged: inside a lesson summary, lecture text, transcript, then notes.
- `answering`: note text only in `UntrustedData`; the planner gets counters only (fixtures with commands). `Card` method returns the answer plus `Removed []bool`, using the narrow port `answering.Notes.NoteActive`; fail-open on error (logged by ids). Wired in `app/answers.go`, `core.go`. Wails `AnswerSource` gets `noteId`, `materialId`, `locatorKind`, `locatorN`, `label`, `removed`.
- Tickets: a ticket answered from a note is covered; wording «Нет в ваших лекциях и конспектах» (`homeworkdoc/tickets.go`, `ticketsView.ts` hint); `.docx` cites «[источник: ваш конспект · стр. N, …]».
- `cases/knowledgebase.Texts` now includes notes (`withNotes`, `LibraryKey` filter).

## Extra files (supervisor widened the scope)
`cases/knowledgebase/service.go` + `notes_test.go`; `homeworkdoc/tickets.go` + test; `desktop/tests/tickets_test.go` (old wording); `app/answers.go`, `answers_test.go`, `core.go`; `frontend/src/ticketsView.ts`.

## Decisions to confirm (owner)
- ADR-0027 Open 6: AC and ADR say the student's fragment ranks before the AI summary; NOT done, the order is unchanged and tests pin it (summary before note). Flip when answered.
- ADR-0027 Open 1: «ваш конспект» is the working name (one constant; `knowledgebase/export.go` and `ownNotesView.ts` have their own UI strings, deliberately separate).
- Not changed: «не подтверждено вашими лекциями», «Из ваших лекций:», «Покрытие лекциями».

## Follow-ups
- TASK-155: render `label`/`removed` as plain text; `Card.Removed` checks the note only, not the material (a replaced part is not marked); any open-note binding must use IDs, never paths.
- Low (review): an unknown note locator kind makes the whole Ask fail (cannot happen today); a lesson with empty Key and empty Recording collapses in `Passages` (theoretical); `"section"` literal duplicated in knowledgebase; the stored heading check does not reject bidi (local DB write only).
- Frontend hint reads «Не найдено в лекциях и конспектах.» (existing style), Go text «Нет в ваших лекциях и конспектах».

## Changed existing tests
`wails/answers_test.go` TestAnswerViewJSON (new DTO fields, `Card` fake); `cases/tickets/fakes_test.go` (`note` flag); `cases/knowledgebase/notes_test.go` TestTextsAndExportWithNotes (Texts now includes notes, was the inverse); homeworkdoc `TestBuildTickets`/bounds (wording, note sources); `desktop/tests/tickets_test.go` (wording); cases/tickets `TestTicketCoveredByOwnNote`.

## Security
Auditor: no high/medium; one low (above). Reviewer: approve, no blockers; process finding fixed by this file.
