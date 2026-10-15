-- The remembered recording source of the source picker (TASK-059).
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- settings: the executable name of the app the user last recorded ("chrome.exe"), preselected next time; empty: the
-- whole computer. An app name, never a process ID: it is remembered across restarts of the app.
ALTER TABLE settings ADD COLUMN recording_source_app TEXT NOT NULL DEFAULT '' CHECK (length(recording_source_app) <= 255);

-- +goose Down
ALTER TABLE settings DROP COLUMN recording_source_app;
