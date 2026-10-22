-- Assignments, the local homework (PRD section 10) with the optimistic-concurrency protocol of ADR-0004(d). The
-- deadline is due_at. Extracted rows are found again by (transcript_id, source_index) when a transcript is
-- reprocessed (TASK-042).

-- name: GetAssignment :one
SELECT * FROM assignments WHERE id = sqlc.arg(id);

-- name: GetAssignmentBySource :one
SELECT * FROM assignments WHERE transcript_id = sqlc.arg(transcript_id) AND source_index = sqlc.arg(source_index);

-- name: ListAssignmentsByTranscript :many
SELECT * FROM assignments WHERE transcript_id = sqlc.arg(transcript_id) ORDER BY source_index, id;

-- Assignments found in a library material (TASK-122), found again by (material_id, source_index).
-- name: GetAssignmentByMaterialSource :one
SELECT * FROM assignments WHERE material_id = sqlc.arg(material_id) AND source_index = sqlc.arg(source_index);

-- name: ListAssignmentsByMaterial :many
SELECT * FROM assignments WHERE material_id = sqlc.arg(material_id) ORDER BY source_index, id;

-- The newest assignments for the homework screen; the items of one extraction keep the order of the answer.
-- name: ListRecentAssignments :many
SELECT * FROM assignments ORDER BY created_at DESC, source_index, id LIMIT sqlc.arg(max_rows);

-- A never-stored assignment (version 0) is inserted with version 1.
-- name: InsertAssignment :exec
INSERT INTO assignments (id, lesson_id, transcript_id, material_id, title, description, due_at, user_edited, source_index,
                         prompt_version, hint_subject, hint_teacher, user_subject, user_teacher, version, created_at,
                         updated_at)
VALUES (sqlc.arg(id), sqlc.arg(lesson_id), sqlc.arg(transcript_id), sqlc.arg(material_id), sqlc.arg(title), sqlc.arg(description),
        sqlc.arg(due_at), sqlc.arg(user_edited), sqlc.arg(source_index), sqlc.arg(prompt_version),
        sqlc.arg(hint_subject), sqlc.arg(hint_teacher), sqlc.arg(user_subject), sqlc.arg(user_teacher), 1,
        sqlc.arg(created_at), sqlc.arg(updated_at));

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
    version        = version + 1,
    updated_at     = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- Compare-and-swap delete of an assignment reprocessing withdrew. A delivery that references it makes the delete fail
-- (ON DELETE RESTRICT), and the row is kept.
-- name: DeleteAssignment :execrows
DELETE FROM assignments WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);
