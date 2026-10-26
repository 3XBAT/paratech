-- Answer keys v2 (TASK-142, ADR-0026): the key of an answer also covers the reuse scope of the request, so the
-- CHECK of answers.request_key accepts 'ans:v2:' next to the 'ans:v1:' keys already stored. SQLite cannot alter a
-- CHECK: the table is rebuilt with its 0027 definition and the new CHECK, keeping every row. The old table is renamed
-- first, so the new one keeps the plain name in its DDL; nothing references answers.
--
-- docs/contracts/schema.sql shows the result: the answers table with the new CHECK (TestMigrationMatchesContractSchema).

-- +goose Up
ALTER TABLE answers RENAME TO answers_v1;
CREATE TABLE answers (
    id          TEXT    NOT NULL PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    request_key TEXT    NOT NULL UNIQUE CHECK (length(request_key) = 71 AND request_key GLOB 'ans:v[12]:*'),
    subject_id  TEXT    CHECK (subject_id IS NULL OR length(subject_id) BETWEEN 1 AND 128),
    question    TEXT    NOT NULL CHECK (length(question) BETWEEN 1 AND 500),
    fragments   INTEGER NOT NULL CHECK (fragments BETWEEN 1 AND 40),
    claims      TEXT    NOT NULL CHECK (json_valid(claims)),
    missing     TEXT    NOT NULL CHECK (json_valid(missing)),
    sources     TEXT    NOT NULL CHECK (json_valid(sources)),
    created_at  INTEGER NOT NULL
);
INSERT INTO answers (id, request_key, subject_id, question, fragments, claims, missing, sources, created_at)
    SELECT id, request_key, subject_id, question, fragments, claims, missing, sources, created_at FROM answers_v1;
DROP TABLE answers_v1;

-- +goose Down
-- The 0027 table again; answers under v2 keys cannot be stored there and are dropped.
ALTER TABLE answers RENAME TO answers_v2;
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
INSERT INTO answers (id, request_key, subject_id, question, fragments, claims, missing, sources, created_at)
    SELECT id, request_key, subject_id, question, fragments, claims, missing, sources, created_at FROM answers_v2
    WHERE request_key GLOB 'ans:v1:*';
DROP TABLE answers_v2;
