-- Assignments, the local homework (PRD section 10) with the optimistic-concurrency protocol of ADR-0004(d). The
-- deadline is due_at. Extracted rows are found again by (transcript_id, source_index) when a transcript is
-- reprocessed (TASK-042). A deleted row (deleted_at, ADR-0029) is loaded by id and by source, so that the restore and the
-- tombstone of reprocessing see it, and skipped by every list.

-- name: GetAssignment :one
SELECT * FROM assignments WHERE id = sqlc.arg(id);

-- name: GetAssignmentBySource :one
SELECT * FROM assignments WHERE transcript_id = sqlc.arg(transcript_id) AND source_index = sqlc.arg(source_index);

-- name: ListAssignmentsByTranscript :many
SELECT * FROM assignments
WHERE transcript_id = sqlc.arg(transcript_id) AND deleted_at IS NULL
ORDER BY source_index, id;

-- Assignments found in a library material (TASK-122), found again by (material_id, source_index).
-- name: GetAssignmentByMaterialSource :one
SELECT * FROM assignments WHERE material_id = sqlc.arg(material_id) AND source_index = sqlc.arg(source_index);

-- name: ListAssignmentsByMaterial :many
SELECT * FROM assignments
WHERE material_id = sqlc.arg(material_id) AND deleted_at IS NULL
ORDER BY source_index, id;

-- The newest assignments for the homework screen; the items of one extraction keep the order of the answer.
-- name: ListRecentAssignments :many
SELECT * FROM assignments
WHERE deleted_at IS NULL
ORDER BY created_at DESC, source_index, id
LIMIT sqlc.arg(max_rows);

-- The same list filtered by the done mark (ADR-0033 section 9) before the limit: the open rows when with_open is 1, the done
-- rows when with_done is 1, open rows first.
-- name: ListRecentAssignmentsByState :many
SELECT * FROM assignments
WHERE deleted_at IS NULL
  AND ((done_at IS NULL AND CAST(sqlc.arg(with_open) AS INTEGER) = 1)
    OR (done_at IS NOT NULL AND CAST(sqlc.arg(with_done) AS INTEGER) = 1))
ORDER BY done_at IS NOT NULL, created_at DESC, source_index, id
LIMIT sqlc.arg(max_rows);

-- A never-stored assignment (version 0) is inserted with version 1.
-- name: InsertAssignment :exec
INSERT INTO assignments (id, lesson_id, transcript_id, material_id, title, description, due_at, user_edited, source_index,
                         prompt_version, hint_subject, hint_teacher, user_subject, user_teacher, version, created_at,
                         updated_at, deleted_at, done_at)
VALUES (sqlc.arg(id), sqlc.arg(lesson_id), sqlc.arg(transcript_id), sqlc.arg(material_id), sqlc.arg(title), sqlc.arg(description),
        sqlc.arg(due_at), sqlc.arg(user_edited), sqlc.arg(source_index), sqlc.arg(prompt_version),
        sqlc.arg(hint_subject), sqlc.arg(hint_teacher), sqlc.arg(user_subject), sqlc.arg(user_teacher), 1,
        sqlc.arg(created_at), sqlc.arg(updated_at), sqlc.arg(deleted_at), sqlc.arg(done_at));

-- Compare-and-swap on the loaded version. The origin (lesson, transcript or material, source index) and the creation
-- time never change, so a stale save cannot undo ON DELETE SET NULL of the lesson, the transcript or the material.
-- name: UpdateAssignment :execrows
UPDATE assignments
SET title          = sqlc.arg(title),
    description    = sqlc.arg(description),
    due_at         = sqlc.arg(due_at),
    user_edited    = sqlc.arg(user_edited),
    prompt_version = sqlc.arg(prompt_version),
    hint_subject   = sqlc.arg(hint_subject),
    hint_teacher   = sqlc.arg(hint_teacher),
    user_subject   = sqlc.arg(user_subject),
    user_teacher   = sqlc.arg(user_teacher),
    deleted_at     = sqlc.arg(deleted_at),
    done_at        = sqlc.arg(done_at),
    version        = version + 1,
    updated_at     = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- Compare-and-swap delete of an assignment reprocessing withdrew, or of one the student added (after
-- AssignmentInUse). A delivery that references it makes the delete fail
-- (ON DELETE RESTRICT), and the row is kept.
-- name: DeleteAssignment :execrows
DELETE FROM assignments WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- What references an assignment the student added before the Undo of its creation removes it (ADR-0029). works
-- cascade on delete and proposals have no foreign key, so both are checked here; the caller passes the proposal
-- statuses that leave the assignment free. A draft generation that has not finished has no homework_drafts row yet,
-- so a job of the draft type in one of the passed active statuses whose payload names the assignment counts too.
-- Both status lists are JSON arrays: sqlc.slice next to a repeated named argument binds the wrong values.
-- name: AssignmentInUse :one
SELECT CAST(EXISTS (SELECT 1 FROM homework_drafts d WHERE d.assignment_id = sqlc.arg(id))
    OR EXISTS (SELECT 1 FROM deliveries v WHERE v.assignment_id = sqlc.arg(id))
    OR EXISTS (SELECT 1 FROM works w WHERE w.assignment_id = sqlc.arg(id))
    OR EXISTS (SELECT 1 FROM proposals p
               WHERE p.assignment_id = sqlc.arg(id)
                 AND p.status NOT IN (SELECT value FROM json_each(CAST(sqlc.arg(free_statuses) AS TEXT))))
    OR EXISTS (SELECT 1 FROM jobs j
               WHERE j.type = sqlc.arg(draft_job_type)
                 AND j.status IN (SELECT value FROM json_each(CAST(sqlc.arg(active_job_statuses) AS TEXT)))
                 AND json_extract(j.payload, '$.assignment_id') = sqlc.arg(id)) AS INTEGER) AS in_use;

