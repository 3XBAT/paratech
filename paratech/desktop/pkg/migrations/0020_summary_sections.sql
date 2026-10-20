-- The sections of a summary (TASK-101): heading, text and points per section. body keeps the plain text of the
-- summary (for a sectioned one its rendering), so it is never empty.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- summaries (migration 0020): the sections as a JSON array of {"heading","text","points"} objects (summary.Section).
-- NULL: a summary made before, one plain text.
ALTER TABLE summaries ADD COLUMN sections TEXT CHECK (sections IS NULL OR (json_valid(sections) AND json_type(sections) = 'array'));

-- +goose Down
ALTER TABLE summaries DROP COLUMN sections;
