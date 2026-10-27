# TASK-236 — subject teacher: Wails binding

## Done
- `input/wails/library_teacher.go`: `SetSubjectTeacher(subjectId, version, name, email)` and `ClearSubjectTeacher(subjectId, version)` return `{problem, field, subject}`. Problems: `""`, `invalid` (field `name`/`email`, empty for a bare invalid), `changed`, `not_found`, `failed`. The subject is returned only on success.
- `LibraryUseCase` got `SetTeacher`/`ClearTeacher` (`library.Service` already has them, so `internal/app` is unchanged). `LibrarySubject` DTO now carries `version` and `teacher {name, email}` (null when none). `version` is beyond the literal text but the page must send it back.
- `bindings/library.ts`: `setSubjectTeacher`, `clearSubjectTeacher`, `parseTeacher`, `parseTeacherResult`; defensive (malformed/unknown/thrown reply is `failed`). No UI texts.
- Logging: invalid is not logged; changed/not_found log a code; failed logs only `cause="local storage"` or `unexpected`. The raw cause, ids, name, e-mail are never logged; nothing formats a `*Subject`. Tests assert a leaky cause does not reach the log.
- Support bundle (`cases/supportbundle`) holds about/settings/capture/logs, no DB or subject rows; the teacher can reach it only via logs, which the new code does not write.
- `ErrInvalidRequest` (bad subject id) maps to `not_found`.
- ADR-0030 Open B-1..B-4: nothing decided.

## Follow-ups
- `LibraryService.fail` (library.go) still logs the raw cause for older calls (`Subjects` etc.), which now return the teacher; storage errors are not expected to carry row data. Consider the same fixed-cause logging.
- `subject.Subject` still prints the teacher under `%v` (TASK-235 follow-up: add `String()`/`LogValue`).
- TASK-238: take `version`/`teacher` from `Subjects()` (`SubjectOverview` has neither); reload the list after `failed` as well as `changed`; render teacher as text nodes only; reword the stale comment in `input/wails/subjectoverview.go:65` ("No teacher: it waits for the owner's decision").

## Changed existing tests
- `frontend/test/library.test.mjs`: the bound-method-names test also reads `library_teacher.go` (the methods moved to a new file).

## Security
Auditor: no high/medium. Reviewer: approve (lows above). Lint run on `./desktop/internal/input/wails/...`; Windows vet, Go tests, arch test, npm test (836) and build pass.
