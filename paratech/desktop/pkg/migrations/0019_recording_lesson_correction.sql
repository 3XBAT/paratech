-- The user's correction of the subject and kind of a recording (TASK-091, the knowledge base): the snapshot of the
-- lesson stays write-once, the correction wins over it.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- recordings (migration 0019): the subject and the kind of the lesson the user set (values.LessonCorrection), one plain
-- line and a kind code. NULL: not corrected, the lesson snapshot or the schedule names it.
ALTER TABLE recordings ADD COLUMN user_subject TEXT CHECK (user_subject IS NULL OR length(user_subject) BETWEEN 1 AND 256);
ALTER TABLE recordings ADD COLUMN user_kind TEXT CHECK (user_kind IS NULL OR user_kind IN ('lecture', 'seminar', 'practice', 'lab'));

-- +goose Down
ALTER TABLE recordings DROP COLUMN user_kind;
ALTER TABLE recordings DROP COLUMN user_subject;
