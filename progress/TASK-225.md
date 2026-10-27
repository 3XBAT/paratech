# TASK-225 — subject page data (core)

## Done
- Use case `cases/subjectview`: `Overview(subjectID)` (read-only) and `MarkOpened(subjectID)`. Ports declared in the use case, wired in `app/subjectview.go`.
- Domain rules in `domain/library/services/overview`: `Nearest` (up to 5 not-yet-passed deadlines), `Slots` (weekly schedule line, window -7/+14 days), `StatusOf` (recorded / transcribed / summaryReady / failed), `Fresh`, `FreshMaterials`.
- Wails: `LibraryService.SubjectOverview` and `MarkSubjectOpened` (no new service). DTO: subjectId, name, openedAt, deadlines, schedule, lessons (10 newest recorded), newMaterials. No teacher field.
- Last opening stored in `<dataDir>/subject-visits.json` via `adapters/visitfile` (atomic, 1 MiB cap, 1000 entries). No migration, no sqlc, no new dependency.
- UI flow for TASK-227: call `SubjectOverview`, then `MarkSubjectOpened` once shown.

## Decisions to confirm
- Last-opened in a file, not SQLite (avoids migration-number collision with TASK-226).
- Material is "new" by `updated_at` after last opening: also catches moves/links/restores (reviewer medium; `CreatedAt` would be narrower). A never-opened subject marks nothing.
- `Nearest` omits overdue assignments (as the today feed does); confirm the prototype's «Ближайшее» should not show them.
- Assignments match the subject by subject string only; list capped by `MaxListedAssignments`.
- Lessons come from the knowledge base: capture-failed recordings and lessons without a recording («записи нет») are not listed.

## Follow-ups
- Narrow window between `SubjectOverview` and `MarkSubjectOpened` (could return `at` and pass it back).
- `Overview` does a full `ListRecordings`; narrow the `Failures` port if slow.
- TASK-227 must render titles and names as text only.

## Changed existing tests
- `input/wails/shell_test.go`: `validCore` gets `SubjectOverview`. `settings_test.go`: one more incomplete-core case.

## Security
Auditor: no findings. Fixed path, ids validated and subject must exist, errors strip paths.
Reviewer: approve; the medium above is recorded as a decision.
