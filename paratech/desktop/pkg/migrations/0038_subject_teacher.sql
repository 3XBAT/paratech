-- The teacher of a subject (TASK-235, ADR-0030): a name and an email the student types once on the subject page.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- subjects: the teacher (subject.Teacher); both '' while none is set, never one without the other. Personal data
-- typed by the student, not a secret; never sent to the agent or the Hub (ADR-0022).
ALTER TABLE subjects ADD COLUMN teacher_name TEXT NOT NULL DEFAULT '' CHECK (length(teacher_name) <= 200);
ALTER TABLE subjects ADD COLUMN teacher_email TEXT NOT NULL DEFAULT '' CHECK (length(teacher_email) <= 254 AND (teacher_email = '') = (teacher_name = ''));

-- +goose Down
ALTER TABLE subjects DROP COLUMN teacher_email;
ALTER TABLE subjects DROP COLUMN teacher_name;
