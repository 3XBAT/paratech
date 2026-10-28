-- Deliveries, the attempts to email an artifact (PRD section 10, AC-15), with the optimistic-concurrency protocol of
-- ADR-0004(d). The idempotency key and the Message-ID are UNIQUE: a second delivery under the same key is refused.

-- name: GetDelivery :one
SELECT * FROM deliveries WHERE id = sqlc.arg(id);

-- name: GetDeliveryByIdempotencyKey :one
SELECT * FROM deliveries WHERE idempotency_key = sqlc.arg(idempotency_key);

-- name: ListDeliveriesByStatus :many
SELECT * FROM deliveries
WHERE status IN (sqlc.slice('statuses'))
ORDER BY created_at, id;

-- The deliveries of one assignment for the homework screen, newest first (idx_deliveries_assignment).
-- name: ListDeliveriesByAssignment :many
SELECT * FROM deliveries
WHERE assignment_id = sqlc.arg(assignment_id)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(max_rows);

-- A never-stored delivery (version 0) is inserted with version 1.
-- name: InsertDelivery :exec
INSERT INTO deliveries (id, assignment_id, recipient, subject, artifact_ref, attachment_sha256, attachment_size,
                        message_id, idempotency_key, status, attempts, send_round, last_error, confirmed_at, sent_at, version,
                        created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(assignment_id), sqlc.arg(recipient), sqlc.arg(subject), sqlc.arg(artifact_ref),
        sqlc.arg(attachment_sha256), sqlc.arg(attachment_size), sqlc.arg(message_id), sqlc.arg(idempotency_key),
        sqlc.arg(status), sqlc.arg(attempts), sqlc.arg(send_round), sqlc.arg(last_error), sqlc.arg(confirmed_at), sqlc.arg(sent_at), 1,
        sqlc.arg(created_at), sqlc.arg(updated_at));

-- Compare-and-swap on the loaded version. What the user confirmed (recipient, subject, attachment, Message-ID, key,
-- confirmation time) never changes after the insert, so only the progress of the delivery is written.
-- name: UpdateDelivery :execrows
UPDATE deliveries
SET status     = sqlc.arg(status),
    attempts   = sqlc.arg(attempts),
    send_round = sqlc.arg(send_round),
    last_error = sqlc.arg(last_error),
    sent_at    = sqlc.arg(sent_at),
    version    = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- The purge of a deleted assignment (ADR-0029 addendum 2): its deliveries in the passed final statuses only, so an
-- unresolved email is never forgotten (hard rule 7).
-- name: DeleteFinalDeliveriesOfAssignment :execrows
DELETE FROM deliveries WHERE assignment_id = sqlc.arg(assignment_id) AND status IN (sqlc.slice('statuses'));
