-- Notices of finished processing (TASK-053).
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- ---------------------------------------------------------------------------------------------------------------
-- job_notices: the user was notified that a job reached a finished status (PRD section 15). Aggregate jobs/notice.
-- One row per job and status, written BEFORE the notification is shown, so repeated events and restarts never notify
-- twice (at most once: a crash between the two loses that notification). Rows never change, so there is no Save
-- port and no version column (ADR-0004(d)). Status values = notice.Statuses. Nothing but the job type is copied: the
-- texts shown are fixed per type, never user content.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE job_notices (
    job_id      TEXT    NOT NULL REFERENCES jobs (id) ON DELETE CASCADE, -- a notice means nothing without its job
    status      TEXT    NOT NULL CHECK (status IN ('COMPLETED', 'FAILED')),
    job_type    TEXT    NOT NULL CHECK (length(job_type) > 0),
    notified_at INTEGER NOT NULL,
    PRIMARY KEY (job_id, status)
);

-- +goose Down
DROP TABLE job_notices;
