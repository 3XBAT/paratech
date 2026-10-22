-- The agent panel (TASK-116, ADR-0021): threads per subject, their requests, the history sent to the planner and the
-- action journal. No secrets: the seal of echoed calls is the Hub's HMAC over them, not a credential. History is kept
-- 90 days (created_at), then only the journal lines stay.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- ---------------------------------------------------------------------------------------------------------------
-- agent_threads (migration 0021): one thread per subject key (the subject id, or 'NONE' for «Без предмета»).
-- Aggregate agent/thread; its current request is the agent_turns row number = turns. seen is the JSON array of the
-- {"kind","id"} the provenance check knows from tool results; history_from is the first request whose history is
-- sent (a request restarted after its model turn expired on the Hub sends only its own).
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE agent_threads (
    id           TEXT    PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    subject_key  TEXT    NOT NULL UNIQUE CHECK (length(subject_key) BETWEEN 1 AND 128),
    turns        INTEGER NOT NULL DEFAULT 0 CHECK (turns >= 0),
    history_from INTEGER NOT NULL DEFAULT 0 CHECK (history_from BETWEEN 0 AND turns),
    seen         TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(seen) AND json_type(seen) = 'array'),
    version      INTEGER NOT NULL DEFAULT 0,                                 -- optimistic concurrency (ADR-0004)
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);

-- ---------------------------------------------------------------------------------------------------------------
-- agent_turns: the requests of a thread, written with the thread (part of its aggregate, no version of its own).
-- Budget counters, the plan (plan_steps JSON), the calls of the last step that wait for results and the ids created
-- by approved create steps. planner_input counts the planner input of the request.
-- ---------------------------------------------------------------------------------------------------------------
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

-- ---------------------------------------------------------------------------------------------------------------
-- agent_steps: the history of a thread as sent to the planner, in order (seq): the student's text, the sealed calls
-- of a step and their results, as JSON. Append only; purged after 90 days by created_at.
-- ---------------------------------------------------------------------------------------------------------------
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

-- ---------------------------------------------------------------------------------------------------------------
-- agent_actions: the action journal (aggregate agent/action). A U call is PENDING before its use case runs and gets
-- its outcome after; compensation is the typed Undo data (JSON {"kind", ...ids, old values}) of a DONE entry and
-- target_version the version of the target right after the action. proposal is '<kind>:<id>' of a PROPOSED entry.
-- One entry per call of a request.
-- ---------------------------------------------------------------------------------------------------------------
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
    UNIQUE (thread_id, turn, call_id)
);
CREATE INDEX idx_agent_actions_thread ON agent_actions (thread_id, created_at);
CREATE INDEX idx_agent_actions_open ON agent_actions (status) WHERE status IN ('PENDING', 'UNDOING');

-- +goose Down
DROP INDEX idx_agent_actions_open;
DROP INDEX idx_agent_actions_thread;
DROP TABLE agent_actions;
DROP INDEX idx_agent_steps_created;
DROP INDEX idx_agent_steps_turn;
DROP TABLE agent_steps;
DROP TABLE agent_turns;
DROP TABLE agent_threads;
