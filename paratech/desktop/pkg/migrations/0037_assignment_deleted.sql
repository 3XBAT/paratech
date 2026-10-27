-- «Удалить» of any assignment is a soft delete (ADR-0029, addendum 2026-10-27; TASK-228): the row stays, hidden from
-- every reader, and «Вернуть» brings it back. A deleted extracted row is also the tombstone reprocessing never revives.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- assignments: when the student deleted the assignment (ms); NULL while it is not deleted.
ALTER TABLE assignments ADD COLUMN deleted_at INTEGER;

-- +goose Down
ALTER TABLE assignments DROP COLUMN deleted_at;
