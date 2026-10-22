-- The library (TASK-121, ADR-0024): subjects with their aliases and folders, and the files filed under them.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- ---------------------------------------------------------------------------------------------------------------
-- subjects (migration 0021): the subjects the student created. Aggregate library/subject; name_key is values.Key of
-- the name (case-insensitive, «ё» = «е»), unique. semester is values.Semester.Code. Never created automatically.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE subjects (
    id          TEXT    NOT NULL PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    name        TEXT    NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    name_key    TEXT    NOT NULL UNIQUE CHECK (length(name_key) BETWEEN 1 AND 400),
    semester    TEXT    NOT NULL CHECK (semester GLOB '[0-9][0-9][0-9][0-9]-autumn' OR semester GLOB '[0-9][0-9][0-9][0-9]-spring'),
    version     INTEGER NOT NULL DEFAULT 0,                               -- optimistic concurrency (ADR-0004)
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

-- subject_aliases: other spellings of a subject, part of the aggregate (no version of their own). An alias key is
-- unique across all subjects; that it differs from every other subject's name key is checked by the use case.
CREATE TABLE subject_aliases (
    alias_key   TEXT    NOT NULL PRIMARY KEY CHECK (length(alias_key) BETWEEN 1 AND 400),
    subject_id  TEXT    NOT NULL REFERENCES subjects (id) ON DELETE CASCADE,
    alias       TEXT    NOT NULL CHECK (length(alias) BETWEEN 1 AND 200),
    position    INTEGER NOT NULL CHECK (position >= 0)
);
CREATE INDEX idx_subject_aliases_subject ON subject_aliases (subject_id);

-- subject_folders: the folders of a subject, part of the aggregate: the six system folders (system = their code) and
-- the student's own ('' system). Names are unique per subject by key. (subject_id, id) is the target of the
-- materials' foreign key, so a file can only be filed in a folder of its own subject.
CREATE TABLE subject_folders (
    id          TEXT    NOT NULL PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    subject_id  TEXT    NOT NULL REFERENCES subjects (id) ON DELETE CASCADE,
    name        TEXT    NOT NULL CHECK (length(name) BETWEEN 1 AND 64),
    name_key    TEXT    NOT NULL CHECK (length(name_key) BETWEEN 1 AND 128),
    system      TEXT    NOT NULL DEFAULT '' CHECK (system IN ('', 'lectures', 'seminars', 'labs', 'materials', 'homework', 'tickets')),
    position    INTEGER NOT NULL CHECK (position >= 0),
    UNIQUE (subject_id, name_key),
    UNIQUE (subject_id, id)
);

-- ---------------------------------------------------------------------------------------------------------------
-- materials: the files of the library. Aggregate library/material. One row and one blob per sha256 (the blob is
-- materials/<sha256> in the artifact store): uploading the same bytes again returns this row. original_name is shown
-- only. NULL subject and folder: «Входящие». A subject or folder that holds a file (active or removed) cannot be
-- deleted (RESTRICT): a removed file still needs them to be restored.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE materials (
    id            TEXT    NOT NULL PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    sha256        TEXT    NOT NULL UNIQUE CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    size_bytes    INTEGER NOT NULL CHECK (size_bytes BETWEEN 1 AND 104857600),
    type          TEXT    NOT NULL CHECK (type IN ('docx', 'pptx', 'txt', 'pdf')),
    original_name TEXT    NOT NULL DEFAULT '' CHECK (length(original_name) <= 255),
    subject_id    TEXT    REFERENCES subjects (id) ON DELETE RESTRICT,
    folder_id     TEXT,
    status        TEXT    NOT NULL CHECK (status IN ('active', 'removed')),
    text_status   TEXT    NOT NULL CHECK (text_status IN ('pending', 'extracted', 'failed', 'pdf_not_extracted')),
    version       INTEGER NOT NULL DEFAULT 0,                             -- optimistic concurrency (ADR-0004)
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    CHECK ((subject_id IS NULL) = (folder_id IS NULL)),
    FOREIGN KEY (subject_id, folder_id) REFERENCES subject_folders (subject_id, id) ON DELETE RESTRICT
);
CREATE INDEX idx_materials_place ON materials (subject_id, folder_id);

-- material_lessons: the lessons a material is linked to, part of the material aggregate. A purged lesson takes its
-- links with it.
CREATE TABLE material_lessons (
    material_id TEXT    NOT NULL REFERENCES materials (id) ON DELETE CASCADE,
    lesson_id   TEXT    NOT NULL REFERENCES schedule_entries (id) ON DELETE CASCADE,
    position    INTEGER NOT NULL CHECK (position >= 0),
    PRIMARY KEY (material_id, lesson_id)
);
CREATE INDEX idx_material_lessons_lesson ON material_lessons (lesson_id);

-- +goose Down
DROP INDEX idx_material_lessons_lesson;
DROP TABLE material_lessons;
DROP INDEX idx_materials_place;
DROP TABLE materials;
DROP TABLE subject_folders;
DROP INDEX idx_subject_aliases_subject;
DROP TABLE subject_aliases;
DROP TABLE subjects;
