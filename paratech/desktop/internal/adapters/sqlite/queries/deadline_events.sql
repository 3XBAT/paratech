-- The Google deadline event of an assignment after a deadline change (TASK-276, ADR-0029 addendum 2): whether it
-- exists and the queue of events to update, the ADR-0018 pattern of deadline_event_removals.

-- An assignment has an event when its create_deadline_event proposal was executed and no removal is waiting.
-- name: AssignmentHasDeadlineEvent :one
SELECT CAST(EXISTS (SELECT 1 FROM proposals p
                    WHERE p.assignment_id = sqlc.arg(assignment_id) AND p.kind = 'create_deadline_event'
                      AND p.status = 'EXECUTED')
            AND NOT EXISTS (SELECT 1 FROM deadline_event_removals r WHERE r.assignment_id = sqlc.arg(assignment_id))
       AS INTEGER) AS has_event;

-- A repeated change keeps one row and moves it to the latest time.
-- name: UpsertDeadlineEventUpdate :exec
INSERT INTO deadline_event_updates (assignment_id, created_at) VALUES (sqlc.arg(assignment_id), sqlc.arg(created_at))
ON CONFLICT (assignment_id) DO UPDATE SET created_at = excluded.created_at;

-- The deadline events left to update, oldest first (the calendar sync).
-- name: ListDeadlineEventUpdates :many
SELECT assignment_id FROM deadline_event_updates ORDER BY created_at, assignment_id LIMIT sqlc.arg(max_rows);

-- name: DeleteDeadlineEventUpdate :exec
DELETE FROM deadline_event_updates WHERE assignment_id = sqlc.arg(assignment_id);
