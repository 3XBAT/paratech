-- Notices of finished processing (PRD section 15, TASK-053): one row per job and status, never updated.

-- An existing notice for the same job and status is kept: zero rows affected means the user was already notified.
-- name: InsertJobNotice :execrows
INSERT INTO job_notices (job_id, status, job_type, notified_at)
VALUES (sqlc.arg(job_id), sqlc.arg(status), sqlc.arg(job_type), sqlc.arg(notified_at))
ON CONFLICT (job_id, status) DO NOTHING;

-- name: GetJobNotice :one
SELECT * FROM job_notices WHERE job_id = sqlc.arg(job_id) AND status = sqlc.arg(status);
