-- The reading of a library material by the quarantined reader (TASK-122, ADR-0022): one row per material with the
-- optimistic-concurrency protocol of ADR-0004(d).

-- name: GetInspection :one
SELECT * FROM material_inspections WHERE material_id = sqlc.arg(material_id);

-- A never-stored inspection (version 0) is inserted with version 1.
-- name: InsertInspection :exec
INSERT INTO material_inspections (material_id, text_sha256, status, attempt, doc_kind, subject_id, lesson_date,
                                  has_homework, has_ticket_list, mentions_email, version, created_at, updated_at)
VALUES (sqlc.arg(material_id), sqlc.arg(text_sha256), sqlc.arg(status), sqlc.arg(attempt), sqlc.arg(doc_kind),
        sqlc.arg(subject_id), sqlc.arg(lesson_date), sqlc.arg(has_homework), sqlc.arg(has_ticket_list),
        sqlc.arg(mentions_email), 1, sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: UpdateInspection :execrows
UPDATE material_inspections
SET text_sha256     = sqlc.arg(text_sha256),
    status          = sqlc.arg(status),
    attempt         = sqlc.arg(attempt),
    doc_kind        = sqlc.arg(doc_kind),
    subject_id      = sqlc.arg(subject_id),
    lesson_date     = sqlc.arg(lesson_date),
    has_homework    = sqlc.arg(has_homework),
    has_ticket_list = sqlc.arg(has_ticket_list),
    mentions_email  = sqlc.arg(mentions_email),
    version         = version + 1,
    updated_at      = sqlc.arg(updated_at)
WHERE material_id = sqlc.arg(material_id) AND version = sqlc.arg(expected_version);
