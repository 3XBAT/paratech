-- Installed STT models (PRD section 8) with the optimistic-concurrency protocol of ADR-0004(d), and the pin lookup
-- that blocks the removal of a model an unfinished job depends on (PRD section 14 case 11).

-- name: GetSttModel :one
SELECT * FROM stt_models WHERE id = sqlc.arg(id);

-- name: ListSttModels :many
SELECT * FROM stt_models ORDER BY name, model_version, id;

-- A never-stored model (version 0) is inserted with version 1. A second (name, model_version) violates the UNIQUE
-- rule and surfaces as shared.ErrDuplicate.
-- name: InsertSttModel :exec
INSERT INTO stt_models (id, name, model_version, catalog_id, file_ref, size_bytes, sha256, quantization,
                        capabilities, installed_at, version)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(model_version), sqlc.arg(catalog_id), sqlc.arg(file_ref),
        sqlc.arg(size_bytes), sqlc.arg(sha256), sqlc.arg(quantization), sqlc.arg(capabilities),
        sqlc.arg(installed_at), 1);

-- Compare-and-swap on the loaded version. An installed model does not change today; the update exists so that Save
-- keeps the protocol of every repository.
-- name: UpdateSttModel :execrows
UPDATE stt_models
SET name          = sqlc.arg(name),
    model_version = sqlc.arg(model_version),
    catalog_id    = sqlc.arg(catalog_id),
    file_ref      = sqlc.arg(file_ref),
    size_bytes    = sqlc.arg(size_bytes),
    sha256        = sqlc.arg(sha256),
    quantization  = sqlc.arg(quantization),
    capabilities  = sqlc.arg(capabilities),
    installed_at  = sqlc.arg(installed_at),
    version       = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- The settings default references the model ON DELETE RESTRICT: the delete then fails with a foreign key error.
-- name: DeleteSttModel :execrows
DELETE FROM stt_models WHERE id = sqlc.arg(id);

-- The caller passes the unfinished statuses (job.ActiveStatuses); the adapter holds no rule about them.
-- name: IsSttModelPinned :one
SELECT EXISTS (
    SELECT 1 FROM jobs
    WHERE pinned_model_id = sqlc.arg(model_id) AND status IN (sqlc.slice('statuses'))
) AS pinned;
