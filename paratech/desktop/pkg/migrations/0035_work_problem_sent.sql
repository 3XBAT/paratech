-- A work remembers why its last reader call did not go through and when it was last sent to the teacher (TASK-226),
-- so both survive a restart.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- works: the problem of the last reader call (work.Problem, a code, never model text); NULL after an answer. The time
-- the delivery of a send of the work was last SENT (ms); NULL if never.
ALTER TABLE works ADD COLUMN last_problem TEXT CHECK (last_problem IS NULL OR last_problem IN ('offline', 'quota', 'refused', 'no_text', 'source_gone'));
ALTER TABLE works ADD COLUMN sent_at INTEGER;

-- +goose Down
ALTER TABLE works DROP COLUMN sent_at;
ALTER TABLE works DROP COLUMN last_problem;
