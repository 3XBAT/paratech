-- Lecture texts (TASK-090): the full readable text of a transcript, formatted by the LLM part by part.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- ---------------------------------------------------------------------------------------------------------------
-- lecture_texts: the lecture text of one transcript. Aggregate coursework/lecturetext; id = the transcript id. blocks is
-- the text shown (empty until a run completed); run, run_parts and parts are the run in progress and its finished
-- parts, so a run that stops continues from the next part. The text is model output, stored as data only (hard rule
-- 3); it stays when its transcript is deleted (transcript_id SET NULL), like the summary.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE lecture_texts (
    id              TEXT    PRIMARY KEY,
    transcript_id   TEXT    REFERENCES transcripts (id) ON DELETE SET NULL,
    lesson_id       TEXT    REFERENCES schedule_entries (id) ON DELETE SET NULL,
    blocks          TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(blocks)), -- JSON array of {heading, start_ms, paragraphs}
    prompt_version  INTEGER NOT NULL DEFAULT 0 CHECK (prompt_version >= 0),  -- prompts.LectureTextVersion; 0 without blocks
    run             TEXT    NOT NULL DEFAULT '' CHECK (length(run) <= 128),  -- digest of the requests of the run; '' none
    run_parts       INTEGER NOT NULL DEFAULT 0 CHECK (run_parts BETWEEN 0 AND 64),
    parts           TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(parts)),  -- JSON array of the finished parts (block arrays)
    version         INTEGER NOT NULL DEFAULT 0,                               -- optimistic concurrency (ADR-0004)
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);
CREATE INDEX idx_lecture_texts_lesson ON lecture_texts (lesson_id);

-- +goose Down
DROP INDEX idx_lecture_texts_lesson;
DROP TABLE lecture_texts;
