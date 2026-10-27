# TASK-235 — subject teacher (name, e-mail): core

## Done
- Domain `library/subject`: value object `Teacher` (`teacher.go`), `SetTeacher`/`ClearTeacher`/`Teacher()` on the aggregate (return `changed`; the same value or clearing none keeps the version), strict check in `Restore`. Errors: `ErrInvalidTeacher` with `ErrInvalidTeacherName`/`ErrInvalidTeacherEmail` wrapping it.
- Name: trimmed, 1-200 runes, valid UTF-8, no control, no `unicode.Cf` (bidi, zero-width), no U+2028/2029. E-mail: own copy of the `proposal.NewRecipient` rules (ARCH-11) plus trim.
- Migration `0037_subject_teacher.sql` (`teacher_name`, `teacher_email` TEXT NOT NULL DEFAULT '', length CHECKs, both-or-neither CHECK); `docs/contracts/schema.sql` mirrors it.
- `cases/library`: `SetTeacher(ctx, id, name, email, expectedVersion)`, `ClearTeacher(ctx, id, expectedVersion)`; mismatch or save conflict is `ErrSubjectChanged`. The use cases log nothing.
- Nothing is sent; the teacher is not in AgentBase or any Hub request (`TestDriverBaseCarriesNoTeacher`).

## Scope deviation
The task names `adapters/sqlite/subjects*.go` and `queries/subjects*.sql`; the subject repository lives in `adapters/sqlite/library.go` and `queries/library.sql`, so those were changed (sqlc v1.31.1 regenerated, `sqlc diff` clean).

## Decisions to confirm (ADR-0030 Open, not decided in code)
- B-4: one teacher per subject is built. If the owner wants lecturer and seminar teacher separately, `teacher` becomes a set keyed by lesson kind, `SetTeacher`/`ClearTeacher` take a kind, columns move to a `subject_teachers` table (new migration; 0037 becomes legacy). `NewTeacher` and its rules stay.
- B-1 (no teacher / no subject): affects only TASK-237. B-2, B-3: untouched.

## Follow-ups
- The e-mail check is a separate copy from `proposal.NewRecipient`; a cross-check test with one corpus (in `desktop/tests`) or a TASK-237 test would catch drift.
- `fmt %v` of a `*subject.Subject` prints the teacher (unexported nested field ignores `String()`); add `String()`/`LogValue` on `Subject` before any code logs a subject.
- A future display name in a To header must go through `net/mail.Address`/`mime.QEncoding`.
- Strict `Restore`: tightening the rules later needs a data migration.
- TASK-236: Wails error mapping must not log the raw `ErrUnavailable` cause with row data; check the support bundle for `subjects` rows.

## Changed existing tests
- `adapters/sqlite/migrate_test.go`: expected schema version 37.
- `tests/agent_driver_test.go`: the test subject "Физика" now has a fake teacher in `newDriverEnv`, so all driver tests run with personal data present.

## Verification note
`TestLogOmitsTheDataDir` (app) timed out once in a long combined run (343 s); passes alone (6.7 s), load-related.

## Security
Auditor: no high/medium. Lows recorded above (Subject `%v`, header display names). Reviewer: approve.
