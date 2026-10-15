-- Action proposals of the LLM (TASK-060, the proposal pipeline of TASK-040).
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema). The migration is self-contained: it only creates its own table.

-- +goose Up
-- ---------------------------------------------------------------------------------------------------------------
-- proposals: the actions the LLM proposed, with their validation and confirmation state (PRD section 13.3; AC-14,
-- AC-18). Aggregate coursework/proposal; the columns are proposal.State. Status values = proposal.Statuses, kinds =
-- proposal.Kinds. title, body and due_at are model text, stored as data only (hard rule 3); recipient, the attachment
-- digest and assignment_title are frozen by the core at validation, never taken from the model.
-- assignment_id has no foreign key on purpose: a proposal naming an assignment that does not exist is stored REJECTED
-- (unknown_assignment), which a foreign key would refuse; an assignment deleted later makes a confirmed proposal STALE
-- (Recheck) and keeps an EXECUTED one as the record of what was done, while a RESTRICT would block the deletion by
-- reprocessing for every declined or rejected proposal.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE proposals (
    id                TEXT    PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    kind              TEXT    NOT NULL CHECK (kind IN ('send_homework_email', 'export_homework', 'create_deadline_event')),
    status            TEXT    NOT NULL CHECK (status IN (
                          'PROPOSED', 'VALIDATED', 'REJECTED', 'CONFIRMED', 'DECLINED', 'EXECUTED', 'STALE')),
    assignment_id     TEXT    NOT NULL CHECK (length(assignment_id) > 0),
    title             TEXT    NOT NULL DEFAULT '',           -- model text: the event title (create_deadline_event)
    body              TEXT    NOT NULL DEFAULT '',           -- model text: the homework (export_homework)
    due_at            INTEGER,                               -- model value: the deadline (create_deadline_event)
    reason            TEXT    NOT NULL DEFAULT '',           -- why it was rejected or went stale (proposal.RejectReason)
    recipient         TEXT    NOT NULL DEFAULT '',           -- the trusted address, an email address, not a secret
    attachment_sha256 TEXT    NOT NULL DEFAULT '' CHECK (attachment_sha256 = '' OR length(attachment_sha256) = 64),
    attachment_size   INTEGER NOT NULL DEFAULT 0 CHECK (attachment_size >= 0),
    assignment_title  TEXT    NOT NULL DEFAULT '',           -- the trusted heading of the export file (export_homework)
    confirmed_at      INTEGER,                               -- the explicit user confirmation (AC-14)
    executed_at       INTEGER,
    version           INTEGER NOT NULL DEFAULT 0,            -- optimistic concurrency (ADR-0004)
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL,
    CHECK ((due_at IS NOT NULL) = (kind = 'create_deadline_event')),
    CHECK ((reason <> '') = (status IN ('REJECTED', 'STALE'))),
    CHECK ((confirmed_at IS NOT NULL) = (status IN ('CONFIRMED', 'EXECUTED', 'STALE'))),
    CHECK ((executed_at IS NOT NULL) = (status = 'EXECUTED'))
);

-- +goose Down
DROP TABLE proposals;
