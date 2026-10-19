-- Why a job waits or retries beyond its status (TASK-083).
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- jobs: job.Cause of the last unsuccessful attempt, for example a Hub that is not set up while the job parks in
-- WAITING_FOR_NETWORK; empty in every other case. Values = job.Causes.
ALTER TABLE jobs ADD COLUMN cause TEXT NOT NULL DEFAULT '' CHECK (cause IN ('', 'service_not_configured', 'service_unavailable'));

-- +goose Down
ALTER TABLE jobs DROP COLUMN cause;
