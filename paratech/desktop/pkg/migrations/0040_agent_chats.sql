-- Agent chats (TASK-204, ADR-0028): a thread is a chat, not tied to a subject. agent_threads loses subject_key and
-- gains title, archived_at and last_active_at; agent_turns gains the mentions of a request and dropped_by. SQLite
-- cannot drop a UNIQUE column, so agent_threads is rebuilt with every row (same ids). The three tables that reference
-- it are rebuilt with it, so no foreign key action deletes a row meanwhile (renaming agent_threads rewrites their
-- references); agent_actions keeps its rowids, which order the journal of one instant. Every thread becomes a chat
-- titled with the name of its subject (cut to 80 characters), «Без предмета» for NONE or a subject gone since; it was
-- last active when it was last updated, or when created if an old row was updated before it (0022 did not check it,
-- the new CHECK last_active_at >= created_at would abort the migration).
--
-- docs/contracts/schema.sql shows the result: the four agent tables as rebuilt here (TestMigrationMatchesContractSchema).

-- +goose Up
ALTER TABLE agent_actions RENAME TO agent_actions_v1;
ALTER TABLE agent_steps RENAME TO agent_steps_v1;
ALTER TABLE agent_turns RENAME TO agent_turns_v1;
ALTER TABLE agent_threads RENAME TO agent_threads_v1;
DROP INDEX idx_agent_actions_created;
DROP INDEX idx_agent_actions_open;
DROP INDEX idx_agent_actions_thread;
DROP INDEX idx_agent_steps_created;
DROP INDEX idx_agent_steps_turn;
CREATE TABLE agent_threads (
    id             TEXT    PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    title          TEXT    NOT NULL CHECK (length(title) BETWEEN 1 AND 80),
    turns          INTEGER NOT NULL DEFAULT 0 CHECK (turns >= 0),
    history_from   INTEGER NOT NULL DEFAULT 0 CHECK (history_from BETWEEN 0 AND turns),
    seen           TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(seen) AND json_type(seen) = 'array'),
    archived_at    INTEGER CHECK (archived_at IS NULL OR archived_at >= created_at),
    last_active_at INTEGER NOT NULL CHECK (last_active_at >= created_at),
    version        INTEGER NOT NULL DEFAULT 0,                               -- optimistic concurrency (ADR-0004)
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);
CREATE INDEX idx_agent_threads_active ON agent_threads (archived_at, last_active_at);
CREATE TABLE agent_turns (
    thread_id       TEXT    NOT NULL REFERENCES agent_threads (id) ON DELETE CASCADE,
    number          INTEGER NOT NULL CHECK (number >= 1),
    status          TEXT    NOT NULL CHECK (status IN ('PLANNING', 'AWAITING_PLAN', 'AWAITING_ANSWERS', 'EXECUTING', 'DONE', 'STOPPED', 'FAILED', 'CANCELLED')),
    resume_status   TEXT    NOT NULL DEFAULT '' CHECK (resume_status IN ('', 'PLANNING', 'EXECUTING')),
    stop_reason     TEXT    NOT NULL DEFAULT '' CHECK (stop_reason IN ('', 'budget', 'offline', 'user', 'quota', 'failed', 'expired')),
    scope           TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(scope) AND json_type(scope) = 'object'),
    mentions        TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(mentions) AND json_type(mentions) = 'array'),
    steps           INTEGER NOT NULL DEFAULT 0 CHECK (steps >= 0),
    writes          INTEGER NOT NULL DEFAULT 0 CHECK (writes >= 0),
    paid_unplanned  INTEGER NOT NULL DEFAULT 0 CHECK (paid_unplanned >= 0),
    planner_input   INTEGER NOT NULL DEFAULT 0 CHECK (planner_input >= 0),
    question_rounds INTEGER NOT NULL DEFAULT 0 CHECK (question_rounds >= 0),
    step_open       INTEGER NOT NULL DEFAULT 0 CHECK (step_open IN (0, 1)),
    retry           INTEGER NOT NULL DEFAULT 0 CHECK (retry >= 0),
    proposed_plan   TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(proposed_plan) AND json_type(proposed_plan) = 'array'),
    approved_plan   TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(approved_plan) AND json_type(approved_plan) = 'array'),
    used            TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(used) AND json_type(used) = 'array'),
    open_calls      TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(open_calls) AND json_type(open_calls) = 'array'),
    created_ids     TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(created_ids) AND json_type(created_ids) = 'array'),
    dropped_by      INTEGER NOT NULL DEFAULT 0 CHECK (dropped_by = 0 OR dropped_by > number),
    started_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    PRIMARY KEY (thread_id, number)
);
CREATE TABLE agent_steps (
    thread_id  TEXT    NOT NULL REFERENCES agent_threads (id) ON DELETE CASCADE,
    seq        INTEGER NOT NULL CHECK (seq >= 1),
    turn       INTEGER NOT NULL CHECK (turn >= 1),
    entry      TEXT    NOT NULL CHECK (entry IN ('student', 'calls', 'results')),
    body       TEXT    NOT NULL CHECK (json_valid(body) AND json_type(body) = 'object'),
    created_at INTEGER NOT NULL,
    PRIMARY KEY (thread_id, seq)
);
CREATE INDEX idx_agent_steps_turn ON agent_steps (thread_id, turn);
CREATE INDEX idx_agent_steps_created ON agent_steps (created_at);
CREATE TABLE agent_actions (
    id             TEXT    PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    thread_id      TEXT    NOT NULL REFERENCES agent_threads (id),
    turn           INTEGER NOT NULL CHECK (turn >= 1),
    call_id        TEXT    NOT NULL CHECK (length(call_id) BETWEEN 1 AND 64),
    tool           TEXT    NOT NULL CHECK (length(tool) BETWEEN 1 AND 64),
    status         TEXT    NOT NULL CHECK (status IN ('PENDING', 'DONE', 'FAILED', 'UNKNOWN', 'UNDOING', 'UNDONE', 'UNDO_REFUSED', 'PROPOSED')),
    compensation   TEXT    CHECK (compensation IS NULL OR (json_valid(compensation) AND json_type(compensation) = 'object')),
    target_version INTEGER NOT NULL DEFAULT 0 CHECK (target_version >= 0),
    proposal       TEXT    NOT NULL DEFAULT '' CHECK (length(proposal) <= 160),
    version        INTEGER NOT NULL DEFAULT 0,                                 -- optimistic concurrency (ADR-0004)
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL,
    intent         TEXT    CHECK (intent IS NULL OR (json_valid(intent) AND json_type(intent) = 'object')),
    undo_refusal   TEXT    NOT NULL DEFAULT '' CHECK (undo_refusal IN ('', 'changed', 'result_ready', 'cannot_stop')),
    UNIQUE (thread_id, turn, call_id)
);
CREATE INDEX idx_agent_actions_thread ON agent_actions (thread_id, created_at);
CREATE INDEX idx_agent_actions_open ON agent_actions (status) WHERE status IN ('PENDING', 'UNDOING');
CREATE INDEX idx_agent_actions_created ON agent_actions (created_at);
INSERT INTO agent_threads (id, title, turns, history_from, seen, archived_at, last_active_at, version, created_at, updated_at)
    SELECT t.id, COALESCE(NULLIF(rtrim(substr(s.name, 1, 80)), ''), 'Без предмета'), t.turns, t.history_from, t.seen,
           NULL, max(t.updated_at, t.created_at), t.version, t.created_at, t.updated_at
    FROM agent_threads_v1 t LEFT JOIN subjects s ON s.id = t.subject_key AND t.subject_key <> 'NONE';
