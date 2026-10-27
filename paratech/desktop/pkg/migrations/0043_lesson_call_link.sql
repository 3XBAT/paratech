-- The call link of an imported lesson (TASK-256, ADR-0032).
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- schedule_entries: the call link of the imported event (lesson.Content.CallLink) in the canonical form of
-- values.CallLink; '' none. Untrusted input, re-checked on read; never written to Google Calendar.
ALTER TABLE schedule_entries ADD COLUMN call_link TEXT NOT NULL DEFAULT '' CHECK (length(call_link) <= 2048);

-- +goose Down
ALTER TABLE schedule_entries DROP COLUMN call_link;
