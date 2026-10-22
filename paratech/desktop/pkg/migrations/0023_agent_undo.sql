-- Undo of the agent's actions (TASK-123, ADR-0021): what was known before an action ran, so a crash between its two
-- transactions is settled at the next start, and why an Undo was refused. No row needs a value: no Undo ran before.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- agent_actions (migration 0023): intent is the JSON object {"target","before","prior","name"} of action.Intent (NULL:
-- none); undo_refusal is why an UNDO_REFUSED entry was refused. The journal of the panel lists entries by created_at.
ALTER TABLE agent_actions ADD COLUMN intent TEXT CHECK (intent IS NULL OR (json_valid(intent) AND json_type(intent) = 'object'));
ALTER TABLE agent_actions ADD COLUMN undo_refusal TEXT NOT NULL DEFAULT '' CHECK (undo_refusal IN ('', 'changed', 'result_ready', 'cannot_stop'));
CREATE INDEX idx_agent_actions_created ON agent_actions (created_at);

-- +goose Down
DROP INDEX idx_agent_actions_created;
ALTER TABLE agent_actions DROP COLUMN undo_refusal;
ALTER TABLE agent_actions DROP COLUMN intent;
