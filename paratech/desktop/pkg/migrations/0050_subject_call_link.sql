-- The link to the online class the student keeps on a subject (TASK-364, ADR-0032).
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- subjects: the call link in the canonical form of subject.CallLink; '' none. Untrusted input, re-checked on read (a
-- value that no longer parses is read as no link); never written to Google Calendar, the logs or the Journal.
ALTER TABLE subjects ADD COLUMN call_link TEXT NOT NULL DEFAULT '' CHECK (length(call_link) <= 2048);

-- +goose Down
ALTER TABLE subjects DROP COLUMN call_link;
