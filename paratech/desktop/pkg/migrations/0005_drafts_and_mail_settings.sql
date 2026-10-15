-- Homework draft history and the mail and Hub settings (TASK-057).
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- ---------------------------------------------------------------------------------------------------------------
-- homework_drafts: the draft history of one assignment (PRD section 10; AC-13). Aggregate coursework/homeworkdraft;
-- this row is the root and carries its version, the versions of the draft are homework_draft_revisions.
-- Deletion: an assignment with a draft cannot be deleted (RESTRICT, the repository reports assignment.ErrInUse), so
-- reprocessing never drops what the student wrote; deleting a draft row removes its versions (CASCADE).
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE homework_drafts (
    assignment_id TEXT    PRIMARY KEY REFERENCES assignments (id) ON DELETE RESTRICT,
    version       INTEGER NOT NULL DEFAULT 0 -- optimistic concurrency (ADR-0004)
);

-- homework_draft_revisions: one version of a draft. Versions are append-only: the repository inserts only those it has
-- not stored, in the compare-and-swap transaction of the draft, and never updates one. Limits are those of
-- homeworkdraft (title 200, body 16000 characters, 100 versions). job_id is the homework_draft job of a generated
-- version, unique so that one job adds at most one version; no foreign key, so purging finished jobs keeps the history.
CREATE TABLE homework_draft_revisions (
    assignment_id  TEXT    NOT NULL REFERENCES homework_drafts (assignment_id) ON DELETE CASCADE,
    number         INTEGER NOT NULL CHECK (number BETWEEN 1 AND 100),
    origin         TEXT    NOT NULL CHECK (origin IN ('generated', 'edited')),
    title          TEXT    NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    body           TEXT    NOT NULL CHECK (length(body) BETWEEN 1 AND 16000),
    instruction    TEXT    NOT NULL DEFAULT '' CHECK (length(instruction) <= 4096), -- the student's instructions; not a secret
    prompt_version INTEGER CHECK (prompt_version IS NULL OR prompt_version >= 1),
    job_id         TEXT    UNIQUE,
    created_at     INTEGER NOT NULL,
    PRIMARY KEY (assignment_id, number),
    CHECK ((origin = 'generated' AND job_id IS NOT NULL AND prompt_version IS NOT NULL) OR
           (origin = 'edited' AND job_id IS NULL AND prompt_version IS NULL AND instruction = ''))
);

-- settings: the non-secret mail and Hub setup (PRD sections 10 and 12). Empty means not set. The SMTP password and the
-- Hub API key are in Windows Credential Manager, never here (hard rule 4).
ALTER TABLE settings ADD COLUMN homework_recipient TEXT NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN smtp_host TEXT NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN smtp_port INTEGER NOT NULL DEFAULT 0 CHECK (smtp_port BETWEEN 0 AND 65535); -- 0: the default of the mode
ALTER TABLE settings ADD COLUMN smtp_security TEXT NOT NULL DEFAULT 'starttls' CHECK (smtp_security IN ('starttls', 'tls'));
ALTER TABLE settings ADD COLUMN smtp_username TEXT NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN hub_url TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE settings DROP COLUMN hub_url;
ALTER TABLE settings DROP COLUMN smtp_username;
ALTER TABLE settings DROP COLUMN smtp_security;
ALTER TABLE settings DROP COLUMN smtp_port;
ALTER TABLE settings DROP COLUMN smtp_host;
ALTER TABLE settings DROP COLUMN homework_recipient;
DROP TABLE homework_draft_revisions;
DROP TABLE homework_drafts;
