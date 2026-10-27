# TASK-149 — aggregate library/note («Мой конспект»), migration, sqlite repository

## Done
- `shared.NoteID`; package `domain/library/note`: `New`/`Restore`, anchor (recording/lesson/day/undated, snapshot date + lesson kind code + title), `FallBackToDay`, `AddParts`, `ReplacePart`, `Reorder`, `Reanchor`, `Retitle`, `Remove`, `Restore`, sentinel errors, status table active<->removed. Names are neutral (`note`).
- Migration `0036_notes.sql` (`notes`, `note_parts`): FK to `materials` and `subjects` (RESTRICT), NO FK from `recording_id`/`lesson_id`; mirrored in `docs/contracts/schema.sql`; sqlc v1.31.1 regenerated.
- `adapters/sqlite/notes.go`: `Save` (version CAS), `Get`, `ListBySubject`, `ListByRecording`, `ListByLesson`; tests on real SQLite, including that purging a recording/schedule entry leaves notes untouched.
- `note` added to the `library` row of §12 in `docs/architecture.md` (one row per context). ADR-0027 untouched (still Proposed).
- Nothing wired in `app` (no port yet; TASK-150+).

## Decisions to confirm
- Title is required (empty after cleaning -> `ErrEmptyTitle`).
- Subject FK RESTRICT: a subject with notes cannot be deleted (`SubjectRepository.Delete` -> `ErrConflict`), like `materials`. Use cases (subject removal) must handle this.
- `AddParts` skips duplicates, refuses the whole call over 200 parts. `Remove`/`Restore` idempotent. Recording/lesson anchors need a date in the snapshot, so `FallBackToDay` always works.
- Date years limited to 2000–2100 (not in ADR/task).
- Lists return notes of any status; use cases (TASK-150..153) must filter on `Active()` so the knowledge base never sees removed notes.
- The lesson kind is a short ASCII code (the schedule `lesson.Kind` cannot be imported across contexts, ARCH-11); `note.Date` duplicates `agent/values.Date`; move to `library/values` if TASK-157+ needs it.

## Questions for the owner (depend on ADR-0027 Open, not decided here)
- Open 10 (limits): `MaxParts=200`, `MaxTitleRunes=120`, snapshot title 200 are the task's numbers, as constants and migration CHECKs; a different answer is a one-line change plus a migration.
- Open 1 (naming): code is neutral; the user-facing name is not in code.
- Undated lectures "ordered by the student" (ADR-0027): no ordering field yet; `ListBySubject` sorts by anchor date, undated last.
- No `RemovePart` method (not in the task's list); needed?

## Follow-ups
- `desktop/tests/schema_test.go`: add `notes` to the version-column test and note statuses/anchor kinds to the CHECK test; `domain/library/doc.go` glossary does not mention `note`.
- Not checked in the aggregate: that a part belongs to the note's subject (use case must, esp. if material IDs come from LLM output).
- Migration was 0035 on the branch; renumbered to 0036 after TASK-226 merged.
- `notesWithParts` runs one query per note; fine at this scale.

## Changed existing tests
- `adapters/sqlite/migrate_test.go`: `note_parts`/`notes` added to the table lists, schema version 35 -> 36 (new migration).
- `domain/shared/shared_test.go`: one `NoteID` row in the validator table.

## Security
Auditor: no findings. Parameterized sqlc queries; titles and snapshot cleaned by `values.CleanText`; IDs validated; no side effects. Reviewer: approve, lows only.
