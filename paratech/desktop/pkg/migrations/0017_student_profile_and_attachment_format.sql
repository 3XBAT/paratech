-- The student profile and the attachment format of a homework email (TASK-094).
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- settings: the student profile of the title page of a homework document (settings.Profile): full name, group,
-- university, faculty or department and city; '' not set. One plain line of at most settings.MaxProfileRunes
-- characters each. Local data, not a secret.
ALTER TABLE settings ADD COLUMN student_full_name TEXT NOT NULL DEFAULT '' CHECK (length(student_full_name) <= 200);
ALTER TABLE settings ADD COLUMN student_group TEXT NOT NULL DEFAULT '' CHECK (length(student_group) <= 200);
ALTER TABLE settings ADD COLUMN student_university TEXT NOT NULL DEFAULT '' CHECK (length(student_university) <= 200);
ALTER TABLE settings ADD COLUMN student_faculty TEXT NOT NULL DEFAULT '' CHECK (length(student_faculty) <= 200);
ALTER TABLE settings ADD COLUMN student_city TEXT NOT NULL DEFAULT '' CHECK (length(student_city) <= 200);

-- proposals: the file a send_homework_email attaches (proposal.Format), chosen by the user in the confirmation dialog
-- and pinned with the digest: 'txt' the text export, 'docx' the Word export; '' for the other kinds and for sends stored
-- before, which attach the text export.
ALTER TABLE proposals ADD COLUMN attachment_format TEXT NOT NULL DEFAULT '' CHECK (attachment_format IN ('', 'txt', 'docx'));

-- +goose Down
ALTER TABLE proposals DROP COLUMN attachment_format;
ALTER TABLE settings DROP COLUMN student_city;
ALTER TABLE settings DROP COLUMN student_faculty;
ALTER TABLE settings DROP COLUMN student_university;
ALTER TABLE settings DROP COLUMN student_group;
ALTER TABLE settings DROP COLUMN student_full_name;
