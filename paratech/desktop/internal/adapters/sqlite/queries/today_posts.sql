-- Posts after lessons on the Today screen (TASK-125) with the optimistic-concurrency protocol of ADR-0004(d). The id is the
-- recording id: one post per recording.

-- name: GetTodayPost :one
SELECT * FROM today_posts WHERE recording_id = sqlc.arg(recording_id);

-- A never-stored post (version 0) is inserted with version 1.
-- name: InsertTodayPost :exec
INSERT INTO today_posts (recording_id, transcript_id, summary_at, homework_at, version, created_at, updated_at)
VALUES (sqlc.arg(recording_id), sqlc.arg(transcript_id), sqlc.arg(summary_at), sqlc.arg(homework_at), 1,
        sqlc.arg(created_at), sqlc.arg(updated_at));

-- Compare-and-swap on the loaded version. The recording and the creation time never change.
-- name: UpdateTodayPost :execrows
UPDATE today_posts
SET transcript_id = sqlc.arg(transcript_id),
    summary_at    = sqlc.arg(summary_at),
    homework_at   = sqlc.arg(homework_at),
    version       = version + 1,
    updated_at    = sqlc.arg(updated_at)
WHERE recording_id = sqlc.arg(recording_id) AND version = sqlc.arg(expected_version);

-- name: ListTodayPostsSince :many
SELECT * FROM today_posts
WHERE updated_at >= sqlc.arg(since)
ORDER BY updated_at DESC, recording_id
LIMIT sqlc.arg(max_rows);
