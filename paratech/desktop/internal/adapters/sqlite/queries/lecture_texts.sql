-- Lecture texts of transcripts (TASK-090) with the optimistic-concurrency protocol of ADR-0004(d). The id is the
-- transcript id, one lecture text per transcript.

-- name: GetLectureText :one
SELECT * FROM lecture_texts WHERE id = sqlc.arg(id);

-- A never-stored lecture text (version 0) is inserted with version 1.
-- name: InsertLectureText :exec
INSERT INTO lecture_texts (id, transcript_id, lesson_id, blocks, prompt_version, run, run_parts, parts, version,
                           created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(transcript_id), sqlc.arg(lesson_id), sqlc.arg(blocks), sqlc.arg(prompt_version),
        sqlc.arg(run), sqlc.arg(run_parts), sqlc.arg(parts), 1, sqlc.arg(created_at), sqlc.arg(updated_at));

-- Compare-and-swap on the loaded version. The transcript and the lesson never change, so a stale save cannot undo
-- ON DELETE SET NULL.
-- name: UpdateLectureText :execrows
UPDATE lecture_texts
SET blocks         = sqlc.arg(blocks),
    prompt_version = sqlc.arg(prompt_version),
    run            = sqlc.arg(run),
    run_parts      = sqlc.arg(run_parts),
    parts          = sqlc.arg(parts),
    version        = version + 1,
    updated_at     = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);