-- What keeps the soft delete of an assignment from going through (ADR-0029, hard rule 7): a delivery that may still go
-- out or may have gone out unseen. A work or a job does not: the delete stops them (TASK-338). The caller passes the
-- statuses as a JSON array (see AssignmentRepository.Busy).
-- name: AssignmentBusy :one
SELECT CAST(EXISTS (SELECT 1 FROM deliveries v
                    WHERE v.assignment_id = sqlc.arg(id)
                      AND v.status IN (SELECT value FROM json_each(CAST(sqlc.arg(delivery_statuses) AS TEXT)))) AS INTEGER) AS busy;

-- What the soft delete of an assignment stops (TASK-338): a work being triaged or written, or an unfinished job (draft
-- generation, writing) whose payload names it. The caller passes the lists as JSON arrays (see
-- AssignmentRepository.Working).
-- name: AssignmentWorking :one
SELECT CAST(EXISTS (SELECT 1 FROM works w
                    WHERE w.assignment_id = sqlc.arg(id)
                      AND w.status IN (SELECT value FROM json_each(CAST(sqlc.arg(work_statuses) AS TEXT))))
    OR EXISTS (SELECT 1 FROM jobs j
               WHERE j.type IN (SELECT value FROM json_each(CAST(sqlc.arg(job_types) AS TEXT)))
                 AND j.status IN (SELECT value FROM json_each(CAST(sqlc.arg(active_job_statuses) AS TEXT)))
                 AND json_extract(j.payload, '$.assignment_id') = sqlc.arg(id)) AS INTEGER) AS working;

-- The unfinished jobs (draft generation, writing) whose payload names an assignment, oldest first, which its soft
-- delete cancels (TASK-338).
-- name: ListActiveAssignmentJobs :many
SELECT j.id FROM jobs j
WHERE j.type IN (SELECT value FROM json_each(CAST(sqlc.arg(job_types) AS TEXT)))
  AND j.status IN (SELECT value FROM json_each(CAST(sqlc.arg(active_job_statuses) AS TEXT)))
  AND json_extract(j.payload, '$.assignment_id') = sqlc.arg(id)
ORDER BY j.created_at, j.id;

-- The purge after the restore window (ADR-0029 addendum 2): the assignments deleted before the cutoff, oldest delete
-- first, after the keyset (after_at, after_id) of the previous page.
-- name: ListAssignmentsDeletedBefore :many
SELECT * FROM assignments
WHERE deleted_at IS NOT NULL AND deleted_at < sqlc.arg(cutoff)
  AND (deleted_at > sqlc.arg(after_at) OR (deleted_at = sqlc.arg(after_at) AND id > sqlc.arg(after_id)))
ORDER BY deleted_at, id
LIMIT sqlc.arg(max_rows);

-- The re-check of the purge inside its transaction: is assignment id still deleted before the cutoff?
-- name: AssignmentDeletedBefore :one
SELECT CAST(EXISTS (SELECT 1 FROM assignments
                    WHERE id = sqlc.arg(id) AND deleted_at IS NOT NULL AND deleted_at < sqlc.arg(cutoff)) AS INTEGER) AS due;

-- The last step of the purge. The guard is repeated here, so a row restored meanwhile is kept. A draft or a delivery
-- still referencing it fails the delete (ON DELETE RESTRICT); works cascade.
-- name: PurgeDeletedAssignment :execrows
DELETE FROM assignments
WHERE id = sqlc.arg(id) AND deleted_at IS NOT NULL AND deleted_at < sqlc.arg(cutoff);
