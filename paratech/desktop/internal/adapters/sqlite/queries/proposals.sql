-- Action proposals of the LLM (PRD section 13.3) with the optimistic-concurrency protocol of ADR-0004(d).

-- name: GetProposal :one
SELECT * FROM proposals WHERE id = sqlc.arg(id);

-- A never-stored proposal (version 0) is inserted with version 1.
-- name: InsertProposal :exec
INSERT INTO proposals (id, kind, status, assignment_id, title, body, due_at, reason, recipient, attachment_sha256,
                       attachment_size, assignment_title, attachment_format, confirmed_at, executed_at, version,
                       created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(kind), sqlc.arg(status), sqlc.arg(assignment_id), sqlc.arg(title), sqlc.arg(body),
        sqlc.arg(due_at), sqlc.arg(reason), sqlc.arg(recipient), sqlc.arg(attachment_sha256), sqlc.arg(attachment_size),
        sqlc.arg(assignment_title), sqlc.arg(attachment_format), sqlc.arg(confirmed_at), sqlc.arg(executed_at), 1,
        sqlc.arg(created_at), sqlc.arg(updated_at));

-- Compare-and-swap on the loaded version. What the model proposed (kind, assignment, title, body, due), the attachment
-- format the user chose before the validation and the creation time never change, so they are not written again.
-- name: UpdateProposal :execrows
UPDATE proposals
SET status            = sqlc.arg(status),
    reason            = sqlc.arg(reason),
    recipient         = sqlc.arg(recipient),
    attachment_sha256 = sqlc.arg(attachment_sha256),
    attachment_size   = sqlc.arg(attachment_size),
    assignment_title  = sqlc.arg(assignment_title),
    confirmed_at      = sqlc.arg(confirmed_at),
    executed_at       = sqlc.arg(executed_at),
    version           = version + 1,
    updated_at        = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);
