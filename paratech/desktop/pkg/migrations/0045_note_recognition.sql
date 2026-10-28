-- «ИИ распознаёт фото конспектов» (TASK-294, ADR-0027 «Recognition of photos and scans»).
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- settings: who recognises the text of photo pages of a note (settings.NoteRecognition): 'off' (the default, also of
-- the existing row) or 'local' (Windows OCR on this computer).
ALTER TABLE settings ADD COLUMN note_recognition TEXT NOT NULL DEFAULT 'off' CHECK (note_recognition IN ('off', 'local'));

-- +goose Down
ALTER TABLE settings DROP COLUMN note_recognition;
