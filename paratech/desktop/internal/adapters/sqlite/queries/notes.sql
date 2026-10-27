-- The student's own notes (TASK-149, ADR-0027): a note with its anchor and its ordered parts. The note follows the
-- optimistic-concurrency protocol of ADR-0004(d); its parts carry no version of their own.

-- name: GetNote :one
SELECT * FROM notes WHERE id = sqlc.arg(id);

-- By the date of the anchor, undated last, then as made.
-- name: ListNotesOfSubject :many
SELECT * FROM notes WHERE subject_id = sqlc.arg(subject_id)
ORDER BY anchor_date IS NULL, anchor_date, created_at, id;

-- Every note, for the knowledge base (TASK-152); ordered as ListNotesOfSubject.
-- name: ListNotes :many
SELECT * FROM notes ORDER BY anchor_date IS NULL, anchor_date, created_at, id;

-- name: ListNotesOfRecording :many
SELECT * FROM notes WHERE recording_id = sqlc.arg(recording_id) ORDER BY created_at, id;

-- name: ListNotesOfLesson :many
SELECT * FROM notes WHERE lesson_id = sqlc.arg(lesson_id) ORDER BY created_at, id;

-- A never-stored note (version 0) is inserted with version 1.
-- name: InsertNote :exec
INSERT INTO notes (id, subject_id, title, anchor_kind, recording_id, lesson_id, anchor_date, lesson_kind, lesson_title,
                   status, version, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(subject_id), sqlc.arg(title), sqlc.arg(anchor_kind), sqlc.arg(recording_id),
        sqlc.arg(lesson_id), sqlc.arg(anchor_date), sqlc.arg(lesson_kind), sqlc.arg(lesson_title), sqlc.arg(status), 1,
        sqlc.arg(created_at), sqlc.arg(updated_at));

-- The subject of a note never changes.
-- name: UpdateNote :execrows
UPDATE notes
SET title        = sqlc.arg(title),
    anchor_kind  = sqlc.arg(anchor_kind),
    recording_id = sqlc.arg(recording_id),
    lesson_id    = sqlc.arg(lesson_id),
    anchor_date  = sqlc.arg(anchor_date),
    lesson_kind  = sqlc.arg(lesson_kind),
    lesson_title = sqlc.arg(lesson_title),
    status       = sqlc.arg(status),
    version      = version + 1,
    updated_at   = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- name: ListNoteParts :many
SELECT material_id FROM note_parts WHERE note_id = sqlc.arg(note_id) ORDER BY position;

-- name: DeleteNoteParts :exec
DELETE FROM note_parts WHERE note_id = sqlc.arg(note_id);

-- name: InsertNotePart :exec
INSERT INTO note_parts (note_id, material_id, position)
VALUES (sqlc.arg(note_id), sqlc.arg(material_id), sqlc.arg(position));
