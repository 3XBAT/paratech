-- Measured recognition speed per installed model and load mode (TASK-084). A row is replaced by
-- (stt_model_id, recognition_load); the rows go with the model (CASCADE).

-- name: GetSttSpeed :one
SELECT factor, samples FROM stt_speeds
WHERE stt_model_id = sqlc.arg(stt_model_id) AND recognition_load = sqlc.arg(recognition_load);

-- name: ListSttSpeeds :many
SELECT stt_model_id, recognition_load, factor, samples FROM stt_speeds
ORDER BY stt_model_id, recognition_load;

-- name: UpsertSttSpeed :exec
INSERT INTO stt_speeds (stt_model_id, recognition_load, factor, samples, updated_at)
VALUES (sqlc.arg(stt_model_id), sqlc.arg(recognition_load), sqlc.arg(factor), sqlc.arg(samples), sqlc.arg(updated_at))
ON CONFLICT (stt_model_id, recognition_load) DO UPDATE
SET factor     = excluded.factor,
    samples    = excluded.samples,
    updated_at = excluded.updated_at;
