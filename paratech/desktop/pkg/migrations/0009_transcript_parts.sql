-- Live transcript parts (TASK-079): what speech recognition produced so far while it runs.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema). The migration is self-contained: it only creates its own table.

-- +goose Up
-- ---------------------------------------------------------------------------------------------------------------
-- transcript_parts: the segments of a recording recognised so far, stored in batches while the STT job runs, so the
-- transcript viewer shows them during recognition and after a restart (TASK-079). Values are transcript.Part. seq is
-- the number in the engine run; a rerun numbers from 0 again and replaces rows by (recording_id, seq). The rows are a
-- preview: the STT job deletes them once the transcript is stored, and they go with the recording (CASCADE). No
-- version column: rows are replaced, never edited through an aggregate.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE transcript_parts (
    recording_id TEXT    NOT NULL REFERENCES recordings (id) ON DELETE CASCADE,
    seq          INTEGER NOT NULL CHECK (seq >= 0),
    start_ms     INTEGER NOT NULL CHECK (start_ms >= 0),
    end_ms       INTEGER NOT NULL CHECK (end_ms >= start_ms),
    text         TEXT    NOT NULL,
    PRIMARY KEY (recording_id, seq)
);

-- +goose Down
DROP TABLE transcript_parts;