INSERT INTO agent_turns (thread_id, number, status, resume_status, stop_reason, scope, steps, writes, paid_unplanned,
                         planner_input, question_rounds, step_open, retry, proposed_plan, approved_plan, used,
                         open_calls, created_ids, started_at, updated_at)
    SELECT thread_id, number, status, resume_status, stop_reason, scope, steps, writes, paid_unplanned, planner_input,
           question_rounds, step_open, retry, proposed_plan, approved_plan, used, open_calls, created_ids, started_at,
           updated_at
    FROM agent_turns_v1;
INSERT INTO agent_steps (thread_id, seq, turn, entry, body, created_at)
    SELECT thread_id, seq, turn, entry, body, created_at FROM agent_steps_v1;
INSERT INTO agent_actions (rowid, id, thread_id, turn, call_id, tool, status, compensation, target_version, proposal,
                           version, created_at, updated_at, intent, undo_refusal)
    SELECT rowid, id, thread_id, turn, call_id, tool, status, compensation, target_version, proposal, version,
           created_at, updated_at, intent, undo_refusal
    FROM agent_actions_v1;
DROP TABLE agent_actions_v1;
DROP TABLE agent_steps_v1;
DROP TABLE agent_turns_v1;
DROP TABLE agent_threads_v1;

-- +goose Down
-- The tables of 0022 and 0023 again. A chat gets back the subject key of the scope of its last request ('NONE' without
-- one); only the newest chat per key is kept: the other chats are LOST with their requests, history and journal
-- lines. Titles, archive marks, mentions and dropped marks are lost too (a dropped request is sent again).
ALTER TABLE agent_actions RENAME TO agent_actions_v2;
ALTER TABLE agent_steps RENAME TO agent_steps_v2;
ALTER TABLE agent_turns RENAME TO agent_turns_v2;
ALTER TABLE agent_threads RENAME TO agent_threads_v2;
DROP INDEX idx_agent_actions_created;
DROP INDEX idx_agent_actions_open;
DROP INDEX idx_agent_actions_thread;
DROP INDEX idx_agent_steps_created;
DROP INDEX idx_agent_steps_turn;
DROP INDEX idx_agent_threads_active;
CREATE TABLE agent_threads (
    id           TEXT    PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    subject_key  TEXT    NOT NULL UNIQUE CHECK (length(subject_key) BETWEEN 1 AND 128),
    turns        INTEGER NOT NULL DEFAULT 0 CHECK (turns >= 0),
    history_from INTEGER NOT NULL DEFAULT 0 CHECK (history_from BETWEEN 0 AND turns),
    seen         TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(seen) AND json_type(seen) = 'array'),
    version      INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE TABLE agent_turns (
    thread_id       TEXT    NOT NULL REFERENCES agent_threads (id) ON DELETE CASCADE,
    number          INTEGER NOT NULL CHECK (number >= 1),
    status          TEXT    NOT NULL CHECK (status IN ('PLANNING', 'AWAITING_PLAN', 'AWAITING_ANSWERS', 'EXECUTING', 'DONE', 'STOPPED', 'FAILED', 'CANCELLED')),
    resume_status   TEXT    NOT NULL DEFAULT '' CHECK (resume_status IN ('', 'PLANNING', 'EXECUTING')),
    stop_reason     TEXT    NOT NULL DEFAULT '' CHECK (stop_reason IN ('', 'budget', 'offline', 'user', 'quota', 'failed', 'expired')),
    scope           TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(scope) AND json_type(scope) = 'object'),
    steps           INTEGER NOT NULL DEFAULT 0 CHECK (steps >= 0),
    writes          INTEGER NOT NULL DEFAULT 0 CHECK (writes >= 0),
    paid_unplanned  INTEGER NOT NULL DEFAULT 0 CHECK (paid_unplanned >= 0),
    planner_input   INTEGER NOT NULL DEFAULT 0 CHECK (planner_input >= 0),
    question_rounds INTEGER NOT NULL DEFAULT 0 CHECK (question_rounds >= 0),
    step_open       INTEGER NOT NULL DEFAULT 0 CHECK (step_open IN (0, 1)),
    retry           INTEGER NOT NULL DEFAULT 0 CHECK (retry >= 0),
    proposed_plan   TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(proposed_plan) AND json_type(proposed_plan) = 'array'),
    approved_plan   TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(approved_plan) AND json_type(approved_plan) = 'array'),
    used            TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(used) AND json_type(used) = 'array'),
    open_calls      TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(open_calls) AND json_type(open_calls) = 'array'),
    created_ids     TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(created_ids) AND json_type(created_ids) = 'array'),
    started_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    PRIMARY KEY (thread_id, number)
);
CREATE TABLE agent_steps (
    thread_id  TEXT    NOT NULL REFERENCES agent_threads (id) ON DELETE CASCADE,
    seq        INTEGER NOT NULL CHECK (seq >= 1),
    turn       INTEGER NOT NULL CHECK (turn >= 1),
    entry      TEXT    NOT NULL CHECK (entry IN ('student', 'calls', 'results')),
    body       TEXT    NOT NULL CHECK (json_valid(body) AND json_type(body) = 'object'),
    created_at INTEGER NOT NULL,
    PRIMARY KEY (thread_id, seq)
);
CREATE INDEX idx_agent_steps_turn ON agent_steps (thread_id, turn);
CREATE INDEX idx_agent_steps_created ON agent_steps (created_at);
CREATE TABLE agent_actions (
    id             TEXT    PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    thread_id      TEXT    NOT NULL REFERENCES agent_threads (id),
    turn           INTEGER NOT NULL CHECK (turn >= 1),
    call_id        TEXT    NOT NULL CHECK (length(call_id) BETWEEN 1 AND 64),
    tool           TEXT    NOT NULL CHECK (length(tool) BETWEEN 1 AND 64),
    status         TEXT    NOT NULL CHECK (status IN ('PENDING', 'DONE', 'FAILED', 'UNKNOWN', 'UNDOING', 'UNDONE', 'UNDO_REFUSED', 'PROPOSED')),
    compensation   TEXT    CHECK (compensation IS NULL OR (json_valid(compensation) AND json_type(compensation) = 'object')),
    target_version INTEGER NOT NULL DEFAULT 0 CHECK (target_version >= 0),
    proposal       TEXT    NOT NULL DEFAULT '' CHECK (length(proposal) <= 160),
    version        INTEGER NOT NULL DEFAULT 0,
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL,
    intent         TEXT    CHECK (intent IS NULL OR (json_valid(intent) AND json_type(intent) = 'object')),
    undo_refusal   TEXT    NOT NULL DEFAULT '' CHECK (undo_refusal IN ('', 'changed', 'result_ready', 'cannot_stop')),
    UNIQUE (thread_id, turn, call_id)
);
CREATE INDEX idx_agent_actions_thread ON agent_actions (thread_id, created_at);
CREATE INDEX idx_agent_actions_open ON agent_actions (status) WHERE status IN ('PENDING', 'UNDOING');
CREATE INDEX idx_agent_actions_created ON agent_actions (created_at);
INSERT INTO agent_threads (id, subject_key, turns, history_from, seen, version, created_at, updated_at)
    SELECT id, subject_key, turns, history_from, seen, version, created_at, updated_at
    FROM (SELECT t.*, k.subject_key,
                 row_number() OVER (PARTITION BY k.subject_key ORDER BY t.last_active_at DESC, t.id DESC) AS n
          FROM agent_threads_v2 t
          JOIN (SELECT t2.id, COALESCE(NULLIF(json_extract(u.scope, '$.subject'), ''), 'NONE') AS subject_key
                FROM agent_threads_v2 t2
                LEFT JOIN agent_turns_v2 u ON u.thread_id = t2.id AND u.number = t2.turns) k ON k.id = t.id)
    WHERE n = 1;
