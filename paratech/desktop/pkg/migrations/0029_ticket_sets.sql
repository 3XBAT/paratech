-- Exam ticket sets (TASK-133): a list of tickets parsed on the desktop, the student's confirmation of the paid run,
-- the answer with citations and the coverage of each ticket, and the .docx filed into the subject's «Билеты». Local
-- data is the system of record; the titles come from an untrusted list, read back through the aggregate's checks.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- ticket_sets (migration 0029): aggregate coursework/ticketset, saved with the optimistic-concurrency protocol of
-- ADR-0004(d). subject_id is the subject whose lessons answer the tickets, material_id the material the list was read
-- from (NULL: pasted); neither has a foreign key, so a set outlives a removed subject or file. content is the tickets
-- with their results (answer id and coverage only: the quotes and recordings stay in answers), the acceptance of a
-- cost above the cap and the file, as canonical JSON (ticketset.Content.Encode) read back with ticketset.ParseContent.
CREATE TABLE ticket_sets (
    id          TEXT    NOT NULL PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    subject_id  TEXT    NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    material_id TEXT    CHECK (material_id IS NULL OR length(material_id) BETWEEN 1 AND 128),
    status      TEXT    NOT NULL CHECK (status IN ('proposed', 'confirmed', 'answering', 'done', 'failed', 'cancelled')),
    content     TEXT    NOT NULL CHECK (json_valid(content)),
    version     INTEGER NOT NULL DEFAULT 0,                          -- optimistic concurrency (ADR-0004)
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
CREATE INDEX idx_ticket_sets_subject ON ticket_sets (subject_id);

-- +goose Down
DROP INDEX idx_ticket_sets_subject;
DROP TABLE ticket_sets;
