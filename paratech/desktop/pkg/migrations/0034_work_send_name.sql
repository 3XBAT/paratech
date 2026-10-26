-- The file name of the send of a work, pinned when the send is proposed (TASK-192): a rename in the library before «Да»
-- no longer changes the name the teacher gets.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- proposals: the name of the copy a send_work_email attaches (proposal.CopyName, a safe file name the core made from
-- the library name, never model text); '' for the other kinds, before validation and for sends stored before 0034.
ALTER TABLE proposals ADD COLUMN attachment_name TEXT NOT NULL DEFAULT '' CHECK (length(attachment_name) <= 128);

-- +goose Down
ALTER TABLE proposals DROP COLUMN attachment_name;
