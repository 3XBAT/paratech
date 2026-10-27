-- Old recordings made elsewhere (TASK-161, ADR-0027): a recording is captured by this app or imported from an audio
-- file; an import keeps the SHA-256 of its bytes, so the same file twice is the same recording.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- recordings (migration 0042): origin 'captured' (every earlier row) or 'imported'; source_sha256 is the lowercase hex
-- digest of an imported file, NULL for a captured recording.
ALTER TABLE recordings ADD COLUMN origin TEXT NOT NULL DEFAULT 'captured' CHECK (origin IN ('captured', 'imported'));
ALTER TABLE recordings ADD COLUMN source_sha256 TEXT CHECK (source_sha256 IS NULL OR (length(source_sha256) = 64 AND source_sha256 NOT GLOB '*[^0-9a-f]*'));
CREATE UNIQUE INDEX idx_recordings_source_sha256 ON recordings (source_sha256) WHERE source_sha256 IS NOT NULL;

-- +goose Down
DROP INDEX idx_recordings_source_sha256;
ALTER TABLE recordings DROP COLUMN source_sha256;
ALTER TABLE recordings DROP COLUMN origin;
