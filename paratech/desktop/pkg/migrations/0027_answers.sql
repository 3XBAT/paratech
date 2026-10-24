-- Answers with citations (TASK-118, ADR-0022): the answer of the quarantined reader ANSWER to a student's question
-- from their lessons, with the citations that passed the desktop's check. Written once, never saved again.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- answers (migration 0027): aggregate coursework/answer, inserted once (no version: it never changes). request_key is
-- the idempotency key of the reader request, one answer per key, so the same question about the same fragments is
-- answered from here. subject_id is the subject asked about (NULL: the whole base), without a foreign key. claims,
-- missing and sources are the checked answer as JSON: untrusted model text and quotes of lessons, read back through
-- answer.Restore; a source is the place a citation opens (recording, section, index, start).
CREATE TABLE answers (
    id          TEXT    NOT NULL PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    request_key TEXT    NOT NULL UNIQUE CHECK (length(request_key) = 71 AND request_key GLOB 'ans:v1:*'),
    subject_id  TEXT    CHECK (subject_id IS NULL OR length(subject_id) BETWEEN 1 AND 128),
    question    TEXT    NOT NULL CHECK (length(question) BETWEEN 1 AND 500),
    fragments   INTEGER NOT NULL CHECK (fragments BETWEEN 1 AND 40),
    claims      TEXT    NOT NULL CHECK (json_valid(claims)),
    missing     TEXT    NOT NULL CHECK (json_valid(missing)),
    sources     TEXT    NOT NULL CHECK (json_valid(sources)),
    created_at  INTEGER NOT NULL
);

-- +goose Down
DROP TABLE answers;
