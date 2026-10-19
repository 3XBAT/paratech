-- Lesson snapshot of a recording (TASK-088): what the recording keeps of its lesson at the start, so a later import
-- that deletes or changes the lesson does not take the title away.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- recordings: the snapshot of the lesson at the start (values.LessonSnapshot): imported text, plain text only; the times
-- are Unix milliseconds of the scheduled start and end. NULL: unknown, an ad-hoc recording or one stored before.
ALTER TABLE recordings ADD COLUMN lesson_title TEXT;
ALTER TABLE recordings ADD COLUMN lesson_subject TEXT;
ALTER TABLE recordings ADD COLUMN lesson_kind TEXT;
ALTER TABLE recordings ADD COLUMN lesson_teacher TEXT;
ALTER TABLE recordings ADD COLUMN lesson_start INTEGER;
ALTER TABLE recordings ADD COLUMN lesson_end INTEGER;

-- +goose Down
ALTER TABLE recordings DROP COLUMN lesson_end;
ALTER TABLE recordings DROP COLUMN lesson_start;
ALTER TABLE recordings DROP COLUMN lesson_teacher;
ALTER TABLE recordings DROP COLUMN lesson_kind;
ALTER TABLE recordings DROP COLUMN lesson_subject;
ALTER TABLE recordings DROP COLUMN lesson_title;
