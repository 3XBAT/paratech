-- Drop the free-work notice of the agent (TASK-342, owner decision): a work on a free topic runs at once, no card.
--
-- docs/contracts/schema.sql shows the result: the column of 0032 is not in it any more
-- (TestMigrationMatchesContractSchema). SQLite drops a column whose CHECK names only that column.

-- +goose Up
ALTER TABLE settings DROP COLUMN free_work_notice;

-- +goose Down
-- The column comes back with its 0032 definition, at the end of the table; the queries name their columns.
ALTER TABLE settings ADD COLUMN free_work_notice INTEGER NOT NULL DEFAULT 1 CHECK (free_work_notice IN (0, 1));
