-- Posts after lessons on «Сегодня» (TASK-125, ADR-0021): one post per recording, what its post-processing produced.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- ---------------------------------------------------------------------------------------------------------------
-- today_posts: the post after a lesson (aggregate agent/post). The recording is the identity, so a repeated completion
-- event adds no second post; it goes with its recording. summary_at is when the summary of transcript_id was stored,
-- homework_at when its homework was looked for (NULL: not yet). No text: the screen builds it from these facts.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE today_posts (
    recording_id  TEXT    PRIMARY KEY REFERENCES recordings (id) ON DELETE CASCADE,
    transcript_id TEXT    NOT NULL CHECK (length(transcript_id) BETWEEN 1 AND 128),
    summary_at    INTEGER,
    homework_at   INTEGER,
    version       INTEGER NOT NULL DEFAULT 0,                               -- optimistic concurrency (ADR-0004)
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    CHECK (summary_at IS NOT NULL OR homework_at IS NOT NULL)
);
CREATE INDEX idx_today_posts_updated ON today_posts (updated_at);

-- +goose Down
DROP INDEX idx_today_posts_updated;
DROP TABLE today_posts;
