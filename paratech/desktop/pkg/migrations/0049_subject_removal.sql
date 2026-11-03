-- Soft removal of a subject (TASK-288, ADR-0027 «Removing a subject that has notes»): the subject is removed with its
-- notes and files for 3 days, then purged (TASK-308). Nothing below it is rewritten; the readers hide what is filed
-- under a removed subject.
--
-- docs/contracts/schema.sql mirrors the column (TestMigrationMatchesContractSchema).

-- +goose Up
ALTER TABLE subjects ADD COLUMN removed_at INTEGER CHECK (removed_at IS NULL OR removed_at > 0);

-- +goose Down
-- A removed subject comes back active: an older build does not know the removal.
ALTER TABLE subjects DROP COLUMN removed_at;
