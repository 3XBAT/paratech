-- Lessons, the local schedule (PRD section 11) with the optimistic-concurrency protocol of ADR-0004(d) and the soft
-- delete of ADR-0004(e): removing a lesson sets deleted_at, only PurgeDeletedLesson deletes a row.

-- name: GetLesson :one
SELECT * FROM schedule_entries WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- A never-stored lesson (version 0) is inserted with version 1.
-- name: InsertLesson :exec
INSERT INTO schedule_entries (id, title, starts_at, ends_at, timezone, subject, kind, teacher, call_link, source_uid,
                              start_notified_at, content_revision, calendar_synced_revision, calendar_synced_at,
                              deleted_at, version, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(title), sqlc.arg(starts_at), sqlc.arg(ends_at), sqlc.arg(timezone),
        sqlc.arg(subject), sqlc.arg(kind), sqlc.arg(teacher), sqlc.arg(call_link), sqlc.arg(source_uid), sqlc.arg(start_notified_at),
        sqlc.arg(content_revision), sqlc.arg(calendar_synced_revision), sqlc.arg(calendar_synced_at), sqlc.arg(deleted_at), 1,
        sqlc.arg(created_at), sqlc.arg(updated_at));

-- Compare-and-swap on the loaded version. The id and the creation time never change.
-- name: UpdateLesson :execrows
UPDATE schedule_entries
SET title                    = sqlc.arg(title),
    starts_at                = sqlc.arg(starts_at),
    ends_at                  = sqlc.arg(ends_at),
    timezone                 = sqlc.arg(timezone),
    subject                  = sqlc.arg(subject),
    kind                     = sqlc.arg(kind),
    teacher                  = sqlc.arg(teacher),
    call_link                = sqlc.arg(call_link),
    source_uid               = sqlc.arg(source_uid),
    start_notified_at        = sqlc.arg(start_notified_at),
    content_revision         = sqlc.arg(content_revision),
    calendar_synced_revision = sqlc.arg(calendar_synced_revision),
    calendar_synced_at       = sqlc.arg(calendar_synced_at),
    deleted_at               = sqlc.arg(deleted_at),
    version                  = version + 1,
    updated_at               = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version);

-- name: ListLiveLessons :many
SELECT * FROM schedule_entries WHERE deleted_at IS NULL ORDER BY starts_at, id;

-- name: ListLessonsStartingBetween :many
SELECT * FROM schedule_entries
WHERE deleted_at IS NULL AND starts_at >= sqlc.arg(from_ms) AND starts_at < sqlc.arg(to_ms)
ORDER BY starts_at, id;

-- The rule of lesson.CalendarPending, stated on the columns (and matching idx_schedule_entries_calendar_pending).
-- name: ListLessonsPendingCalendarSync :many
SELECT * FROM schedule_entries
WHERE deleted_at IS NOT NULL OR calendar_synced_revision IS NULL OR calendar_synced_revision < content_revision
ORDER BY starts_at, id
LIMIT sqlc.arg(max_rows);

-- Runs before PurgeDeletedLesson: the FK cascade drops material_lessons rows silently, so the materials linked to
-- a soft-deleted lesson get a new version (Undo compares material versions).
-- name: BumpMaterialsLinkedToDeletedLesson :exec
UPDATE materials
SET version = version + 1
WHERE id IN (SELECT ml.material_id
             FROM material_lessons ml
                      JOIN schedule_entries se ON se.id = ml.lesson_id
             WHERE ml.lesson_id = sqlc.arg(id) AND se.deleted_at IS NOT NULL);

-- name: PurgeDeletedLesson :execrows
DELETE FROM schedule_entries WHERE id = sqlc.arg(id) AND deleted_at IS NOT NULL;

-- Tells "already purged" from "still live" after PurgeDeletedLesson changed no row.
-- name: LessonExists :one
SELECT EXISTS (SELECT 1 FROM schedule_entries WHERE id = sqlc.arg(id));
