-- Photos as pages of a note without text (TASK-165, ADR-0027 «Handwriting and scans»): materials of type 'jpeg' and
-- 'png' with text_status 'no_text'. SQLite cannot alter a CHECK, so materials is rebuilt with every row.
--
-- Other tables reference materials (material_lessons, note_parts, works, ...). With foreign keys on, DROP TABLE would
-- delete through them (CASCADE) or fail (RESTRICT), and RENAME would point their references at the old table. So the
-- rebuild runs outside goose's transaction, with foreign keys off and the legacy rename (references left as they
-- are), inside a transaction of its own; the ids do not change, so every reference still holds. The old table is
-- renamed first, so the new one keeps its plain name in its DDL.
--
-- docs/contracts/schema.sql shows the result: materials as rebuilt here (TestMigrationMatchesContractSchema).

-- +goose NO TRANSACTION

-- +goose Up
PRAGMA foreign_keys = OFF;
PRAGMA legacy_alter_table = ON;
BEGIN IMMEDIATE;
ALTER TABLE materials RENAME TO materials_v1;
CREATE TABLE materials (
    id            TEXT    NOT NULL PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    sha256        TEXT    NOT NULL UNIQUE CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    size_bytes    INTEGER NOT NULL CHECK (size_bytes BETWEEN 1 AND 104857600),
    type          TEXT    NOT NULL CHECK (type IN ('docx', 'pptx', 'txt', 'pdf', 'jpeg', 'png')),
    original_name TEXT    NOT NULL DEFAULT '' CHECK (length(original_name) <= 255),
    subject_id    TEXT    REFERENCES subjects (id) ON DELETE RESTRICT,
    folder_id     TEXT,
    status        TEXT    NOT NULL CHECK (status IN ('active', 'removed')),
    text_status   TEXT    NOT NULL CHECK (text_status IN ('pending', 'extracted', 'failed', 'pdf_not_extracted', 'no_text')),
    version       INTEGER NOT NULL DEFAULT 0,                             -- optimistic concurrency (ADR-0004)
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    CHECK ((subject_id IS NULL) = (folder_id IS NULL)),
    CHECK ((type IN ('jpeg', 'png')) = (text_status = 'no_text')),
    FOREIGN KEY (subject_id, folder_id) REFERENCES subject_folders (subject_id, id) ON DELETE RESTRICT
);
INSERT INTO materials (id, sha256, size_bytes, type, original_name, subject_id, folder_id, status, text_status, version,
                       created_at, updated_at)
    SELECT id, sha256, size_bytes, type, original_name, subject_id, folder_id, status, text_status, version, created_at,
           updated_at
    FROM materials_v1;
DROP TABLE materials_v1;
CREATE INDEX idx_materials_place ON materials (subject_id, folder_id);
COMMIT;
PRAGMA legacy_alter_table = OFF;
PRAGMA foreign_keys = ON;

-- +goose Down
-- The 0021 table again; photos cannot be stored there and are dropped with their links and note parts.
PRAGMA foreign_keys = OFF;
PRAGMA legacy_alter_table = ON;
BEGIN IMMEDIATE;
DELETE FROM material_lessons WHERE material_id IN (SELECT id FROM materials WHERE type IN ('jpeg', 'png'));
DELETE FROM note_parts WHERE material_id IN (SELECT id FROM materials WHERE type IN ('jpeg', 'png'));
ALTER TABLE materials RENAME TO materials_v2;
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
INSERT INTO materials (id, sha256, size_bytes, type, original_name, subject_id, folder_id, status, text_status, version,
                       created_at, updated_at)
    SELECT id, sha256, size_bytes, type, original_name, subject_id, folder_id, status, text_status, version, created_at,
           updated_at
    FROM materials_v2 WHERE type NOT IN ('jpeg', 'png');
DROP TABLE materials_v2;
CREATE INDEX idx_materials_place ON materials (subject_id, folder_id);
COMMIT;
PRAGMA legacy_alter_table = OFF;
PRAGMA foreign_keys = ON;
