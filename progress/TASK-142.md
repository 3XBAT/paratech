# TASK-142: answer reader key scoped to the agent request

## Done
- `answer.NewRequest(question, excerpts, scope)`: scope 1..64 bytes of 0x21..0x7e, else `ErrInvalidRequest`. Key `ans:v2:` + SHA-256 over data, places, NUL + scope (still 71 chars). Stored `ans:v1:` answers still load.
- `answering.Ask(..., scope)`; a bad scope is `ErrUnavailable` (caller bug). Agent passes `ag:<thread>:<turn>` (`tools.run` sets `tools.reuse` on its value copy; `read` type unchanged, to avoid conflicts with TASK-171). Tickets pass constant `tk:v1` (app/tickets.go), so their behaviour is unchanged.
- Migration 0030 rebuilds `answers` with `GLOB 'ans:v[12]:*'` (0027 CHECK would have rejected v2); schema.sql updated; down keeps v1 rows only.
- ADR-0026 «Open» replaced by «Decided by the owner» (2026-10-26, five answers).

## Decisions to confirm
- Files outside the task list (forced): migration 0030, docs/contracts/schema.sql, app/tickets.go, test call sites (hubclient, input/wails, ticketset, cases/tickets, desktop/tests), migrate_test (29 -> 30). Ticket scope `tk:v1` is a coordinator decision.
- «Повторить» of a failed step (Retry) keeps the turn, so the answer key is the same (reuse); only Rerun (next turn) gives a fresh answer.

## Follow-ups
- A reader call paid under a v1 key and interrupted across the upgrade is paid once more (one-time).
- `MaxScopeBytes` 64 fits current 26-char thread IDs; a longer or non-ASCII ThreadID would make `answer_question` fail. Consider raising the bound or a test with the longest valid ThreadID.
- Passing the scope through `tools.reuse` is implicit; pass the step into `read` once TASK-171 has landed.
- Re-check the migration number 0030 against master at merge.

## Changed existing tests
- `tests/answering_sqlite_test.go`: Hub key prefix `ans:v1:` -> `ans:v2:` (new requests build v2); step 7 added.
- `adapters/sqlite/migrate_test.go`: version 29 -> 30.
- NewRequest/Ask callers in tests pass a scope.

## Security
Auditor: no findings. Reviewer: approve, no blockers.
