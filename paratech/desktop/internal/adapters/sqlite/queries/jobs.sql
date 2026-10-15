-- Job store (PRD section 7). Writes run through DB.Do; the repository restores the aggregate and applies the domain
-- transition, the queries only store the result.

-- name: GetJob :one
SELECT * FROM jobs WHERE id = sqlc.arg(id);

-- name: GetJobIDByKey :one
SELECT id FROM jobs WHERE idempotency_key = sqlc.arg(idempotency_key);

-- A never-stored job (version 0) is inserted with version 1.
-- name: InsertJob :exec
INSERT INTO jobs (id, type, status, idempotency_key, payload, pinned_model_id, attempts, max_attempts, next_run_at,
                  run_now, progress_percent, checkpoint, last_error, created_at, started_at, finished_at, version,
                  updated_at)
VALUES (sqlc.arg(id), sqlc.arg(type), sqlc.arg(status), sqlc.arg(idempotency_key), sqlc.arg(payload),
        sqlc.arg(pinned_model_id), sqlc.arg(attempts), sqlc.arg(max_attempts), sqlc.arg(next_run_at),
        sqlc.arg(run_now), sqlc.arg(progress_percent), sqlc.arg(checkpoint), sqlc.arg(last_error),
        sqlc.arg(created_at), sqlc.arg(started_at), sqlc.arg(finished_at), 1, sqlc.arg(updated_at));

-- Compare-and-swap on the loaded version (ADR-0004). Type, key, payload and pinned model never change.
-- name: UpdateJob :execrows
UPDATE jobs
SET status           = sqlc.arg(status),
    attempts         = sqlc.arg(attempts),
    next_run_at      = sqlc.arg(next_run_at),
    run_now          = sqlc.arg(run_now),
    progress_percent = sqlc.arg(progress_percent),
    checkpoint       = sqlc.arg(checkpoint),
    last_error       = sqlc.arg(last_error),
    started_at       = sqlc.arg(started_at),
    finished_at      = sqlc.arg(finished_at),
    version          = version + 1,
    updated_at       = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- Same update for the claim, guarded by the status as well: zero rows means another worker took the job.
-- name: UpdateClaimedJob :execrows
UPDATE jobs
SET status           = sqlc.arg(status),
    attempts         = sqlc.arg(attempts),
    next_run_at      = sqlc.arg(next_run_at),
    run_now          = sqlc.arg(run_now),
    progress_percent = sqlc.arg(progress_percent),
    checkpoint       = sqlc.arg(checkpoint),
    last_error       = sqlc.arg(last_error),
    started_at       = sqlc.arg(started_at),
    finished_at      = sqlc.arg(finished_at),
    version          = version + 1,
    updated_at       = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND status = 'QUEUED' AND version = sqlc.arg(expected_version);

-- Next due QUEUED job admitted by job.ClaimFilter. The lists are JSON arrays of type names (an empty list is safe,
-- unlike an empty IN list): a heavy type needs heavy_allowed or the run_now override of the job (AC-22).
-- skip_ids lists rows the caller found unusable (corrupt) so that they do not block the jobs behind them.
-- name: NextClaimableJob :one
SELECT * FROM jobs
WHERE status = 'QUEUED'
  AND attempts < max_attempts
  AND next_run_at <= sqlc.arg(now)
  AND id NOT IN (SELECT value FROM json_each(CAST(sqlc.arg(skip_ids) AS TEXT)))
  AND type IN (SELECT value FROM json_each(CAST(sqlc.arg(types) AS TEXT)))
  AND (CAST(sqlc.arg(heavy_allowed) AS INTEGER) = 1
       OR run_now = 1
       OR type NOT IN (SELECT value FROM json_each(CAST(sqlc.arg(heavy_types) AS TEXT))))
ORDER BY next_run_at, created_at, id
LIMIT 1;

-- Whether a job of the type has an idempotency key that starts with the prefix, in any status (TASK-060: one automatic
-- STT job per recording). substr and length both count characters, so the comparison is exact for any text.
-- name: HasJobKeyPrefix :one
SELECT EXISTS (
    SELECT 1 FROM jobs
    WHERE type = sqlc.arg(type)
      AND substr(idempotency_key, 1, length(CAST(sqlc.arg(prefix) AS TEXT))) = CAST(sqlc.arg(prefix) AS TEXT)
) AS found;

-- name: ListJobsByStatus :many
SELECT * FROM jobs
WHERE status IN (sqlc.slice('statuses'))
ORDER BY created_at, id;

-- Ids for the jobs screen (TASK-023): every row in an active status plus the newest max_terminal other rows,
-- newest first. Ids only, so that one row that cannot be read does not hide the others (the caller loads each).
-- active is a JSON array of status names.
-- name: ListJobActivityIDs :many
SELECT id FROM jobs
WHERE id IS NOT NULL
  AND (status IN (SELECT value FROM json_each(CAST(sqlc.arg(active) AS TEXT)))
       OR id IN (SELECT id FROM jobs
                 WHERE id IS NOT NULL
                   AND status NOT IN (SELECT value FROM json_each(CAST(sqlc.arg(active) AS TEXT)))
                 ORDER BY created_at DESC, id DESC
                 LIMIT sqlc.arg(max_terminal)))
ORDER BY created_at DESC, id DESC;

-- Ids only, so that one row that cannot be read does not hide the others (startup recovery loads each with GetJob).
-- SQLite accepts NULL in a TEXT primary key; such a row cannot be addressed by id, so it is left out.
-- name: ListJobIDsByStatus :many
SELECT id FROM jobs
WHERE status IN (sqlc.slice('statuses')) AND id IS NOT NULL
ORDER BY created_at, id;

-- Rows that ListJobIDsByStatus leaves out for a NULL id, so the caller can report them.
-- name: CountJobsWithoutIDByStatus :one
SELECT COUNT(*) FROM jobs
WHERE status IN (sqlc.slice('statuses')) AND id IS NULL;

-- name: CountJobsByStatus :many
SELECT status, COUNT(*) AS total
FROM jobs
GROUP BY status
ORDER BY status;
