-- Drop the notifications of finished processing (TASK-065, owner decision): the table of 0004 and the setting of 0001.
--
-- docs/contracts/schema.sql shows the result: neither the table nor the column is in it any more
-- (TestMigrationMatchesContractSchema). SQLite drops a column whose CHECK names only that column.

-- +goose Up
DROP TABLE job_notices;
ALTER TABLE settings DROP COLUMN notify_on_processing_complete;

-- +goose Down
-- The column comes back with its 0001 definition, at the end of the table; the queries name their columns.
ALTER TABLE settings ADD COLUMN notify_on_processing_complete INTEGER NOT NULL DEFAULT 1 CHECK (notify_on_processing_complete IN (0, 1));
CREATE TABLE job_notices (
    job_id      TEXT    NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
    status      TEXT    NOT NULL CHECK (status IN ('COMPLETED', 'FAILED')),
    job_type    TEXT    NOT NULL CHECK (length(job_type) > 0),
    notified_at INTEGER NOT NULL,
    PRIMARY KEY (job_id, status)
);
