# TASK-266: storage of the reply line, chat summary and fork source (migration 0047, sqlite, agentturn)

## Done
- Migration `0047_agent_reply_summary_fork.sql`, only `ALTER TABLE ADD COLUMN` (no rebuild; rows, rowids, FKs untouched, old rows get NULL): `agent_turns.reply_line` (NULL or 1..2048 bytes); `agent_threads.summary_text` (1..8192 bytes), `summary_through` (>= 1), `summary_at` (its CHECK holds the all-or-none rule for the three), `forked_from` (1..128 chars, no FK). Mirrored verbatim in `docs/contracts/schema.sql`.
- Down drops `forked_from`, then `summary_at` (before the two columns its CHECK names), `summary_through`, `summary_text`, then `reply_line`. Loses: frozen reply lines (Window rebuilds them from the steps while kept), summaries (next compaction rebuilds), fork sources.
- sqlc v1.31.1: threads Insert/Update write the summary and fork columns, `InsertAgentTurn` writes `reply_line` (NULL when empty) on every Save, so a resumed STOPPED request clears it. `ListAgentSteps` LEFT JOINs `agent_turns` and returns `reply_line`; dropped requests still hidden.
- sqlite adapter: Restore rebuilds the summary with `ParseHistorySummary`; a partial summary triple, a text not in its trimmed form, an empty reply line or fork source, a bad line in `Since` are errors, never skipped.
- Domain (small): `thread.State.ForkedFrom` + accessor + Restore check (valid id, not itself) in `thread/fork.go`; `NeedsReplyLine()` in `thread/replyline.go` so the use case does not know what "final" is. `budget.go` untouched.
- `cases/agentturn`: `save` freezes the reply line (`FreezeReply` of stored + not yet appended entries) inside the transaction before `threads.Save`.
- Tests: migration on a v46 DB with data, Down/Up with 0047 data, roundtrip, CHECK table, 15 corrupt-row variants, resume clears the line, agentturn freeze for DONE/FAILED/CANCELLED/STOPPED, restart scenario in `desktop/tests`.

## Changed existing tests
`agent_migration_test.go`: the repository now reads the latest schema, so the 0040 tests migrate to latest before using it (Down goes through 0047); expectations unchanged. `migrate_test.go`: latest version 47.

## Decisions to confirm
1. A history-read failure while freezing: DONE/FAILED/CANCELLED fail the save (the DONE reply then rolls back and the request stops as «нет связи» via abort); STOPPED logs a warning (code, no text) and saves without a line, so abort/«Стоп» can always store the stop and nothing stays PLANNING (TASK-171). The next save retries.
2. ADR-0028 says reply_line is "written once when the request becomes final"; per TASK-265 a resumed STOPPED request loses its line and is frozen again at its real end. ADR wording to align.
3. Requests cancelled by a newer request (`CancelEarlierAgentTurns`) keep NULL; Window falls back to `Reply(steps)`. Once steps are purged (90 days) such a request has no line: the purge task must freeze or keep it.
4. Earlier-view mapping of the frozen line (`cases/agentchats/earlier.go` still ignores it; the line is in planner format `reply:`/`plan:`) is NOT decided here; separate decision before TASK-271.

## Follow-ups
- Nothing calls `Compact` or creates a fork yet (TASK-268, TASK-271); the Insert path of `forked_from` has no test until a fork constructor exists.
- One corrupt stored reply line fails `Since` for the whole chat and stops later requests as «нет связи»; consider a distinct stop reason (only tampering can cause it).
- `forked_from` CHECK counts characters, the other new ones bytes (harmless: ids are ASCII).
- `desktop/internal/app` tests take ~11 min under load (default 10-minute timeout exceeded in a combined run); CI may need a longer timeout.

## Security
Auditor: no findings. Summary and reply line stored by bound parameters only, size and charset checked in the domain and by SQL CHECK, never logged, never in provenance. Reviewer: approve, four lows (above).
