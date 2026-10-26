-- Works without an assignment (TASK-187, ADR-0025 addendum): a work may be made from a topic, so the CHECK of exactly
-- one source counts three columns. SQLite cannot alter a CHECK: works is rebuilt with every row. work_rounds, the only
-- table that references works, is rebuilt with it, so no foreign key action deletes a round meanwhile. The old tables
-- are renamed first, so the new ones keep their plain names in their DDL.
--
-- docs/contracts/schema.sql shows the result: works and work_rounds as rebuilt here (TestMigrationMatchesContractSchema).

-- +goose Up
ALTER TABLE work_rounds RENAME TO work_rounds_v1;
ALTER TABLE works RENAME TO works_v1;
DROP INDEX idx_works_assignment;
DROP INDEX idx_works_material;
CREATE TABLE works (
    id                 TEXT    NOT NULL PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    assignment_id      TEXT    REFERENCES assignments (id) ON DELETE CASCADE,
    material_id        TEXT    REFERENCES materials (id) ON DELETE CASCADE,
    topic_request      TEXT    CHECK (topic_request IS NULL OR length(topic_request) BETWEEN 1 AND 128),
    topic              TEXT    CHECK (topic IS NULL OR length(topic) BETWEEN 1 AND 500),
    topic_subject_id   TEXT    CHECK (topic_subject_id IS NULL OR length(topic_subject_id) BETWEEN 1 AND 128),
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
    writing            TEXT    CHECK (writing IS NULL OR json_valid(writing)),
    version            INTEGER NOT NULL DEFAULT 0,                          -- optimistic concurrency (ADR-0004)
    created_at         INTEGER NOT NULL,
    updated_at         INTEGER NOT NULL,
    CHECK ((assignment_id IS NOT NULL) + (material_id IS NOT NULL) + (topic IS NOT NULL) = 1),
    CHECK ((topic IS NULL) = (topic_request IS NULL)),
    CHECK (topic IS NOT NULL OR topic_subject_id IS NULL)
);
CREATE UNIQUE INDEX idx_works_assignment ON works (assignment_id) WHERE assignment_id IS NOT NULL AND status <> 'failed';
CREATE UNIQUE INDEX idx_works_material ON works (material_id) WHERE material_id IS NOT NULL AND status <> 'failed';
CREATE UNIQUE INDEX idx_works_topic ON works (topic_request) WHERE topic_request IS NOT NULL AND status <> 'failed';
CREATE TABLE work_rounds (
    work_id  TEXT    NOT NULL REFERENCES works (id) ON DELETE CASCADE,
    round    INTEGER NOT NULL CHECK (round BETWEEN 1 AND 3),
    answer   TEXT    NOT NULL CHECK (json_valid(answer)),
    replies  TEXT    CHECK (replies IS NULL OR json_valid(replies)),
    PRIMARY KEY (work_id, round)
);
INSERT INTO works (id, assignment_id, material_id, status, round, attempt, edit_work_type, edit_volume, edit_file_format,
                   edit_formatting, edit_variant, edit_deadline, edit_sources, writing, version, created_at, updated_at)
    SELECT id, assignment_id, material_id, status, round, attempt, edit_work_type, edit_volume, edit_file_format,
           edit_formatting, edit_variant, edit_deadline, edit_sources, writing, version, created_at, updated_at
    FROM works_v1;
INSERT INTO work_rounds (work_id, round, answer, replies) SELECT work_id, round, answer, replies FROM work_rounds_v1;
DROP TABLE work_rounds_v1;
DROP TABLE works_v1;

-- +goose Down
-- The 0026 tables with the 0028 column again; works of a topic cannot be stored there and are dropped with their rounds.
ALTER TABLE work_rounds RENAME TO work_rounds_v2;
ALTER TABLE works RENAME TO works_v2;
DROP INDEX idx_works_assignment;
DROP INDEX idx_works_material;
DROP INDEX idx_works_topic;
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
    writing            TEXT    CHECK (writing IS NULL OR json_valid(writing)),
    CHECK ((assignment_id IS NULL) <> (material_id IS NULL))
);
CREATE UNIQUE INDEX idx_works_assignment ON works (assignment_id) WHERE assignment_id IS NOT NULL AND status <> 'failed';
CREATE UNIQUE INDEX idx_works_material ON works (material_id) WHERE material_id IS NOT NULL AND status <> 'failed';
CREATE TABLE work_rounds (
    work_id  TEXT    NOT NULL REFERENCES works (id) ON DELETE CASCADE,
    round    INTEGER NOT NULL CHECK (round BETWEEN 1 AND 3),
    answer   TEXT    NOT NULL CHECK (json_valid(answer)),
    replies  TEXT    CHECK (replies IS NULL OR json_valid(replies)),
    PRIMARY KEY (work_id, round)
);
INSERT INTO works (id, assignment_id, material_id, status, round, attempt, edit_work_type, edit_volume, edit_file_format,
                   edit_formatting, edit_variant, edit_deadline, edit_sources, version, created_at, updated_at, writing)
    SELECT id, assignment_id, material_id, status, round, attempt, edit_work_type, edit_volume, edit_file_format,
           edit_formatting, edit_variant, edit_deadline, edit_sources, version, created_at, updated_at, writing
    FROM works_v2 WHERE topic IS NULL;
INSERT INTO work_rounds (work_id, round, answer, replies)
    SELECT work_id, round, answer, replies FROM work_rounds_v2 WHERE work_id IN (SELECT id FROM works);
DROP TABLE work_rounds_v2;
DROP TABLE works_v2;
