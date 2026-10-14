-- Post-processing of transcripts: summaries and extracted assignments (TASK-042).
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema). Deadlines are the due_at of the assignments; there is no separate table.

-- +goose Up
-- ---------------------------------------------------------------------------------------------------------------
-- summaries: the LLM summary of one transcript (PRD sections 9 and 10; AC-13). Aggregate coursework/summary.
-- id = the transcript id: one summary per transcript, so reprocessing replaces it. The text is model output, stored
-- as data only (hard rule 3). The summary stays when its transcript is deleted (transcript_id SET NULL).
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE summaries (
    id              TEXT    PRIMARY KEY,
    transcript_id   TEXT    REFERENCES transcripts (id) ON DELETE SET NULL,
    lesson_id       TEXT    REFERENCES schedule_entries (id) ON DELETE SET NULL,
    body            TEXT    NOT NULL CHECK (length(body) > 0),
    key_points      TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(key_points)), -- JSON array of strings
    prompt_version  INTEGER NOT NULL CHECK (prompt_version >= 1),                 -- prompts.Version that produced it
    version         INTEGER NOT NULL DEFAULT 0,                                    -- optimistic concurrency (ADR-0004)
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);
CREATE INDEX idx_summaries_lesson ON summaries (lesson_id);

-- assignments extracted from a transcript: source_index is the position of the item in the extraction answer, so
-- reprocessing finds the assignment it made before (no duplicates); prompt_version is the prompts.Version of the
-- extraction that last set the values. Both are NULL for an assignment that was not extracted. A user-edited row
-- (user_edited = 1) is never overwritten by reprocessing; the rule lives in the assignment aggregate.
ALTER TABLE assignments ADD COLUMN source_index INTEGER CHECK (source_index IS NULL OR source_index >= 0);
ALTER TABLE assignments ADD COLUMN prompt_version INTEGER CHECK (prompt_version IS NULL OR prompt_version >= 1);
CREATE UNIQUE INDEX idx_assignments_source ON assignments (transcript_id, source_index)
    WHERE transcript_id IS NOT NULL AND source_index IS NOT NULL;

-- +goose Down
DROP INDEX idx_assignments_source;
ALTER TABLE assignments DROP COLUMN prompt_version;
ALTER TABLE assignments DROP COLUMN source_index;
DROP INDEX idx_summaries_lesson;
DROP TABLE summaries;
