# TASK-431 — call links per lesson kind (core)

ADR-0032 «Addendum 2026-11-10», ADR-0038 §4, D-2. Bindings TASK-432, UI TASK-433, batch TASK-435, agent TASK-436/437 build on it.

## Done
- `domain/library/subject`: `CallKind` (general, lecture, seminar, lab), `ParseCallKind` (`ErrInvalidCallKind`), `CallKindOfLesson` (practice → seminar, empty/unknown → general), value `CallLinks` with `Of` and `Join` (kind's link, else general, else none; a seminar never takes the lecture link). `Subject.CallLinks`, `SetCallLinkOf`, `ClearCallLinkOf`; `SetCallLink`/`ClearCallLink`/`CallLink` mean the general link. `Restore` reads four strings; a stored value that no longer parses or is not canonical is read as no link (per kind). Extras: `CallKind.Valid`, `CallLinks.IsZero`, `String/Format/LogValue` (hosts only).
- Migration `0052_subject_call_links_by_kind.sql`: `call_link_lecture/seminar/lab TEXT NOT NULL DEFAULT ''` with the same 2048 CHECK as 0050; `call_link` untouched, nothing copied; down drops the three columns. `docs/contracts/schema.sql` mirrors it (contract test requires it).
- SQLite: queries/library.sql, `sqlcgen` (hand-edited, see below), `library.go` mapping both ways.
- `cases/library`: `SetCallLinkOf`, `ClearCallLinkOf`, `OpenJoinLink(subject, lessonKind)` (`ErrNoCallLink` when Join is none), `CallLinkSets(ids)`; `ErrInvalidCallKind`. Old `CallLinks(ctx, ids)` stays until TASK-432. Same error order as before: bad kind / bad link before the subject is read, no version check, last save wins, no link text in logs or errors.
- Tests: domain tables (`CallKindOfLesson`, `Join` over 16 link sets x 6 kinds, set/clear per kind, Restore, Format/LogValue), use case tests, SQLite round trip + migration up/down, `desktop/tests/calllinks_sqlite_test.go` scenario; the ParseCallLink cross-check stays green.

## Decisions to confirm
- `Join` with no link returns `(CallLink{}, "")`; callers must test `link.IsZero()`, not the kind.
- Migration number 0052 taken at branch time; renumber at push if another PR took it (checked at push).
- `docs/contracts/schema.sql` changed (outside the task's file list, required by `TestMigrationMatchesContractSchema`).

## Follow-ups
- sqlc was NOT run: the auto-mode classifier refused `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate` (twice: implementer and coordinator). `sqlcgen/library.sql.go` and `models.go` were edited by hand; the reviewer compared them with what sqlc produces and found them equal, but the drift check (CI / verify.sh SQLC_CHANGED) must confirm.
- TASK-432 must map `library.ErrInvalidCallKind` in `input/wails/library_calllink.go` (reason code) and rely on `link.IsZero()`.
- Low: early `ErrInvalidCallKind` in `cases/library/calllink.go` is the bare library sentinel while the in-aggregate path wraps both; callers check only the library one. Optional symmetry.
- Low: SQLite bad-value test does not show one bad column leaves another kind's good link readable (domain `TestRestoreCallLinks` does). Optional.
- Low: no test pins the order when kind and link are both invalid (kind first).

## Changed existing tests
`adapters/sqlite/migrate_test.go`: new `subjectCallLinksByKindVersion = 52`; `TestMigrateIsIdempotent` expects it (schema version 51 → 52); comment on `deadlineEventUpdatesVersion` no longer says «newest».

## Review
Reviewer (opus): approve, no blocker/high/medium, four lows and one info (above). security-auditor (opus): no findings (placeholders and columns checked one by one in the hand-edited sqlc code).
Security: every kind passes `ParseCallLink` on write and read; the opener re-checks; `lessonKind` is never used to build a key or query; nothing logs link text.
