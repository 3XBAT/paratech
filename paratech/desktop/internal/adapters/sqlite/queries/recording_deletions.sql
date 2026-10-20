-- The deletion of a recording with what was made from it (TASK-095, ADR-0018). The rows go in one transaction after
-- the files; summaries and lecture texts are keyed by the transcript id.

-- What the recording holds, for the confirmation.
-- name: RecordingDeletionCounts :one
SELECT
    (SELECT COUNT(*) FROM transcripts t WHERE t.recording_id = sqlc.arg(recording_id)) AS transcripts,
    (SELECT COUNT(*) FROM transcript_parts p WHERE p.recording_id = sqlc.arg(recording_id)) AS parts,
    (SELECT COUNT(*) FROM summaries s JOIN transcripts t ON t.id = s.id
     WHERE t.recording_id = sqlc.arg(recording_id)) AS summaries,
    (SELECT COUNT(*) FROM lecture_texts l JOIN transcripts t ON t.id = l.id
     WHERE t.recording_id = sqlc.arg(recording_id)) AS lecture_texts;

-- The assignments found in the transcripts of the recording, with whether a draft or any email exists.
-- name: ListRecordingAssignments :many
SELECT a.id, a.title,
       CAST(EXISTS (SELECT 1 FROM homework_drafts d WHERE d.assignment_id = a.id) AS INTEGER) AS has_draft,
       (SELECT COUNT(*) FROM deliveries v WHERE v.assignment_id = a.id) AS deliveries
FROM assignments a
JOIN transcripts t ON t.id = a.transcript_id
WHERE t.recording_id = sqlc.arg(recording_id)
ORDER BY a.created_at, a.source_index, a.id;

-- name: DeleteSummariesOfRecording :exec
DELETE FROM summaries WHERE id IN (SELECT t.id FROM transcripts t WHERE t.recording_id = sqlc.arg(recording_id));

-- name: DeleteLectureTextsOfRecording :exec
DELETE FROM lecture_texts WHERE id IN (SELECT t.id FROM transcripts t WHERE t.recording_id = sqlc.arg(recording_id));

-- The draft of an assignment chosen for deletion; its versions go with it (CASCADE).
-- name: DeleteDraftOfAssignment :exec
DELETE FROM homework_drafts WHERE assignment_id = sqlc.arg(assignment_id);

-- An assignment chosen for deletion, decided in the same transaction. A delivery referencing it fails the delete
-- (ON DELETE RESTRICT); the use case never chooses such an assignment.
-- name: DeleteChosenAssignment :exec
DELETE FROM assignments WHERE id = sqlc.arg(id);

-- name: InsertDeadlineEventRemoval :exec
INSERT INTO deadline_event_removals (assignment_id, created_at) VALUES (sqlc.arg(assignment_id), sqlc.arg(created_at))
ON CONFLICT (assignment_id) DO NOTHING;

-- A job of the recording, only once it is finished (statuses is the list of terminal statuses).
-- name: DeleteFinishedJob :exec
DELETE FROM jobs WHERE id = sqlc.arg(id) AND status IN (sqlc.slice('statuses'));

-- The deadline events left to remove, oldest first (the calendar sync).
-- name: ListDeadlineEventRemovals :many
SELECT assignment_id FROM deadline_event_removals ORDER BY created_at, assignment_id LIMIT sqlc.arg(max_rows);

-- name: DeleteDeadlineEventRemoval :exec
DELETE FROM deadline_event_removals WHERE assignment_id = sqlc.arg(assignment_id);
