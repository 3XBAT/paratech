-- Recordings (PRD section 6.2). Writes run through DB.Do; the status machine stays in the aggregate, the queries only
-- store the result.

-- name: GetRecording :one
SELECT * FROM recordings WHERE id = sqlc.arg(id);

-- A never-stored recording (version 0) is inserted with version 1.
-- name: InsertRecording :exec
INSERT INTO recordings (id, lesson_id, status, started_at, stopped_at, heartbeat_at, max_duration_ms, duration_ms,
                        chunk_count, chunks_ref, audio_ref, failure_reason, version, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(lesson_id), sqlc.arg(status), sqlc.arg(started_at), sqlc.arg(stopped_at),
        sqlc.arg(heartbeat_at), sqlc.arg(max_duration_ms), sqlc.arg(duration_ms), sqlc.arg(chunk_count),
        sqlc.arg(chunks_ref), sqlc.arg(audio_ref), sqlc.arg(failure_reason), 1, sqlc.arg(created_at),
        sqlc.arg(updated_at));

-- Compare-and-swap on the loaded version (ADR-0004). The lesson, the cap, the chunk directory and the creation time
-- never change; lesson_id is left alone so that ON DELETE SET NULL of the lesson is not undone by a stale save.
-- name: UpdateRecording :execrows
UPDATE recordings
SET status         = sqlc.arg(status),
    started_at     = sqlc.arg(started_at),
    stopped_at     = sqlc.arg(stopped_at),
    heartbeat_at   = sqlc.arg(heartbeat_at),
    duration_ms    = sqlc.arg(duration_ms),
    chunk_count    = sqlc.arg(chunk_count),
    audio_ref      = sqlc.arg(audio_ref),
    failure_reason = sqlc.arg(failure_reason),
    version        = version + 1,
    updated_at     = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- name: ListRecordingsByStatus :many
SELECT * FROM recordings
WHERE status IN (sqlc.slice('statuses'))
ORDER BY created_at, id;

-- name: DeleteRecording :execrows
DELETE FROM recordings WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- Transcripts of a recording, for its deletion (PRD section 13.1).
-- name: ListTranscriptRefsByRecording :many
SELECT text_ref FROM transcripts WHERE recording_id = sqlc.arg(recording_id) ORDER BY id;

-- name: DeleteTranscriptsByRecording :exec
DELETE FROM transcripts WHERE recording_id = sqlc.arg(recording_id);
