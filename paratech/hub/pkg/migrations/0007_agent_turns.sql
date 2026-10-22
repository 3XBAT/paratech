-- Kept model turns of AgentStep (TASK-116, ADR-0023). The raw assistant content of every returned call set (thinking
-- blocks with their signatures, text, tool_use) is kept so that the next step of the tool loop sends it back to the
-- provider unchanged; the client never sees it. call_set is values.CallSetKey (hex SHA-256 of thread, profile,
-- catalog version and calls); rows are scoped by the API key. It is model output: kept as bytes, never logged, and
-- deleted by the Hub's purge when the result window ends (ADR-0005(d)). An empty content means the provider gave none.

-- +goose Up
CREATE TABLE agent_turns (
    api_key_id text        NOT NULL REFERENCES api_keys (id),
    call_set   text        NOT NULL CHECK (call_set ~ '^[0-9a-f]{64}$'),
    content    bytea       NOT NULL CHECK (octet_length(content) <= 1048576),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (api_key_id, call_set)
);

-- The purge finds old turns by their creation time.
CREATE INDEX agent_turns_created ON agent_turns (created_at);

-- +goose Down
DROP TABLE agent_turns;
