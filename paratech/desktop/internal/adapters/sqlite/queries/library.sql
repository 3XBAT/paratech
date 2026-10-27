-- The library (TASK-121, ADR-0024): subjects with their aliases and folders, materials with their lesson links. Both
-- aggregates follow the optimistic-concurrency protocol of ADR-0004(d); child rows carry no version of their own.

-- name: ListSubjects :many
SELECT * FROM subjects ORDER BY name_key, id;

-- name: GetSubject :one
SELECT * FROM subjects WHERE id = sqlc.arg(id);

-- A never-stored subject (version 0) is inserted with version 1.
-- name: InsertSubject :exec
INSERT INTO subjects (id, name, name_key, semester, teacher_name, teacher_email, version, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(name_key), sqlc.arg(semester), sqlc.arg(teacher_name),
        sqlc.arg(teacher_email), 1, sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: UpdateSubject :execrows
UPDATE subjects
SET name          = sqlc.arg(name),
    name_key      = sqlc.arg(name_key),
    semester      = sqlc.arg(semester),
    teacher_name  = sqlc.arg(teacher_name),
    teacher_email = sqlc.arg(teacher_email),
    version       = version + 1,
    updated_at    = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- Removes the subject with its aliases and folders (CASCADE); a material still filed under it fails the RESTRICT.
-- name: DeleteSubject :execrows
DELETE FROM subjects WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- name: ListAllSubjectAliases :many
SELECT * FROM subject_aliases ORDER BY subject_id, position;

-- name: ListSubjectAliases :many
SELECT * FROM subject_aliases WHERE subject_id = sqlc.arg(subject_id) ORDER BY position;

-- name: DeleteSubjectAliases :exec
DELETE FROM subject_aliases WHERE subject_id = sqlc.arg(subject_id);

-- name: InsertSubjectAlias :exec
INSERT INTO subject_aliases (alias_key, subject_id, alias, position)
VALUES (sqlc.arg(alias_key), sqlc.arg(subject_id), sqlc.arg(alias), sqlc.arg(position));

-- name: ListAllSubjectFolders :many
SELECT * FROM subject_folders ORDER BY subject_id, position, id;

-- name: ListSubjectFolders :many
SELECT * FROM subject_folders WHERE subject_id = sqlc.arg(subject_id) ORDER BY position, id;

-- A folder goes after the others: its position is one more than the largest of its subject.
-- name: InsertSubjectFolder :exec
INSERT INTO subject_folders (id, subject_id, name, name_key, system, position)
VALUES (sqlc.arg(id), sqlc.arg(subject_id), sqlc.arg(name), sqlc.arg(name_key), sqlc.arg(system),
        (SELECT COALESCE(MAX(f.position), -1) + 1 FROM subject_folders f WHERE f.subject_id = sqlc.arg(subject_id)));

-- Folders are never renamed, so a save only inserts and deletes them; a folder that holds a file fails the RESTRICT.
-- name: DeleteSubjectFolder :exec
DELETE FROM subject_folders WHERE subject_id = sqlc.arg(subject_id) AND id = sqlc.arg(id);

-- name: GetMaterial :one
SELECT * FROM materials WHERE id = sqlc.arg(id);

-- name: GetMaterialBySHA256 :one
SELECT * FROM materials WHERE sha256 = sqlc.arg(sha256);

-- name: ListMaterialsOfSubject :many
SELECT * FROM materials WHERE subject_id = sqlc.arg(subject_id) ORDER BY created_at DESC, id;

-- name: ListInboxMaterials :many
SELECT * FROM materials WHERE subject_id IS NULL ORDER BY created_at DESC, id;

-- Files of a subject in any status: a removed one still blocks the removal of its subject.
-- name: CountMaterialsOfSubject :one
SELECT count(*) FROM materials WHERE subject_id = sqlc.arg(subject_id);

-- name: CountMaterialsInFolder :one
SELECT count(*) FROM materials WHERE subject_id = sqlc.arg(subject_id) AND folder_id = sqlc.arg(folder_id);

-- A never-stored material (version 0) is inserted with version 1.
-- name: InsertMaterial :exec
INSERT INTO materials (id, sha256, size_bytes, type, original_name, subject_id, folder_id, status, text_status, version,
                       created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(sha256), sqlc.arg(size_bytes), sqlc.arg(type), sqlc.arg(original_name),
        sqlc.arg(subject_id), sqlc.arg(folder_id), sqlc.arg(status), sqlc.arg(text_status), 1, sqlc.arg(created_at),
        sqlc.arg(updated_at));

-- The content (sha256, size, type, name) never changes; the text status changes only through UpdateMaterialText.
-- name: UpdateMaterial :execrows
UPDATE materials
SET subject_id = sqlc.arg(subject_id),
    folder_id  = sqlc.arg(folder_id),
    status     = sqlc.arg(status),
    version    = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- The text status is a fact about the content, not a change: checked against the version but keeps it (TASK-122).
-- name: UpdateMaterialText :execrows
UPDATE materials
SET text_status = sqlc.arg(text_status)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- name: ListMaterialLessons :many
SELECT lesson_id FROM material_lessons WHERE material_id = sqlc.arg(material_id) ORDER BY position;

-- name: DeleteMaterialLessons :exec
DELETE FROM material_lessons WHERE material_id = sqlc.arg(material_id);

-- name: InsertMaterialLesson :exec
INSERT INTO material_lessons (material_id, lesson_id, position)
VALUES (sqlc.arg(material_id), sqlc.arg(lesson_id), sqlc.arg(position));

-- The free subject strings of the live schedule, with how many lessons name each.
-- name: ListScheduleSubjectCounts :many
SELECT subject, CAST(count(*) AS INTEGER) AS lessons
FROM schedule_entries
WHERE deleted_at IS NULL AND subject <> ''
GROUP BY subject;

-- The free subject strings the recordings name (the user's correction, else the lesson snapshot), with how many
-- recordings name each; a recording marked for deletion is left out.
-- name: ListRecordingSubjectCounts :many
SELECT CAST(COALESCE(user_subject, lesson_subject) AS TEXT) AS subject, CAST(count(*) AS INTEGER) AS recordings
FROM recordings
WHERE deleting_at IS NULL AND COALESCE(user_subject, lesson_subject, '') <> ''
GROUP BY COALESCE(user_subject, lesson_subject);
