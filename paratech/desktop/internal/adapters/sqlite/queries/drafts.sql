-- Homework drafts (PRD section 10): the root row carries the version of the optimistic-concurrency protocol of
-- ADR-0004(d), the versions of a draft are append-only rows of homework_draft_revisions.

-- The draft with its versions in one statement, so that a concurrent save cannot pair the version of the root with the
-- versions of another state. A root is never stored without a version, so no row means no draft.
-- name: GetDraft :many
SELECT d.version AS draft_version, r.number, r.origin, r.title, r.body, r.instruction, r.prompt_version, r.job_id,
       r.created_at
FROM homework_drafts d
JOIN homework_draft_revisions r ON r.assignment_id = d.assignment_id
WHERE d.assignment_id = sqlc.arg(assignment_id)
ORDER BY r.number;

-- The number of the latest stored version, 0 when none; the repository inserts only the versions above it.
-- name: LatestDraftRevision :one
SELECT CAST(COALESCE(MAX(number), 0) AS INTEGER) AS latest
FROM homework_draft_revisions
WHERE assignment_id = sqlc.arg(assignment_id);

-- A never-stored draft (version 0) is inserted with version 1.
-- name: InsertDraft :exec
INSERT INTO homework_drafts (assignment_id, version) VALUES (sqlc.arg(assignment_id), 1);

-- Compare-and-swap on the loaded version.
-- name: UpdateDraftVersion :execrows
UPDATE homework_drafts
SET version = version + 1
WHERE assignment_id = sqlc.arg(assignment_id) AND version = sqlc.arg(expected_version);

-- name: InsertDraftRevision :exec
INSERT INTO homework_draft_revisions (assignment_id, number, origin, title, body, instruction, prompt_version, job_id,
                                      created_at)
VALUES (sqlc.arg(assignment_id), sqlc.arg(number), sqlc.arg(origin), sqlc.arg(title), sqlc.arg(body),
        sqlc.arg(instruction), sqlc.arg(prompt_version), sqlc.arg(job_id), sqlc.arg(created_at));
