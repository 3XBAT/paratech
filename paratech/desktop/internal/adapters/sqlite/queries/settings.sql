-- Settings singleton (row id = 1) of the settings repository, with the optimistic-concurrency protocol of ADR-0004(d).
-- No row means the defaults of the aggregate (version 0).

-- name: GetSettings :one
SELECT id, recording_prompts_enabled, process_only_on_ac_power, default_stt_model_id, version, updated_at,
       homework_recipient, smtp_host, smtp_port, smtp_security, smtp_username, hub_url, recording_source_app,
       recognition_load
FROM settings
WHERE id = 1;

-- A never-stored settings aggregate (version 0) is inserted with version 1. A second insert violates the primary key,
-- which the adapter reports as shared.ErrVersionConflict (someone else created the row first).
-- name: InsertSettings :exec
INSERT INTO settings (id, recording_prompts_enabled, process_only_on_ac_power, default_stt_model_id,
                      homework_recipient, smtp_host, smtp_port, smtp_security, smtp_username, hub_url,
                      recording_source_app, recognition_load, version, updated_at)
VALUES (1, sqlc.arg(recording_prompts_enabled), sqlc.arg(process_only_on_ac_power), sqlc.arg(default_stt_model_id),
        sqlc.arg(homework_recipient), sqlc.arg(smtp_host), sqlc.arg(smtp_port), sqlc.arg(smtp_security),
        sqlc.arg(smtp_username), sqlc.arg(hub_url), sqlc.arg(recording_source_app),
        sqlc.arg(recognition_load), 1, sqlc.arg(updated_at));

-- Compare-and-swap on the loaded version. Zero rows affected means a stale version (or no row): the caller maps that
-- with VersionCAS to shared.ErrVersionConflict.
-- name: UpdateSettings :execrows
UPDATE settings
SET recording_prompts_enabled = sqlc.arg(recording_prompts_enabled),
    process_only_on_ac_power  = sqlc.arg(process_only_on_ac_power),
    default_stt_model_id      = sqlc.arg(default_stt_model_id),
    homework_recipient        = sqlc.arg(homework_recipient),
    smtp_host                 = sqlc.arg(smtp_host),
    smtp_port                 = sqlc.arg(smtp_port),
    smtp_security             = sqlc.arg(smtp_security),
    smtp_username             = sqlc.arg(smtp_username),
    hub_url                   = sqlc.arg(hub_url),
    recording_source_app      = sqlc.arg(recording_source_app),
    recognition_load          = sqlc.arg(recognition_load),
    version                   = version + 1,
    updated_at                = sqlc.arg(updated_at)
WHERE id = 1 AND version = sqlc.arg(expected_version);
