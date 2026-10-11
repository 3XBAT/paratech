-- Settings singleton (row id = 1) with the optimistic-concurrency protocol of ADR-0004(d).
-- These queries are the first consumer of the toolchain; the settings repository (TASK-007 owner) builds on them.

-- name: GetSettings :one
SELECT id, recording_prompts_enabled, process_only_on_ac_power, notify_on_processing_complete,
       default_stt_model_id, version, updated_at
FROM settings
WHERE id = 1;

-- A never-stored settings aggregate (version 0) is inserted with version 1. A second insert violates the primary key,
-- which the adapter reports as shared.ErrVersionConflict (someone else created the row first).
-- name: InsertSettings :exec
INSERT INTO settings (id, recording_prompts_enabled, process_only_on_ac_power, notify_on_processing_complete,
                      default_stt_model_id, version, updated_at)
VALUES (1, sqlc.arg(recording_prompts_enabled), sqlc.arg(process_only_on_ac_power),
        sqlc.arg(notify_on_processing_complete), sqlc.arg(default_stt_model_id), 1, sqlc.arg(updated_at));

-- Compare-and-swap on the loaded version. Zero rows affected means a stale version (or no row): the caller maps that
-- with VersionCAS to shared.ErrVersionConflict.
-- name: UpdateSettings :execrows
UPDATE settings
SET recording_prompts_enabled     = sqlc.arg(recording_prompts_enabled),
    process_only_on_ac_power      = sqlc.arg(process_only_on_ac_power),
    notify_on_processing_complete = sqlc.arg(notify_on_processing_complete),
    default_stt_model_id          = sqlc.arg(default_stt_model_id),
    version                       = version + 1,
    updated_at                    = sqlc.arg(updated_at)
WHERE id = 1 AND version = sqlc.arg(expected_version);
