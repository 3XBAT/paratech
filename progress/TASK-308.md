# TASK-308 — subjects core: a removed subject is purged completely after 3 days

## Done
- Domain `library/subject`: `PurgeAt()` (removedAt + 72 h) and `PurgeCutoff(now)`; `PurgeDue` uses them, so `Restore` and the purge share one boundary (exactly +72 h: purge due, `ErrRestoreExpired`).
- New use case `cases/subjectpurge` (own ports, no other use case, ARCH-20): `Purge(ctx) (int, error)`. For each removed subject past the window, in order: its notes, its ticket sets, its files, then the subject row; aliases, folders and teachers go by CASCADE with the row. Every step is one guarded SQL statement (`removed_at IS NOT NULL AND removed_at <= cutoff` repeated in each), idempotent and crash-safe; a failing file or subject does not stop the others (joined error, count). A mutex serializes the startup cleanup, the daily loop and the name check.
- Files: bytes and cached text are deleted only through `BlobStore.Delete` and only when `MaterialRepository.DigestInUse` says nothing else uses them (another material, or a pending `send_work_email` that pinned the digest); the subject is re-checked (`StillDue`) just before the delete; the row goes after the bytes (a crash leaves a row that the next run finishes). A file that must survive (`KeptElsewhere`: a note of another subject holds it, or a `works` row was made from it) is moved to «Входящие» with its status kept.
- Migration: none. sqlite statements in `adapters/sqlite/library_purge.go`, `notes_purge.go`.
- Wiring (`app/purge.go`, `core.go`): cleanup "removed subjects past the restore window" at start and loop "subject purge" every 24 h.
- Supervisor addition 1 (name freed): `cases/library` has an optional port `ExpiredSubjects`; `namesFor` (create, alias, schedule create/alias, derived ID) purges first when the holder of the name is a removed subject past its window, then re-evaluates. A purge failure of that subject is `ErrUnavailable`, never «удалён». Boundary tests: −1 s still `RemovedNameError`, +72 h free.
- Supervisor addition 2: `library.RemovedSubjects(ctx)` → removed subjects inside the window with `RemovedAt` and `PurgeAt`, most recent first. No binding, no UI.
- Tests: fakes (every relation, failure at each of 12 steps and rerun, second run no-op, one subject failing does not stop another, shared digest, note-held and work-source file moved to Inbox, restored subject kept), real SQLite adapter, library name-freeing, app wiring, scenario `desktop/tests/subject_purge_sqlite_test.go` (day-3 boundary, assignments/recordings/lessons/work stay, shared file in Inbox, name creatable again).

## Decisions to confirm
- Ticket sets of the purged subject are deleted (no FK; a schedule subject gets the same derived ID again and would inherit them).
- A file that is the source of a work (`works.material_id`, ON DELETE CASCADE) is NOT purged but moved to «Входящие» with its bytes: otherwise the student's written paper and rounds would be destroyed, which ADR-0027 does not list. Works are not mentioned in the ADR.
- A file held by a note of another subject is kept for a note in any status (also of a removed subject), not only an active one as the ADR says: `note_parts` RESTRICT would block otherwise.
- Answers (`answers.subject_id`, no FK), `works.topic_subject_id`, inspection candidates, agent journal lines stay (other contexts). A schedule subject recreated under the same derived ID can pick up those rows again (probably intended).
- Re-upload of the same bytes during the purge can leave a material in Inbox without its bytes until the next upload (documented in `purgeMaterial`).

## Follow-ups
- A UI task: «Недавно удалённые» with «Вернуть» over `library.RemovedSubjects` (binding + page).
- TASK-290/291 (note revisions): add revisions to the `MaterialDigestInUse` query and a revisions step in `subjectpurge/purge.go`; there are no revisions in the schema today.
- Reviewer low: add `StillDue` before the move to «Входящие» (placement only); an aggregate method `material.Unfile(now)` instead of the `Restore → MoveToInbox → Remove` sequence in `moveToInbox` (ARCH-22, outside this task's file list); `Index` accessor for owner-by-key in `holdsExpired`.
- Reviewer/auditor low: a finished paper of a work (`Writing.File.Material`, filed in the subject's «ДЗ») is purged with the subject and the work card then shows no file (worksend handles NotFound); decide whether to keep it too (`MaterialKeptElsewhere` + `json_extract` on `writing`). A file held only by notes of two removed subjects ends up in Inbox and outlives both purges (retention low). Answers of a purged subject keep their quotes.
- Changes outside the task's file list: `domain/library/subject` (PurgeAt/PurgeCutoff), `cases/library` (supervisor additions).

## Changed existing tests
- `app/purge_test.go`: `purgeLoop` takes the step name now.
- `app/prompts_test.go`: loop names now include "subject purge".
- `adapters/sqlite` purge test and `TestPurgeStoreGuardKeepsARestoredSubject` (new in this task, revised after review): the work's source file now survives.

## Security
security-auditor: no high/medium; lows L1 (shared file between two removed subjects), L2 (answers stay), L3 (documented import race). Reviewer: round 1 changes requested (HIGH works cascade, MEDIUM bytes before re-check, fixed), round 2 approve.

## Verification
go vet and GOOS=windows go vet ./desktop/...; go test -race on domain/library, cases/subjectpurge, cases/library, adapters/sqlite, input, app, desktop/tests (architecture); golangci-lint v2.14.0 0 issues; sqlc generate drift-free (v1.31.1). Master (TASK-289, TASK-345) merged in; cases/library, input and app re-run after the merge.
