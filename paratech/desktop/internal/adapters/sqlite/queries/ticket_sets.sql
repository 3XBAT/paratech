-- Exam ticket sets (TASK-133): the aggregate coursework/ticketset, saved with the optimistic-concurrency protocol of
-- ADR-0004(d).

-- name: GetTicketSet :one
SELECT * FROM ticket_sets WHERE id = sqlc.arg(id);

-- A never-stored set (version 0) is inserted with version 1.
-- name: InsertTicketSet :exec
INSERT INTO ticket_sets (id, subject_id, material_id, status, content, version, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(subject_id), sqlc.arg(material_id), sqlc.arg(status), sqlc.arg(content), 1,
        sqlc.arg(created_at), sqlc.arg(updated_at));

-- Compare-and-swap on the loaded version. The subject, the material and the creation time never change.
-- name: UpdateTicketSet :execrows
UPDATE ticket_sets
SET status     = sqlc.arg(status),
    content    = sqlc.arg(content),
    version    = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);
