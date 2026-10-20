-- Summaries of transcripts (PRD sections 9 and 10) with the optimistic-concurrency protocol of ADR-0004(d). The id is
-- the transcript id, one summary per transcript (TASK-042). sections is NULL for a summary made before TASK-101.

-- name: GetSummary :one
SELECT * FROM summaries WHERE id = sqlc.arg(id);

-- A never-stored summary (version 0) is inserted with version 1.
-- name: InsertSummary :exec
INSERT INTO summaries (id, transcript_id, lesson_id, body, sections, key_points, prompt_version, version, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(transcript_id), sqlc.arg(lesson_id), sqlc.arg(body), sqlc.arg(sections), sqlc.arg(key_points),
        sqlc.arg(prompt_version), 1, sqlc.arg(created_at), sqlc.arg(updated_at));

-- Compare-and-swap on the loaded version. The transcript and the lesson never change, so a stale save cannot undo
-- ON DELETE SET NULL.
-- name: UpdateSummary :execrows
UPDATE summaries
SET body           = sqlc.arg(body),
    sections       = sqlc.arg(sections),
    key_points     = sqlc.arg(key_points),
    prompt_version = sqlc.arg(prompt_version),
    version        = version + 1,
    updated_at     = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);
