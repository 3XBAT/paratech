-- The reading of library materials (TASK-122, ADR-0022, ADR-0024): the outcome of the quarantined reader per material,
-- and the summaries and assignments made from the text of a material instead of a transcript. No row needs a value:
-- nothing was read from a material before.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- material_inspections (migration 0024): aggregate library/inspection, one row per material. text_sha256 is the digest
-- of the extracted text the outcome is for; attempt counts the reader calls for it (each refused one is charged).
-- The answer columns are suggestions only (never acted on alone) and are set exactly when status is 'done';
-- subject_id is a candidate the reader named, without a foreign key: a subject removed since is dropped when read.
CREATE TABLE material_inspections (
    material_id     TEXT    NOT NULL PRIMARY KEY REFERENCES materials (id) ON DELETE CASCADE,
    text_sha256     TEXT    NOT NULL CHECK (length(text_sha256) = 64 AND text_sha256 NOT GLOB '*[^0-9a-f]*'),
    status          TEXT    NOT NULL CHECK (status IN ('done', 'refused')),
    attempt         INTEGER NOT NULL CHECK (attempt BETWEEN 1 AND 3),
    doc_kind        TEXT    CHECK (doc_kind IS NULL OR doc_kind IN ('lecture', 'seminar', 'lab', 'homework', 'ticket_list', 'syllabus', 'other')),
    subject_id      TEXT    CHECK (subject_id IS NULL OR length(subject_id) BETWEEN 1 AND 128),
    lesson_date     TEXT    CHECK (lesson_date IS NULL OR lesson_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    has_homework    INTEGER NOT NULL DEFAULT 0 CHECK (has_homework IN (0, 1)),
    has_ticket_list INTEGER NOT NULL DEFAULT 0 CHECK (has_ticket_list IN (0, 1)),
    mentions_email  INTEGER NOT NULL DEFAULT 0 CHECK (mentions_email IN (0, 1)),
    version         INTEGER NOT NULL DEFAULT 0,                             -- optimistic concurrency (ADR-0004)
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    CHECK ((status = 'done') = (doc_kind IS NOT NULL)),
    CHECK (status = 'done' OR (subject_id IS NULL AND lesson_date IS NULL AND has_homework = 0 AND has_ticket_list = 0 AND mentions_email = 0))
);

-- summaries, assignments (migration 0024): the material a summary or an assignment was made from. Its summary id is
-- "material-<material id>" (summary.IDOfMaterial); an assignment of a material is found again by (material_id,
-- source_index), as one of a transcript. A deleted material leaves the rows (ON DELETE SET NULL).
ALTER TABLE summaries ADD COLUMN material_id TEXT REFERENCES materials (id) ON DELETE SET NULL;
CREATE UNIQUE INDEX idx_summaries_material ON summaries (material_id) WHERE material_id IS NOT NULL;
ALTER TABLE assignments ADD COLUMN material_id TEXT REFERENCES materials (id) ON DELETE SET NULL;
CREATE UNIQUE INDEX idx_assignments_material_source ON assignments (material_id, source_index)
    WHERE material_id IS NOT NULL AND source_index IS NOT NULL;

-- +goose Down
DROP INDEX idx_assignments_material_source;
ALTER TABLE assignments DROP COLUMN material_id;
DROP INDEX idx_summaries_material;
ALTER TABLE summaries DROP COLUMN material_id;
DROP TABLE material_inspections;
