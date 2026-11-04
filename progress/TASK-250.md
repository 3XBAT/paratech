# TASK-250 — chat history retention: texts forever, technical details 90 days

## Done
- **Definition of «technical details»** (binding): `agent_steps` rows with entry `calls` and `results` (sealed planner calls, tool results) of requests that are final (DONE/FAILED/CANCELLED/STOPPED), not the current request of their chat (`number < agent_threads.turns`) and ended before now-90d (`agent_turns.updated_at < cutoff`, strict; exactly 90 days stays). Never touched: the `student` rows of `agent_steps` (the request text lives there, NOT in `agent_turns` as the task text said), `agent_turns`, `agent_threads` (summary, history_from), `agent_actions` (journal, undo data). Dropped requests are purged by the same rule, their texts stay. The current request of every chat keeps its steps, so Retry, «Продолжить», Rerun and the live card work after any time.
- Before the delete a request without `reply_line` gets it frozen (`thread.FrozenReply` = `Reply` plus validation), written only while NULL, in the same transaction; the delete also requires a non-NULL line and re-checks status/current/cutoff in SQL. An unreadable or invalid request is skipped untouched and logged by chat id, number and code.
- New `cases/agentretention` (`Purger.Purge`), ports in the use case; domain `thread.DetailsRetention/DetailsCutoff` (90 days, cutoff from the Clock port), `thread.EndedRequest` (store projection and keyset cursor), `thread.CardLines` (parser of the frozen line); sqlite `agentretention.go` + 4 sqlc queries (no migration); app: startup cleanup «agent history details past 90 days» and a daily loop (`wireAgentRetention`).
- Bounded: 50 requests per batch, 20 batches per run (both in Deps); idempotent; keyset cursor so a broken request does not stall the run.
- Earlier view: a request without `calls` is shown from its frozen line (`CardLines` mapped to the panel captions); requests with calls are computed as before.
- Docs: `agent_steps` comment in `docs/contracts/schema.sql`, one bullet in ADR-0028 «Retention».

## Decisions to confirm
1. The task text assumed the student text is in `agent_turns`; it is the `student` rows of `agent_steps`. They stay forever; only `calls`/`results` go.
2. Replies of purged requests show in the Earlier view from the frozen one-line-per-card form: line breaks inside a reply text become spaces; a `cannot_do` part containing « | » or «; our_part: » is split there. Lossless alternative: a migration with a second frozen column written by the purge (not done).
3. A skipped (broken) request does not fail the run: one warning a day.
4. `agent_turns.proposed_plan/approved_plan/used/created_ids` (plan tool names and short arguments) are kept forever; the ADR lists only `agent_steps`. Blank them in the same purge if the owner counts them as details (auditor low).

## Follow-ups
- `docs/architecture.md` (about line 239, list of agent use cases) should gain `cases/agentretention` (file not in the task list).
- `idx_agent_steps_created` is now unused; `agent_turns` has no `updated_at` index (fine at desktop size).
- Very long chats: reads are capped by rows (`HistoryReadLimit`, `earlierLimit`); after the purge the same cap reaches further back, so Fill/Overflow/Earlier may include older requests than before (more correct).
- The final-status list is repeated in 3 SQL queries (mirrors `Status.final()`); optional test that pins it.
- `thread.EndedRequest` could move to `domain/agent/values` (reviewer low).
- TASK-271 (fork) can rely on: every purged request has a non-NULL `reply_line` and a `student` row.
- The current request of an abandoned chat keeps its details forever (by design).

## Changed existing tests
`app/prompts_test.go`: expected loop names gain «agent history retention» (the loop list is in `prompts.go`). Nothing weakened or skipped.

## Security
Auditor: no high or medium; two lows (decision 4; current request of an abandoned chat never purged). SQL parameters only, logs carry chat id/number/code only, no LLM text reaches a side effect. Reviewer: approve, five lows (above).
