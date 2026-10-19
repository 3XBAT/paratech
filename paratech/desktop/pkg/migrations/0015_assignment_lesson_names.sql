-- Subject and teacher of an assignment that do not come from the schedule (TASK-092).
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- assignments: the subject and teacher the homework search found in the transcript (hint_*, model output, shown
-- marked «по записи») and the ones the user entered (user_*); '' none. One plain line of at most values.MaxNameRunes
-- characters each. The schedule's subject and teacher are read from the recording's lesson snapshot, never copied here.
ALTER TABLE assignments ADD COLUMN hint_subject TEXT NOT NULL DEFAULT '' CHECK (length(hint_subject) <= 200);
ALTER TABLE assignments ADD COLUMN hint_teacher TEXT NOT NULL DEFAULT '' CHECK (length(hint_teacher) <= 200);
ALTER TABLE assignments ADD COLUMN user_subject TEXT NOT NULL DEFAULT '' CHECK (length(user_subject) <= 200);
ALTER TABLE assignments ADD COLUMN user_teacher TEXT NOT NULL DEFAULT '' CHECK (length(user_teacher) <= 200);

-- +goose Down
ALTER TABLE assignments DROP COLUMN user_teacher;
ALTER TABLE assignments DROP COLUMN user_subject;
ALTER TABLE assignments DROP COLUMN hint_teacher;
ALTER TABLE assignments DROP COLUMN hint_subject;
