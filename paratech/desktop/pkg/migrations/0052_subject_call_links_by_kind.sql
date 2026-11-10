-- A call link per lesson kind on a subject (TASK-431, ADR-0032 addendum 2026-11-10). subjects.call_link stays the
-- general link; nothing is copied into the new columns.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- subjects: the links of lectures, seminars (and practice) and labs, in the canonical form of subject.CallLink; ''
-- none. Untrusted input, re-checked on read as call_link.
ALTER TABLE subjects ADD COLUMN call_link_lecture TEXT NOT NULL DEFAULT '' CHECK (length(call_link_lecture) <= 2048);
ALTER TABLE subjects ADD COLUMN call_link_seminar TEXT NOT NULL DEFAULT '' CHECK (length(call_link_seminar) <= 2048);
ALTER TABLE subjects ADD COLUMN call_link_lab TEXT NOT NULL DEFAULT '' CHECK (length(call_link_lab) <= 2048);

-- +goose Down
ALTER TABLE subjects DROP COLUMN call_link_lab;
ALTER TABLE subjects DROP COLUMN call_link_seminar;
ALTER TABLE subjects DROP COLUMN call_link_lecture;
