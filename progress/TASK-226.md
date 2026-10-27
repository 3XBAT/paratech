# TASK-226 — work remembers last triage problem and send time

## Done
- Domain `work`: `Problem` codes (offline, quota, refused, no_text, source_gone), `NoteProblem`, `RecordSent`; problem is cleared by any move out of triage/failed; invariants checked in Restore.
- Migration `0035_work_problem_sent.sql`: `works.last_problem` (CHECK on codes), `works.sent_at` (ms). Mirrored in `docs/contracts/schema.sql`; sqlc regenerated (only works files changed). Existing JSON fields could not hold it.
- sqlite `WorkRepository` saves/restores both; `LastSent` reads the newest SENT delivery of the work's `send_work_email` proposal (key `proposal:<id>`).
- `cases/turnkey`: optional port `WorkSends`; `ask()` records the problem code; `Work`/`WorkOf` sync `sentAt` from deliveries (idempotent, failure only logged).
- Binding `TurnkeyWork`: `lastProblem`, `lastMessage` (fixed text), `sentAt` (RFC 3339 UTC). ADR-0025 addendum.

## Decisions to confirm
1. sentAt is synced on read (not by the delivery job) to avoid editing senddelivery / two aggregates in one tx (ARCH-23). The save bumps the work version once per send; an open outline editor gets «Работа изменилась» once.
2. `lastMessage` added beyond the task so the frontend need not copy texts; droppable.
3. A send does not change `updatedAt`.
4. Only SENT deliveries set sentAt; UNKNOWN outcome does not until reconciled.
5. A work that reached `failed` after 3 refusals keeps `refused` but `WorkOf` skips failed works, so after restart it still shows a fresh «Разобрать задание» (TASK-227 to confirm).

## Follow-ups (reviewer minors)
- Concurrent reads may log a harmless version-conflict WARN in syncSent.
- Optionally move `LastSent` into a separate sqlite type and comment the key format next to the proposal key builder.

## Changed existing tests
- `sqlite/migrate_test.go` version 34 -> 35; `works_topic_test.go` TestMigration31KeepsWorks migrates up to 35 first; `wails/turnkey_test.go` TestTurnkeyWorkJSON expects the new JSON tail.

## Security
Auditor: no findings. Reviewer: approve, 3 minors (above). Implementer used one `python -c` edit of works.sql (result correct).
