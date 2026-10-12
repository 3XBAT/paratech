-- NOTE: toolchain proof: these queries show that sqlc, goose and the adapter work together. The TASK-008 owner may replace or remove them.

-- Read-only job statistics; the job repository (TASK-008 owner) adds Enqueue, Claim and Save.

-- name: CountJobsByStatus :many
SELECT status, COUNT(*) AS total
FROM jobs
GROUP BY status
ORDER BY status;
