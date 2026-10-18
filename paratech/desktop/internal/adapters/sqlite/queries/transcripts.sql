-- Transcripts (PRD section 8, AC-08) with the optimistic-concurrency protocol of ADR-0004(d). model_name and
-- model_version are copies of the model identity, so they stay after the model row is removed (model_id SET NULL).

-- name: GetTranscript :one
SELECT * FROM transcripts WHERE id = sqlc.arg(id);

-- A never-stored transcript (version 0) is inserted with version 1.
-- name: InsertTranscript :exec
INSERT INTO transcripts (id, recording_id, model_id, model_name, model_version, language, text_ref, version, created_at)
VALUES (sqlc.arg(id), sqlc.arg(recording_id), sqlc.arg(model_id), sqlc.arg(model_name), sqlc.arg(model_version),
        sqlc.arg(language), sqlc.arg(text_ref), 1, sqlc.arg(created_at));

-- Compare-and-swap on the loaded version. A transcript does not change today; the update exists so that Save keeps
-- the protocol of every repository. The recording, the model reference and the pinned identity never change, so a
-- stale save cannot undo ON DELETE SET NULL of the model.
-- name: UpdateTranscript :execrows
UPDATE transcripts
SET language = sqlc.arg(language),
    text_ref = sqlc.arg(text_ref),
    version  = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- The newest transcript of a recording (TASK-079: the transcript viewer); idx_transcripts_recording serves it.
-- name: LatestTranscriptByRecording :one
SELECT * FROM transcripts
WHERE recording_id = sqlc.arg(recording_id)
ORDER BY created_at DESC, id DESC
LIMIT 1;