INSERT INTO agent_turns (thread_id, number, status, resume_status, stop_reason, scope, steps, writes, paid_unplanned,
                         planner_input, question_rounds, step_open, retry, proposed_plan, approved_plan, used,
                         open_calls, created_ids, started_at, updated_at)
    SELECT thread_id, number, status, resume_status, stop_reason, scope, steps, writes, paid_unplanned, planner_input,
           question_rounds, step_open, retry, proposed_plan, approved_plan, used, open_calls, created_ids, started_at,
           updated_at
    FROM agent_turns_v2 WHERE thread_id IN (SELECT id FROM agent_threads);
INSERT INTO agent_steps (thread_id, seq, turn, entry, body, created_at)
    SELECT thread_id, seq, turn, entry, body, created_at FROM agent_steps_v2 WHERE thread_id IN (SELECT id FROM agent_threads);
INSERT INTO agent_actions (rowid, id, thread_id, turn, call_id, tool, status, compensation, target_version, proposal,
                           version, created_at, updated_at, intent, undo_refusal)
    SELECT rowid, id, thread_id, turn, call_id, tool, status, compensation, target_version, proposal, version,
           created_at, updated_at, intent, undo_refusal
    FROM agent_actions_v2 WHERE thread_id IN (SELECT id FROM agent_threads);
DROP TABLE agent_actions_v2;
DROP TABLE agent_steps_v2;
DROP TABLE agent_turns_v2;
DROP TABLE agent_threads_v2;
