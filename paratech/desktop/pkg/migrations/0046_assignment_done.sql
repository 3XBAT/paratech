-- «Выполнено» of an assignment (ADR-0033 §9; TASK-313): the student marks it done and may un-do it.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- assignments: when the student marked the assignment done (ms); NULL while it is not done.
ALTER TABLE assignments ADD COLUMN done_at INTEGER;

-- +goose Down
ALTER TABLE assignments DROP COLUMN done_at;
