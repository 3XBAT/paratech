-- Answers with citations (TASK-118, ADR-0022): the aggregate coursework/answer, written once and never saved again.

-- name: GetAnswer :one
SELECT * FROM answers WHERE id = sqlc.arg(id);

-- The answer of a reader request key; at most one (request_key is UNIQUE).
-- name: GetAnswerByKey :one
SELECT * FROM answers WHERE request_key = sqlc.arg(request_key);

-- name: InsertAnswer :exec
INSERT INTO answers (id, request_key, subject_id, question, fragments, claims, missing, sources, created_at)
VALUES (sqlc.arg(id), sqlc.arg(request_key), sqlc.arg(subject_id), sqlc.arg(question), sqlc.arg(fragments),
        sqlc.arg(claims), sqlc.arg(missing), sqlc.arg(sources), sqlc.arg(created_at));
