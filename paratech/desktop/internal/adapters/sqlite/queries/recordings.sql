-- Recordings (PRD section 6.2). Writes run through DB.Do; the status machine stays in the aggregate, the queries only
-- store the result.

-- name: GetRecording :one
SELECT * FROM recordings WHERE id = sqlc.arg(id);

-- A never-stored recording (version 0) is inserted with version 1.
-- name: InsertRecording :exec
INSERT INTO recordings (id, lesson_id, status, started_at, stopped_at, heartbeat_at, max_duration_ms, duration_ms,
                        chunk_count, chunks_ref, audio_ref, failure_reason, silent, lesson_title, lesson_subject,
                        lesson_kind, lesson_teacher, lesson_start, lesson_end, version, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(lesson_id), sqlc.arg(status), sqlc.arg(started_at), sqlc.arg(stopped_at),
        sqlc.arg(heartbeat_at), sqlc.arg(max_duration_ms), sqlc.arg(duration_ms), sqlc.arg(chunk_count),
        sqlc.arg(chunks_ref), sqlc.arg(audio_ref), sqlc.arg(failure_reason), sqlc.arg(silent), sqlc.arg(lesson_title),
        sqlc.arg(lesson_subject), sqlc.arg(lesson_kind), sqlc.arg(lesson_teacher), sqlc.arg(lesson_start),
        sqlc.arg(lesson_end), 1, sqlc.arg(created_at), sqlc.arg(updated_at));

-- Compare-and-swap on the loaded version (ADR-0004). The lesson, its snapshot, the cap, the chunk directory and the
-- creation time never change; lesson_id is left alone so that ON DELETE SET NULL of the lesson is not undone by a stale save.
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
    silent         = sqlc.arg(silent),
    version        = version + 1,
    updated_at     = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- name: ListRecordingsByStatus :many
SELECT * FROM recordings
WHERE status IN (sqlc.slice('statuses'))
ORDER BY created_at, id;

-- Ids for the recordings screen (TASK-023): every row in an active status plus the newest max_terminal other rows,
-- newest first. Ids only, so that one row that cannot be read does not hide the others (the caller loads each).
-- active is a JSON array of status names.
-- name: ListRecordingActivityIDs :many
SELECT id FROM recordings
WHERE id IS NOT NULL
  AND (status IN (SELECT value FROM json_each(CAST(sqlc.arg(active) AS TEXT)))
       OR id IN (SELECT id FROM recordings
                 WHERE id IS NOT NULL
                   AND status NOT IN (SELECT value FROM json_each(CAST(sqlc.arg(active) AS TEXT)))
                 ORDER BY created_at DESC, id DESC
                 LIMIT sqlc.arg(max_terminal)))
ORDER BY created_at DESC, id DESC;

-- Recordings in the status with a final track and no transcript, in id order after the given id (TASK-060: the
-- startup sweep that enqueues their transcription). Silent recordings are never transcribed (TASK-082). Ids only: the
-- caller loads each one and its aggregate decides.
-- name: ListUntranscribedRecordingIDs :many
SELECT r.id FROM recordings r
WHERE r.id IS NOT NULL
  AND r.id > sqlc.arg(after)
  AND r.status = sqlc.arg(status)
  AND r.audio_ref IS NOT NULL
  AND r.silent = 0
  AND NOT EXISTS (SELECT 1 FROM transcripts t WHERE t.recording_id = r.id)
ORDER BY r.id
LIMIT sqlc.arg(max_rows);

-- name: DeleteRecording :execrows
DELETE FROM recordings WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- Transcripts of a recording, for its deletion (PRD section 13.1).
-- name: ListTranscriptRefsByRecording :many
SELECT text_ref FROM transcripts WHERE recording_id = sqlc.arg(recording_id) ORDER BY id;

-- name: DeleteTranscriptsByRecording :exec
DELETE FROM transcripts WHERE recording_id = sqlc.arg(recording_id);

-- The recording a transcript was made from, for the lesson snapshot of its assignments (TASK-092).
-- name: GetRecordingOfTranscript :one
SELECT recordings.* FROM recordings
JOIN transcripts ON transcripts.recording_id = recordings.id
WHERE transcripts.id = sqlc.arg(transcript_id);
