# TASK-183 — agent history as request/reply pairs, core

## Done
- `values.HistoryTurn.AgentReply` (exactly one part, <= 2 KiB, UTF-8, no NUL, non-blank); `hubclient.turnOf` maps it to `AgentTurn_AgentReply`. Storage unchanged: the reply is built from stored steps, never stored.
- `thread/reply.go` (no IO): `Reply` builds the text with fixed labels `reply:`, `plan:`, `cannot_do:`, `asked:`, `actions:`; `NoReply` = "no answer: the request ended without a reply"; cut to 2 KiB on a rune boundary. Only validated planner texts (reply.text, show_plan.summary, cannot_do parts), fixed ask_questions line, tool names with ok/error. Never results, reader texts, titles, IDs.
- `Window`: earlier requests as [texts..., agent_reply], whole pairs newest first: <= 12 requests, <= 24 KiB, <= 64 turns per request (`MaxRequestTurns`). Every earlier request gets a reply (all-or-nothing marker rule of TASK-181). Pairs only when the current request starts with a student text.
- ADR-0022 addendum «Planner sees».

## Deployment
The desktop with this task needs a Hub with TASK-181 (Hub first, as for catalogs 2 and 3). No gateway by catalog version; an old Hub answers invalid_argument.

## Decisions to confirm
- A card closed by `Abandon` (not_done) counts as shown; other error results mean refused and the text is left out. A call without a result is reported as `error`.
- A request with actions but no card: `NoReply` plus the `actions:` line.
- Newlines in card texts become spaces so labels are the only line starts.

## Changed existing tests (scope note)
Outside the file list, tests only: `cases/agentturn/{progress,rerun,service}_test.go` (TestAttemptsOfANewRequestEndWithItsText lives there; histories now include replies, pinned exactly) and `desktop/tests/agent_sqlite_test.go` (earlier turns may be an agent_reply). `thread/calls_test.go` TestWindow updated. Nothing weakened. No production code in cases/agentturn or input/agent changed.

## Follow-ups
- `adapters/sqlite.AgentHistory.Append` would store an AgentReply turn as an empty student row (nothing appends replies today); add a guard.
- Comment on `historyLimit` in cases/agentturn/service.go is slightly stale.
- Reviewer notes: not_done of a never-shown card / outcome_unknown of a shown card make the reply slightly inaccurate; result lookup only in the entry right after the echo; current request > 64 turns is not capped (pre-existing); agent_sqlite_test could assert a positive agent_reply.

## Security
Reviewer: approve, no blocker/high/medium. Security auditor: no findings (Hub side not audited here).
