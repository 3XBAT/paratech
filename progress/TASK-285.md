# TASK-285 — three teachers per subject (lecture, practice, lab)

## Done
- Domain `library/subject`: `TeacherRole` (`lecture`, `practice`, `lab`), `SetTeacher(role, name, email, now)`, `ClearTeacher(role, now)`, `Teacher(role)`, `Teachers()`. `NewTeacher` rules are unchanged; an unknown or empty role is `ErrInvalidTeacherRole` (wraps `ErrInvalidTeacher`). The same value in the same role does not bump the version. `Restore` is strict: bad data, an unknown role or a repeated role in any entry fails.
- Printing: `Subject`, `Teacher`, `Teachers`, `TeacherState` have `Format`/`LogValue` that never show the name or e-mail (`TestPrintingHidesTeachers` covers %v %+v %#v %s %q and slog text/JSON). This is the first part of TASK-286 (supervisor decision on TASK-235/236).
- Migration `0044_subject_teachers.sql`: table `subject_teachers(subject_id, role, name, email)` with the 0038 length checks, PK (subject_id, role), ON DELETE CASCADE. Up copies the old single teacher into all three roles and blanks the legacy columns. Down keeps one teacher per subject (lecture, else practice, else lab); it is lossy by design. `docs/contracts/schema.sql` updated. sqlc v1.31.1 regenerated, `sqlc diff` clean.
- Adapter: teachers are deleted and re-inserted in the same transaction as the version compare-and-swap, so a stale save cannot overwrite them.
- `cases/library`: `SetTeacher(ctx, subjectID, role, name, email, expectedVersion)`, `ClearTeacher(ctx, subjectID, role, expectedVersion)`; the role is checked before the subject is read.
- Wails: `SetSubjectTeacher(subjectId, version, role, name, email)`, `ClearSubjectTeacher(subjectId, version, role)`; invalid role is `invalid` with field `role`; `LibrarySubject.teachers {lecture, practice, lab}` (each null when empty). Logs carry only a code.
- TS `bindings/library.ts`: `parseTeachers`, role in both calls. «Как у лектора / семинариста» stays in the UI (TASK-238).
- The agent and Hub still get no teacher: `TestDriverBaseCarriesNoTeacher` now fills all three roles.

## Decisions to confirm
- Migration number 0044 (0043 was last on master at push time).
- `Down` is lossy (one teacher per subject).

## Follow-ups
- TASK-286 is smaller now: the `String`/`Format`/`LogValue` part is done here; left are `LibraryService.fail` fixed-cause logging and the stale comment in `input/wails/subjectoverview.go:65`.
- `GetSubject`/`ListSubjects` queries still select the legacy `teacher_name`/`teacher_email` columns (always empty after 0044, unread); drop them in a later migration.
- TASK-238: take `teachers` from `Subjects()`; render as text nodes only.

## Changed existing tests
- `domain/library/subject/teacher_test.go`, `cases/library/teacher_test.go`, `adapters/sqlite/subjects_teacher_test.go`, `input/wails/library_teacher_test.go`, `frontend/test/libraryTeacher.test.mjs`: rewritten from one teacher to a role argument and three roles.
- `adapters/sqlite/migrate_test.go`: schema version 44, new migration test.
- `tests/agent_driver_test.go`: `TestDriverBaseCarriesNoTeacher` fills three teachers.
- Invisible characters in test literals are written as `\u` escapes (staticcheck ST1018).

## Security
Auditor: no high/medium. Reviewer: approve, lows fixed (escapes restored in teacher_test.go). Verified: go vet (also GOOS=windows), go test -race of domain/library, cases (library, subjectview, homeworkflow, tickets, turnkey, knowledgebase), sqlite, input (wails, agent), app, migrations, desktop/tests; lint 0 issues; npm test (965) and build; sqlc diff clean.
