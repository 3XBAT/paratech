-- Recognition load modes and the recognition speed measured on this computer (TASK-084, ADR-0016).
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema). The migration is self-contained: it changes only settings and its own table.

-- +goose Up
-- settings: how hard speech recognition may load the computer: 'gentle', 'normal' or 'max'. The aggregate reads any
-- other value as the default, so the column is only bounded, not restricted to the list.
ALTER TABLE settings ADD COLUMN recognition_load TEXT NOT NULL DEFAULT 'gentle' CHECK (length(recognition_load) <= 16);

-- ---------------------------------------------------------------------------------------------------------------
-- stt_speeds: the measured speed of an installed model in a load mode: the moving average of wall time over audio
-- duration of its runs (values.MeasuredSpeed) and how many runs it is based on. It refines the time estimates of the
-- pages. The rows go with the model (CASCADE). No version column: a row is replaced, never saved through an aggregate.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE stt_speeds (
    stt_model_id     TEXT    NOT NULL REFERENCES stt_models (id) ON DELETE CASCADE,
    recognition_load TEXT    NOT NULL CHECK (recognition_load IN ('gentle', 'normal', 'max')),
    factor           REAL    NOT NULL CHECK (factor > 0),
    samples          INTEGER NOT NULL CHECK (samples >= 1),
    updated_at       INTEGER NOT NULL,
    PRIMARY KEY (stt_model_id, recognition_load)
);

-- +goose Down
DROP TABLE stt_speeds;
ALTER TABLE settings DROP COLUMN recognition_load;
