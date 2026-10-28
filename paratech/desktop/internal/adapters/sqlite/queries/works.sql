-- Turnkey works (TASK-129, ADR-0025): the aggregate coursework/work with its answered reader rounds, saved with the
-- optimistic-concurrency protocol of ADR-0004(d); the rounds carry no version of their own.

-- name: GetWork :one
SELECT * FROM works WHERE id = sqlc.arg(id);

-- The work of an assignment that has not failed; at most one (idx_works_assignment).
-- name: GetActiveWorkOfAssignment :one
SELECT * FROM works WHERE assignment_id = sqlc.arg(assignment_id) AND status <> 'failed';

-- The work of a library material that has not failed; at most one (idx_works_material).
-- name: GetActiveWorkOfMaterial :one
SELECT * FROM works WHERE material_id = sqlc.arg(material_id) AND status <> 'failed';

-- The work of a topic request that has not failed; at most one (idx_works_topic).
-- name: GetActiveWorkOfTopic :one
SELECT * FROM works WHERE topic_request = sqlc.arg(topic_request) AND status <> 'failed';

-- A never-stored work (version 0) is inserted with version 1.
-- name: InsertWork :exec
INSERT INTO works (id, assignment_id, material_id, topic_request, topic, topic_subject_id, status, round, attempt,
                   edit_work_type, edit_volume, edit_file_format, edit_formatting, edit_variant, edit_deadline,
                   edit_sources, writing, last_problem, sent_at, version, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(assignment_id), sqlc.arg(material_id), sqlc.arg(topic_request), sqlc.arg(topic),
        sqlc.arg(topic_subject_id), sqlc.arg(status), sqlc.arg(round), sqlc.arg(attempt), sqlc.arg(edit_work_type),
        sqlc.arg(edit_volume), sqlc.arg(edit_file_format), sqlc.arg(edit_formatting), sqlc.arg(edit_variant),
        sqlc.arg(edit_deadline), sqlc.arg(edit_sources), sqlc.arg(writing), sqlc.arg(last_problem),
        sqlc.arg(sent_at), 1, sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: UpdateWork :execrows
UPDATE works
SET status           = sqlc.arg(status),
    round            = sqlc.arg(round),
    attempt          = sqlc.arg(attempt),
    edit_work_type   = sqlc.arg(edit_work_type),
    edit_volume      = sqlc.arg(edit_volume),
    edit_file_format = sqlc.arg(edit_file_format),
    edit_formatting  = sqlc.arg(edit_formatting),
    edit_variant     = sqlc.arg(edit_variant),
    edit_deadline    = sqlc.arg(edit_deadline),
    edit_sources     = sqlc.arg(edit_sources),
    writing          = sqlc.arg(writing),
    last_problem     = sqlc.arg(last_problem),
    sent_at          = sqlc.arg(sent_at),
    version          = version + 1,
    updated_at       = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- When a send of the work to the teacher was last SENT (TASK-226), 0 if never: a send_work_email proposal of the work
-- and the delivery keyed by it (proposal.EmailCommand.IdempotencyKey is 'proposal:' and the proposal id).
-- name: LastWorkSentAt :one
SELECT CAST(COALESCE(MAX(d.sent_at), 0) AS INTEGER) AS sent_at
FROM proposals p
JOIN deliveries d ON d.idempotency_key = 'proposal:' || p.id
WHERE p.kind = 'send_work_email' AND p.work_id = sqlc.arg(work_id) AND d.status = 'SENT';

-- Removes the work with its rounds (CASCADE) if it is still at the version the caller saw.
-- name: DeleteWork :execrows
DELETE FROM works WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- name: ListWorkRounds :many
SELECT * FROM work_rounds WHERE work_id = sqlc.arg(work_id) ORDER BY round;

-- A round is written once with its answer; its replies are set when the student answers.
-- name: UpsertWorkRound :exec
INSERT INTO work_rounds (work_id, round, answer, replies)
VALUES (sqlc.arg(work_id), sqlc.arg(round), sqlc.arg(answer), sqlc.arg(replies))
ON CONFLICT (work_id, round) DO UPDATE SET answer = excluded.answer, replies = excluded.replies;

-- The purge of a deleted assignment (ADR-0029 addendum 2): its works, the rounds go with them (CASCADE); only while
-- the assignment is still deleted before the cutoff.
-- name: DeleteWorksOfAssignment :execrows
DELETE FROM works
WHERE assignment_id = sqlc.arg(assignment_id)
  AND EXISTS (SELECT 1 FROM assignments a
              WHERE a.id = works.assignment_id AND a.deleted_at IS NOT NULL AND a.deleted_at < sqlc.arg(cutoff));
