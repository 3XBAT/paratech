-- Subject, kind and teacher of a lesson (TASK-089).
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- schedule_entries: the course of the lesson ('' unknown), the type of the class (values = lesson.Kinds) and the
-- teacher ('' none), at most lesson.MaxSubjectRunes and MaxTeacherRunes characters; part of the content the calendar
-- shows. Lessons stored before stay unknown until the next import of their file derives them.
ALTER TABLE schedule_entries ADD COLUMN subject TEXT NOT NULL DEFAULT '' CHECK (length(subject) <= 200);
ALTER TABLE schedule_entries ADD COLUMN kind TEXT NOT NULL DEFAULT 'unknown' CHECK (kind IN ('unknown', 'lecture', 'seminar', 'practice', 'lab'));
ALTER TABLE schedule_entries ADD COLUMN teacher TEXT NOT NULL DEFAULT '' CHECK (length(teacher) <= 200);

-- +goose Down
ALTER TABLE schedule_entries DROP COLUMN teacher;
ALTER TABLE schedule_entries DROP COLUMN kind;
ALTER TABLE schedule_entries DROP COLUMN subject;
