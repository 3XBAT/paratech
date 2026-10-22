-- Turnkey works (TASK-129, ADR-0025): the triage of an assignment by the quarantined reader TRIAGE_ASSIGNMENT, the
-- student's answers to its questions (at most two rounds), the edits of «Как я понял задание» and the helper mode.
-- Local data is the system of record; the reader's answers are untrusted data, read back through the strict schema.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- works (migration 0026): aggregate coursework/work. Exactly one source: an assignment or a library material (an
-- uploaded assignment file); a source has at most one work that has not failed. round is the round of the pending or
-- last reader call, attempt the attempt of the pending one. edit_* are the student's edits of the card (NULL: the
-- reader's value is shown).
CREATE TABLE works (
    id                 TEXT    NOT NULL PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    assignment_id      TEXT    REFERENCES assignments (id) ON DELETE CASCADE,
    material_id        TEXT    REFERENCES materials (id) ON DELETE CASCADE,
    status             TEXT    NOT NULL CHECK (status IN ('triage', 'questions', 'review', 'confirmed', 'writing', 'done', 'helper', 'failed')),
    round              INTEGER NOT NULL CHECK (round BETWEEN 1 AND 3),
    attempt            INTEGER NOT NULL CHECK (attempt BETWEEN 1 AND 3),
    edit_work_type     TEXT    CHECK (edit_work_type IS NULL OR edit_work_type IN ('referat', 'doklad', 'essay', 'answers', 'problems', 'lab_report', 'presentation', 'other')),
    edit_volume        TEXT    CHECK (edit_volume IS NULL OR length(edit_volume) <= 200),
    edit_file_format   TEXT    CHECK (edit_file_format IS NULL OR length(edit_file_format) <= 200),
    edit_formatting    TEXT    CHECK (edit_formatting IS NULL OR length(edit_formatting) <= 200),
    edit_variant       TEXT    CHECK (edit_variant IS NULL OR length(edit_variant) <= 200),
    edit_deadline      TEXT    CHECK (edit_deadline IS NULL OR length(edit_deadline) <= 200),
    edit_sources       TEXT    CHECK (edit_sources IS NULL OR length(edit_sources) <= 200),
    version            INTEGER NOT NULL DEFAULT 0,                          -- optimistic concurrency (ADR-0004)
    created_at         INTEGER NOT NULL,
    updated_at         INTEGER NOT NULL,
    CHECK ((assignment_id IS NULL) <> (material_id IS NULL))
);
CREATE UNIQUE INDEX idx_works_assignment ON works (assignment_id) WHERE assignment_id IS NOT NULL AND status <> 'failed';
CREATE UNIQUE INDEX idx_works_material ON works (material_id) WHERE material_id IS NOT NULL AND status <> 'failed';

-- work_rounds (migration 0026): one row per answered reader call of a work, saved with it (the work carries the
-- version). answer is the reader's checked answer in its canonical JSON (work.Answer.Encode), untrusted data read back
-- with work.ParseAnswer; replies the student's resolved answers to its questions (NULL until answered or skipped).
CREATE TABLE work_rounds (
    work_id  TEXT    NOT NULL REFERENCES works (id) ON DELETE CASCADE,
    round    INTEGER NOT NULL CHECK (round BETWEEN 1 AND 3),
    answer   TEXT    NOT NULL CHECK (json_valid(answer)),
    replies  TEXT    CHECK (replies IS NULL OR json_valid(replies)),
    PRIMARY KEY (work_id, round)
);

-- +goose Down
DROP TABLE work_rounds;
DROP INDEX idx_works_material;
DROP INDEX idx_works_assignment;
DROP TABLE works;
