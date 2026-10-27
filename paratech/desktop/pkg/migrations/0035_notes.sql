-- The student's own notes of a lecture (TASK-149, ADR-0027): a note with its anchor and its ordered parts.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- ---------------------------------------------------------------------------------------------------------------
-- notes (migration 0035): aggregate library/note. A subject that holds a note cannot be deleted (RESTRICT). The anchor
-- is one of recording (recording_id), lesson (lesson_id), day or undated; recording_id and lesson_id carry NO foreign
-- key, so the purge of a recording (ADR-0018) and a re-import of the schedule never touch a note: a gone recording or
-- lesson falls back to the day of the snapshot when read. The snapshot is anchor_date (local YYYY-MM-DD, NULL only for
-- undated), lesson_kind (a short code, '' if unknown) and lesson_title.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE notes (
    id            TEXT    NOT NULL PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    subject_id    TEXT    NOT NULL REFERENCES subjects (id) ON DELETE RESTRICT,
    title         TEXT    NOT NULL CHECK (length(title) BETWEEN 1 AND 120),
    anchor_kind   TEXT    NOT NULL CHECK (anchor_kind IN ('recording', 'lesson', 'day', 'undated')),
    recording_id  TEXT    CHECK (recording_id IS NULL OR length(recording_id) BETWEEN 1 AND 128),
    lesson_id     TEXT    CHECK (lesson_id IS NULL OR length(lesson_id) BETWEEN 1 AND 128),
    anchor_date   TEXT    CHECK (anchor_date IS NULL OR anchor_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    lesson_kind   TEXT    NOT NULL DEFAULT '' CHECK (length(lesson_kind) <= 32),
    lesson_title  TEXT    NOT NULL DEFAULT '' CHECK (length(lesson_title) <= 200),
    status        TEXT    NOT NULL CHECK (status IN ('active', 'removed')),
    version       INTEGER NOT NULL DEFAULT 0,                             -- optimistic concurrency (ADR-0004)
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    CHECK ((anchor_kind = 'recording') = (recording_id IS NOT NULL)),
    CHECK ((anchor_kind = 'lesson') = (lesson_id IS NOT NULL)),
    CHECK ((anchor_kind = 'undated') = (anchor_date IS NULL))
);
CREATE INDEX idx_notes_subject ON notes (subject_id);
CREATE INDEX idx_notes_recording ON notes (recording_id) WHERE recording_id IS NOT NULL;
CREATE INDEX idx_notes_lesson ON notes (lesson_id) WHERE lesson_id IS NOT NULL;

-- note_parts: the materials of a note in the student's order, part of the note aggregate (no version of their own).
-- A material is soft-deleted, so one that is a part of a note cannot be deleted (RESTRICT).
CREATE TABLE note_parts (
    note_id     TEXT    NOT NULL REFERENCES notes (id) ON DELETE CASCADE,
    material_id TEXT    NOT NULL REFERENCES materials (id) ON DELETE RESTRICT,
    position    INTEGER NOT NULL CHECK (position BETWEEN 0 AND 199),
    PRIMARY KEY (note_id, material_id),
    UNIQUE (note_id, position)
);
CREATE INDEX idx_note_parts_material ON note_parts (material_id);

-- +goose Down
DROP INDEX idx_note_parts_material;
DROP TABLE note_parts;
DROP INDEX idx_notes_lesson;
DROP INDEX idx_notes_recording;
DROP INDEX idx_notes_subject;
DROP TABLE notes;
