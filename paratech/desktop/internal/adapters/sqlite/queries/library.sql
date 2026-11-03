-- The library (TASK-121, ADR-0024): subjects with their aliases and folders, materials with their lesson links. Both
-- aggregates follow the optimistic-concurrency protocol of ADR-0004(d); child rows carry no version of their own.

-- A removed subject (removed_at set, TASK-288) and everything filed under it are hidden from every reader; only
-- GetSubjectAnyStatus and ListRemovedSubjects see it. Its rows are never rewritten by the removal.
-- name: ListSubjects :many
SELECT * FROM subjects WHERE removed_at IS NULL ORDER BY name_key, id;

-- name: ListRemovedSubjects :many
SELECT * FROM subjects WHERE removed_at IS NOT NULL ORDER BY removed_at, id;

-- name: GetSubject :one
SELECT * FROM subjects WHERE id = sqlc.arg(id) AND removed_at IS NULL;

-- name: GetSubjectAnyStatus :one
SELECT * FROM subjects WHERE id = sqlc.arg(id);

-- A never-stored subject (version 0) is inserted with version 1. subjects.teacher_* are legacy since migration 0044
-- (always '', never read): the teachers are rows of subject_teachers.
-- name: InsertSubject :exec
INSERT INTO subjects (id, name, name_key, semester, call_link, version, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(name_key), sqlc.arg(semester), sqlc.arg(call_link), 1,
        sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: UpdateSubject :execrows
UPDATE subjects
SET name       = sqlc.arg(name),
    name_key   = sqlc.arg(name_key),
    semester   = sqlc.arg(semester),
    call_link  = sqlc.arg(call_link),
    removed_at = sqlc.arg(removed_at),
    version    = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- Removes the subject with its aliases, folders and teachers (CASCADE); a material still filed under it fails the
-- RESTRICT.
-- name: DeleteSubject :execrows
DELETE FROM subjects WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- The purge of a removed subject past its restore window (TASK-308, ADR-0027). Every purge delete re-checks that the
-- subject is still removed at or before the cutoff (ms), so a restore that won the race keeps everything.
-- name: PurgeRemovedSubject :execrows
DELETE FROM subjects
WHERE id = sqlc.arg(id) AND removed_at IS NOT NULL AND removed_at <= sqlc.arg(cutoff);

-- name: SubjectStillDue :one
SELECT CAST(EXISTS (SELECT 1 FROM subjects
                    WHERE id = sqlc.arg(id) AND removed_at IS NOT NULL AND removed_at <= sqlc.arg(cutoff)) AS INTEGER)
       AS due;

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

-- The teachers of a subject (TASK-285): rewritten with the subject on every save, under its version check.
-- name: ListAllSubjectTeachers :many
SELECT * FROM subject_teachers ORDER BY subject_id, role;

-- name: ListSubjectTeachers :many
SELECT * FROM subject_teachers WHERE subject_id = sqlc.arg(subject_id) ORDER BY role;

-- name: DeleteSubjectTeachers :exec
DELETE FROM subject_teachers WHERE subject_id = sqlc.arg(subject_id);

-- name: InsertSubjectTeacher :exec
INSERT INTO subject_teachers (subject_id, role, name, email)
VALUES (sqlc.arg(subject_id), sqlc.arg(role), sqlc.arg(name), sqlc.arg(email));

-- A material is visible while it is in the inbox or its subject is not removed; one filed under a removed subject
-- stays visible while it is a part of an active note of a subject that is not removed (ADR-0027). (Comments here stay
-- ASCII: sqlc v1.31.1 counts the offsets of its rewrites in bytes.)
-- name: GetMaterial :one
SELECT * FROM materials
WHERE materials.id = sqlc.arg(id)
  AND (subject_id IS NULL
    OR EXISTS (SELECT 1 FROM subjects s WHERE s.id = materials.subject_id AND s.removed_at IS NULL)
    OR EXISTS (SELECT 1
               FROM note_parts p
               JOIN notes n ON n.id = p.note_id
               JOIN subjects ns ON ns.id = n.subject_id
               WHERE p.material_id = materials.id AND n.status = 'active' AND ns.removed_at IS NULL));

-- In any visibility: the same bytes are one material (import dedupe).
-- name: GetMaterialBySHA256 :one
SELECT * FROM materials WHERE sha256 = sqlc.arg(sha256);

-- name: ListMaterialsOfSubject :many
SELECT * FROM materials
WHERE subject_id = sqlc.arg(subject_id)
  AND EXISTS (SELECT 1 FROM subjects s WHERE s.id = materials.subject_id AND s.removed_at IS NULL)
ORDER BY created_at DESC, id;

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

-- The files filed under a subject in any status and visibility, for its purge (TASK-308).
-- name: ListMaterialsFiledUnder :many
SELECT * FROM materials WHERE subject_id = sqlc.arg(subject_id) ORDER BY created_at, id;

-- The one check of a file that must survive its subject's purge: a note of another subject holds it, or a work was
-- written from it (works.material_id cascades, ADR-0027 keeps works).
-- name: MaterialKeptElsewhere :one
SELECT CAST(EXISTS (SELECT 1
                    FROM note_parts p
                    JOIN notes n ON n.id = p.note_id
                    WHERE p.material_id = sqlc.arg(id) AND n.subject_id <> sqlc.arg(subject_id))
         OR EXISTS (SELECT 1 FROM works w WHERE w.material_id = sqlc.arg(id)) AS INTEGER) AS kept;

-- Whether the stored bytes of a sha256 are used by anything but one material: another material, or a send of a work
-- not yet carried out that pinned them (its copy is made from them). One query on purpose: later stores of those
-- bytes (note revisions, TASK-290/291) are added here.
-- name: MaterialDigestInUse :one
SELECT CAST(EXISTS (SELECT 1 FROM materials m WHERE m.sha256 = sqlc.arg(sha256) AND m.id <> sqlc.arg(except_id))
         OR EXISTS (SELECT 1
                    FROM proposals p
                    WHERE p.kind = 'send_work_email' AND p.attachment_sha256 = sqlc.arg(sha256)
                      AND p.status IN ('PROPOSED', 'VALIDATED', 'CONFIRMED')) AS INTEGER) AS used;

-- A material held by any note or the source of any work is never deleted (works would cascade); its lesson links and
-- inspection cascade, and the summaries and assignments made from it keep their rows (SET NULL).
-- name: PurgeMaterialOfRemovedSubject :execrows
DELETE FROM materials
WHERE materials.id = sqlc.arg(id)
  AND materials.subject_id = sqlc.arg(subject_id)
  AND EXISTS (SELECT 1 FROM subjects s
              WHERE s.id = materials.subject_id AND s.removed_at IS NOT NULL AND s.removed_at <= sqlc.arg(cutoff))
  AND NOT EXISTS (SELECT 1 FROM note_parts p WHERE p.material_id = materials.id)
  AND NOT EXISTS (SELECT 1 FROM works w WHERE w.material_id = materials.id);

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

-- The free subject strings of the live schedule and of the recordings not marked for deletion (the user's correction,
-- else the lesson snapshot) in one read: per string the lessons by kind (seminar and practice together) and the
-- recordings, sorted by string.
-- name: ListScheduleSubjectStrings :many
SELECT CAST(subject AS TEXT) AS subject,
       CAST(sum(kind = 'lecture') AS INTEGER) AS lectures,
       CAST(sum(kind IN ('seminar', 'practice')) AS INTEGER) AS seminars,
       CAST(sum(kind = 'lab') AS INTEGER) AS labs,
       CAST(sum(kind = 'unknown') AS INTEGER) AS unknown,
       CAST(sum(recording) AS INTEGER) AS recordings
FROM (
    SELECT subject AS subject, kind AS kind, 0 AS recording
    FROM schedule_entries
    WHERE deleted_at IS NULL AND subject <> ''
    UNION ALL
    SELECT COALESCE(user_subject, lesson_subject) AS subject, '' AS kind, 1 AS recording
    FROM recordings
    WHERE deleting_at IS NULL AND COALESCE(user_subject, lesson_subject, '') <> ''
) AS strings
GROUP BY subject
ORDER BY subject;
