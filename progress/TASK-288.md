# TASK-288 — subjects core: soft removal with notes and files, «Вернуть» for 3 days

## Done
- Domain `library/subject`: status active/removed as a transition table (ARCH-19), `removedAt`, `Remove(now)` / `Restore(now)`, `RestoreWindow` = 72 h, `PurgeDue(now)` (true from exactly removedAt+72h; `Restore` then answers `ErrRestoreExpired`). Every mutation of a removed subject (aliases, folders, teachers) is `ErrRemoved`. `RemoveAlias` now returns `(bool, error)`. The «ссылка» setter does not exist yet (TASK-259): it must call `active()`.
- Migration `0049_subject_removal.sql`: `subjects.removed_at INTEGER NULL` (CHECK > 0), mirrored in `docs/contracts/schema.sql`. Number to be re-checked at push time.
- Visibility is filtered in the SQLite queries (subjects, materials, notes by subject/all/recording/lesson), so every reader hides a removed subject with no row rewritten: lists, KB, search, answers/Passages, subjectmatch, agent tools, mention candidates. Exception: a material of a removed subject that is a part of an active note of another (active) subject stays visible. `GetMaterialBySHA256` is unfiltered (import dedupe).
- `cases/library`: `DiscardSubject(ctx, id, expectedVersion)` → `Discarded{Subject, Notes, Files}` (Files = active materials not parts of any active note); `RestoreSubject` (after the window `ErrRestoreExpired`); `RemoveSubject` (agent Undo) unchanged, still `ErrHasNotes`.
- A removed subject keeps its name and aliases: create/alias/schedule paths give `*RemovedNameError` (unwraps to `ErrRemovedNameTaken` → `ErrNameTaken`) with the removed subject. Schedule skip rows carry `ScheduleSkipped.Removed` (Reason stays `name_taken`, so `input/wails` is unchanged).
- Tests: domain, use case, sqlite adapter, scenario on real SQLite (`desktop/tests/subject_removal_sqlite_test.go`: remove → hidden → restore → as before; expiry).

## Decisions to confirm
- Re-uploading the same bytes as a file hidden under a removed subject moves that file to «Входящие» (not a dead end) — it then does not return with «Вернуть». Alternative: keep it where it is.
- `ScheduleSkipped.Removed` field instead of a new skip reason (a new reason would fall through the wails whitelist).
- The cross-subject exception applies to every loader of the material by id, not only inside that note (matches ADR wording).

## Follow-ups
- TASK-289: Wails binding (`DiscardSubject`/`RestoreSubject`, warning with N and M, «Вернуть», `expired`), plus `removedSubjectId` on `ScheduleSkippedRow` (`input/wails/library_schedule.go`) and the «Вернуть» button for skipped schedule rows.
- TASK-308: purge after 3 days.
- Ticket sets of a removed subject are not filtered (`cases/tickets` List/Set/Confirm); recordings whose free subject string equals the removed name stay in the KB under the bare string during the window (ADR-0027: other context) — note in the TASK-289 QA.
- `addScheduled` can offer «Вернуть» when one alias is held by a removed and another by an active subject (reviewer low); `scheduleNameTaken` with two different removed holders returns the first (low).
- Queued material jobs whose material becomes hidden see «not found»; `ScheduleSubjects` may still propose a string equal to a removed name (skipped as name_taken with `Removed`).
- The knowledge base still drops note parts filed under another subject (earlier rule).
- Nested folders do not exist in the domain yet; visibility goes by the material's subject, so they are covered when they come.

## Changed existing tests
- `adapters/sqlite/migrate_test.go`: newest version 49.
- `adapters/sqlite/subjects_teacher_test.go`: the 0044 test migrates to newest before reading through the repository (queries now select `removed_at`).
- `domain/library/subject/subject_test.go`: `RemoveAlias` new result.
- `cases/library/fakes_test.go`, `notes_test.go`: fakes gained `GetAny`/`ListRemoved`, hide removed subjects.

## Security
security-auditor: no high/medium. Lows: re-upload moves a hidden file to Inbox (decision above); tickets not filtered (follow-up); cross-subject exception is per material (matches ADR). Reviewer: round 1 changes requested (schedule path medium, fixed), round 2 approve.

## Verification
go vet and GOOS=windows go vet ./desktop/...; go test -race on domain/library, cases, adapters/sqlite, input, app, pkg/migrations, tests (architecture); golangci-lint v2.14.0 0 issues; sqlc generate drift-free (v1.31.1).
