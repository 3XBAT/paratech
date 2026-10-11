-- Read-only job statistics; the job repository (TASK-008 owner) adds Enqueue, Claim and Save.

-- name: CountJobsByStatus :many
SELECT status, COUNT(*) AS total
FROM jobs
GROUP BY status
ORDER BY status;
