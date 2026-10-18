-- Live transcript parts (TASK-079). A row is replaced by (recording_id, seq): a rerun of the engine numbers from 0
-- again. The STT job deletes the rows of a recording once its transcript is stored.

-- name: UpsertTranscriptPart :exec
INSERT INTO transcript_parts (recording_id, seq, start_ms, end_ms, text)
VALUES (sqlc.arg(recording_id), sqlc.arg(seq), sqlc.arg(start_ms), sqlc.arg(end_ms), sqlc.arg(text))
ON CONFLICT (recording_id, seq) DO UPDATE
SET start_ms = excluded.start_ms,
    end_ms   = excluded.end_ms,
    text     = excluded.text;

-- One page in number order; the primary key serves the order.
-- name: ListTranscriptParts :many
SELECT seq, start_ms, end_ms, text FROM transcript_parts
WHERE recording_id = sqlc.arg(recording_id)
ORDER BY seq
LIMIT sqlc.arg(max_rows) OFFSET sqlc.arg(skip_rows);

-- name: CountTranscriptParts :one
SELECT COUNT(*) FROM transcript_parts WHERE recording_id = sqlc.arg(recording_id);

-- name: DeleteTranscriptParts :exec
DELETE FROM transcript_parts WHERE recording_id = sqlc.arg(recording_id);
