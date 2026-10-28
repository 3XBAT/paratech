# TASK-313: «Выполнено» — assignments.done_at, MarkDone / MarkUndone

## Done
- `coursework/assignment`: `MarkDone(now) (changed, err)`, `MarkUndone(now) (changed, err)`, `Done()`, `DoneAt()`, `State.DoneAt` (zero = not done). A deleted assignment gives `ErrDeleted` for both; a repeat mark changes nothing (`changed = false`, no save needed). Edit, EditByStudent, Correct, Delete and Restore («Вернуть») keep the mark. `Restore(State)` refuses `DoneAt` before `CreatedAt`.
- `MarkDone` with a clock set back before `CreatedAt` stores `CreatedAt` as the done time (so the saved row stays loadable); `updatedAt` is `now`.
- Migration `0046_assignment_done.sql`: `assignments.done_at INTEGER` (nullable); Down drops the column. `docs/contracts/schema.sql` mirrors it.
- Adapter `sqlite/assignments.go`: `done_at` on insert and in the compare-and-swap update; read in `assignmentFromRow` (all loaders are `SELECT *`, including the retention purge). sqlc v1.31.1 regenerated, `sqlc diff` clean.
- Readers («Сегодня», list) unchanged: TASK-314. Nothing touches Google (ADR-0033 О-13: decided nothing).

## Decisions to confirm
- `done_at` is INTEGER unix ms, not TEXT as the task text says: every time column in the schema is INTEGER ms and the adapter helpers (`jobNullMs`/`jobFromNullMs`) work on that.
- Migration number 0046: 0045 was taken by TASK-294 (PR #330) at push time; renumbered during the merge of origin/master.

## Follow-ups
- TASK-314 (readers, filter, use case) and TASK-320 (agent tool) build on `Done()`/`DoneAt()`.
- `Reextract` and `SetHints` are not changed for done assignments.

## Changed existing tests
- `adapters/sqlite/migrate_test.go`: `TestMigrateIsIdempotent` expects the newest version (`assignmentDoneVersion = 46`); `noteRecognitionVersion = 45` kept for TASK-294's test.

## Security
Security-auditor: no findings. Reviewer: approve; one Low (query alignment, fixed), one Low (INTEGER vs TEXT, recorded above).
