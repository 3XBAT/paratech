-- The agent panel (TASK-116): chats (threads) with their current request, the history sent to the planner and the
-- action journal. Threads and journal entries use the optimistic-concurrency protocol of ADR-0004(d).

-- name: GetAgentThread :one
SELECT * FROM agent_threads WHERE id = sqlc.arg(id);

-- The current chat (ADR-0028): the unarchived one active last; ties go to the newer id.
-- name: GetCurrentAgentThread :one
SELECT * FROM agent_threads
WHERE archived_at IS NULL
ORDER BY last_active_at DESC, id DESC
LIMIT 1;

-- Every chat: the unarchived ones by their last activity, newest first, then the archived ones in the same order.
-- (Comments of this file stay ASCII: sqlc rewrites queries by byte offset.)
-- name: ListAgentThreadIDs :many
SELECT id FROM agent_threads
ORDER BY archived_at IS NOT NULL, last_active_at DESC, id DESC;

-- A never-stored thread (version 0) is inserted with version 1.
-- name: InsertAgentThread :exec
INSERT INTO agent_threads (id, title, turns, history_from, seen, archived_at, last_active_at, version, created_at,
                           updated_at)
VALUES (sqlc.arg(id), sqlc.arg(title), sqlc.arg(turns), sqlc.arg(history_from), sqlc.arg(seen), sqlc.arg(archived_at),
        sqlc.arg(last_active_at), 1, sqlc.arg(created_at), sqlc.arg(updated_at));

-- Compare-and-swap on the loaded version.
-- name: UpdateAgentThread :execrows
UPDATE agent_threads
SET title          = sqlc.arg(title),
    turns          = sqlc.arg(turns),
    history_from   = sqlc.arg(history_from),
    seen           = sqlc.arg(seen),
    archived_at    = sqlc.arg(archived_at),
    last_active_at = sqlc.arg(last_active_at),
    version        = version + 1,
    updated_at     = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- name: GetAgentTurn :one
SELECT * FROM agent_turns WHERE thread_id = sqlc.arg(thread_id) AND number = sqlc.arg(number);

-- The current request is replaced as a whole in the transaction of its thread's compare-and-swap.
-- name: DeleteAgentTurn :exec
DELETE FROM agent_turns WHERE thread_id = sqlc.arg(thread_id) AND number = sqlc.arg(number);

-- name: InsertAgentTurn :exec
INSERT INTO agent_turns (thread_id, number, status, resume_status, stop_reason, scope, mentions, steps, writes,
                         paid_unplanned, planner_input, question_rounds, step_open, retry, proposed_plan, approved_plan,
                         used, open_calls, created_ids, started_at, updated_at)
VALUES (sqlc.arg(thread_id), sqlc.arg(number), sqlc.arg(status), sqlc.arg(resume_status), sqlc.arg(stop_reason),
        sqlc.arg(scope), sqlc.arg(mentions), sqlc.arg(steps), sqlc.arg(writes), sqlc.arg(paid_unplanned), sqlc.arg(planner_input),
        sqlc.arg(question_rounds), sqlc.arg(step_open), sqlc.arg(retry), sqlc.arg(proposed_plan),
        sqlc.arg(approved_plan), sqlc.arg(used), sqlc.arg(open_calls), sqlc.arg(created_ids), sqlc.arg(started_at),
        sqlc.arg(updated_at));

-- Only the current request of a thread can be unfinished: a new request cancels a waiting one (thread.StartTurn), so
-- the stored rows of earlier requests follow.
-- name: CancelEarlierAgentTurns :exec
UPDATE agent_turns
SET status = 'CANCELLED', resume_status = '', stop_reason = '', step_open = 0, updated_at = sqlc.arg(updated_at)
WHERE thread_id = sqlc.arg(thread_id) AND number < sqlc.arg(number)
  AND status NOT IN ('DONE', 'FAILED', 'CANCELLED');

-- Threads whose current request was running (startup recovery).
-- name: ListRunningAgentThreadIDs :many
SELECT t.id FROM agent_threads t
JOIN agent_turns u ON u.thread_id = t.id AND u.number = t.turns
WHERE u.status IN ('PLANNING', 'EXECUTING')
ORDER BY t.id;

-- name: LastAgentStepSeq :one
SELECT CAST(COALESCE(MAX(seq), 0) AS INTEGER) AS seq FROM agent_steps WHERE thread_id = sqlc.arg(thread_id);

-- name: InsertAgentStep :exec
INSERT INTO agent_steps (thread_id, seq, turn, entry, body, created_at)
VALUES (sqlc.arg(thread_id), sqlc.arg(seq), sqlc.arg(turn), sqlc.arg(entry), sqlc.arg(body), sqlc.arg(created_at));

-- The newest entries of the requests from a number on, newest first (the adapter returns them oldest first). Entries
-- of dropped requests are left out here, so they never take a place under the limit (ADR-0028).
-- name: ListAgentSteps :many
SELECT s.* FROM agent_steps s
WHERE s.thread_id = sqlc.arg(thread_id) AND s.turn >= sqlc.arg(from_turn)
  AND NOT EXISTS (SELECT 1 FROM agent_turns u WHERE u.thread_id = s.thread_id AND u.number = s.turn AND u.dropped_by > 0)
ORDER BY s.seq DESC
LIMIT sqlc.arg(max_rows);

-- name: GetAgentAction :one
SELECT * FROM agent_actions WHERE id = sqlc.arg(id);

-- name: GetAgentActionByCall :one
SELECT * FROM agent_actions
WHERE thread_id = sqlc.arg(thread_id) AND turn = sqlc.arg(turn) AND call_id = sqlc.arg(call_id);

-- A never-stored entry (version 0) is inserted with version 1.
-- name: InsertAgentAction :exec
INSERT INTO agent_actions (id, thread_id, turn, call_id, tool, status, intent, compensation, target_version,
                           undo_refusal, proposal, version, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(thread_id), sqlc.arg(turn), sqlc.arg(call_id), sqlc.arg(tool), sqlc.arg(status),
        sqlc.arg(intent), sqlc.arg(compensation), sqlc.arg(target_version), sqlc.arg(undo_refusal), sqlc.arg(proposal), 1,
        sqlc.arg(created_at), sqlc.arg(updated_at));

-- Compare-and-swap on the loaded version; the call and its intent never change.
-- name: UpdateAgentAction :execrows
UPDATE agent_actions
SET status         = sqlc.arg(status),
    compensation   = sqlc.arg(compensation),
    target_version = sqlc.arg(target_version),
    undo_refusal   = sqlc.arg(undo_refusal),
    version        = version + 1,
    updated_at     = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- Entries of the same instant follow the insert order (rowid; ids are random), so "undo the request" goes exactly
-- in reverse.
-- name: ListAgentActionsByThread :many
SELECT * FROM agent_actions
WHERE thread_id = sqlc.arg(thread_id)
ORDER BY created_at DESC, rowid DESC
LIMIT sqlc.arg(max_rows);

-- The journal of the panel: the newest entries of every thread.
-- name: ListRecentAgentActions :many
SELECT * FROM agent_actions
ORDER BY created_at DESC, rowid DESC
LIMIT sqlc.arg(max_rows);

-- name: ListUnfinishedAgentActions :many
SELECT * FROM agent_actions WHERE status IN ('PENDING', 'UNDOING') ORDER BY created_at, rowid;
