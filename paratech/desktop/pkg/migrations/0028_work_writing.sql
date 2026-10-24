-- The writing of a turnkey work (TASK-131, ADR-0025 addendum): the outline, the text of each section per revision and
-- attempt, the lesson snapshot of the title page and the paper filed in the library. Local data is the system of
-- record; the texts are model output, read back through the same strict checks as a fresh answer.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- works.writing (migration 0028): the writing state of a work in its canonical JSON (work.Writing.Encode), read back
-- with work.ParseWriting and checked by the aggregate; NULL before the writing starts. Saved with the work under its
-- version.
ALTER TABLE works ADD COLUMN writing TEXT CHECK (writing IS NULL OR json_valid(writing));

-- +goose Down
ALTER TABLE works DROP COLUMN writing;
