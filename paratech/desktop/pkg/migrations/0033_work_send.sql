-- The send of a turnkey work to the teacher (TASK-180, ADR-0025 addendum): a proposal of the core's own kind
-- send_work_email names the work whose library file it attaches, as .docx or .pptx. SQLite cannot alter a CHECK:
-- proposals is rebuilt with every row (no table references it). The old table is renamed first, so the new one keeps
-- its plain name in its DDL; the columns keep their order, attachment_format last as 0017 added it, then work_id.
--
-- docs/contracts/schema.sql shows the result: proposals as rebuilt here (TestMigrationMatchesContractSchema).

-- +goose Up
ALTER TABLE proposals RENAME TO proposals_v1;
CREATE TABLE proposals (
    id                TEXT    PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    kind              TEXT    NOT NULL CHECK (kind IN ('send_homework_email', 'export_homework', 'create_deadline_event', 'send_work_email')),
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
    attachment_format TEXT    NOT NULL DEFAULT '' CHECK (attachment_format IN ('', 'txt', 'docx', 'pptx')),
    work_id           TEXT    NOT NULL DEFAULT '' CHECK (length(work_id) <= 128), -- the work whose file is sent (send_work_email)
    CHECK ((due_at IS NOT NULL) = (kind = 'create_deadline_event')),
    CHECK ((reason <> '') = (status IN ('REJECTED', 'STALE'))),
    CHECK ((confirmed_at IS NOT NULL) = (status IN ('CONFIRMED', 'EXECUTED', 'STALE'))),
    CHECK ((executed_at IS NOT NULL) = (status = 'EXECUTED')),
    CHECK ((work_id <> '') = (kind = 'send_work_email'))
);
INSERT INTO proposals (id, kind, status, assignment_id, title, body, due_at, reason, recipient, attachment_sha256,
                       attachment_size, assignment_title, confirmed_at, executed_at, version, created_at, updated_at,
                       attachment_format)
    SELECT id, kind, status, assignment_id, title, body, due_at, reason, recipient, attachment_sha256, attachment_size,
           assignment_title, confirmed_at, executed_at, version, created_at, updated_at, attachment_format
    FROM proposals_v1;
DROP TABLE proposals_v1;

-- +goose Down
-- The 0017 table again; sends of a work cannot be stored there and are dropped.
ALTER TABLE proposals RENAME TO proposals_v2;
CREATE TABLE proposals (
    id                TEXT    PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    kind              TEXT    NOT NULL CHECK (kind IN ('send_homework_email', 'export_homework', 'create_deadline_event')),
    status            TEXT    NOT NULL CHECK (status IN (
                          'PROPOSED', 'VALIDATED', 'REJECTED', 'CONFIRMED', 'DECLINED', 'EXECUTED', 'STALE')),
    assignment_id     TEXT    NOT NULL CHECK (length(assignment_id) > 0),
    title             TEXT    NOT NULL DEFAULT '',
    body              TEXT    NOT NULL DEFAULT '',
    due_at            INTEGER,
    reason            TEXT    NOT NULL DEFAULT '',
    recipient         TEXT    NOT NULL DEFAULT '',
    attachment_sha256 TEXT    NOT NULL DEFAULT '' CHECK (attachment_sha256 = '' OR length(attachment_sha256) = 64),
    attachment_size   INTEGER NOT NULL DEFAULT 0 CHECK (attachment_size >= 0),
    assignment_title  TEXT    NOT NULL DEFAULT '',
    confirmed_at      INTEGER,
    executed_at       INTEGER,
    version           INTEGER NOT NULL DEFAULT 0,
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL,
    attachment_format TEXT NOT NULL DEFAULT '' CHECK (attachment_format IN ('', 'txt', 'docx')),
    CHECK ((due_at IS NOT NULL) = (kind = 'create_deadline_event')),
    CHECK ((reason <> '') = (status IN ('REJECTED', 'STALE'))),
    CHECK ((confirmed_at IS NOT NULL) = (status IN ('CONFIRMED', 'EXECUTED', 'STALE'))),
    CHECK ((executed_at IS NOT NULL) = (status = 'EXECUTED'))
);
INSERT INTO proposals (id, kind, status, assignment_id, title, body, due_at, reason, recipient, attachment_sha256,
                       attachment_size, assignment_title, confirmed_at, executed_at, version, created_at, updated_at,
                       attachment_format)
    SELECT id, kind, status, assignment_id, title, body, due_at, reason, recipient, attachment_sha256, attachment_size,
           assignment_title, confirmed_at, executed_at, version, created_at, updated_at, attachment_format
    FROM proposals_v2 WHERE kind <> 'send_work_email';
DROP TABLE proposals_v2;
