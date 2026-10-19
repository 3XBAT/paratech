-- Silent recordings (TASK-082): a COMPLETED recording whose final track is silent all or almost all the time.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema). Numbers 0010 and 0011 are taken by TASK-083 and TASK-084.

-- +goose Up
-- recordings: 1 when the final track is silent (recording.Recording.Silent): the chosen source played nothing, so the
-- recording is not transcribed and the UI says so. Only a COMPLETED recording is silent; existing rows are not.
ALTER TABLE recordings ADD COLUMN silent INTEGER NOT NULL DEFAULT 0 CHECK (silent IN (0, 1));

-- +goose Down
ALTER TABLE recordings DROP COLUMN silent;
