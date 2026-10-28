-- Agent chats storage (TASK-266, ADR-0028 «Context compaction», «Retention», «Restart from an earlier message»): the
-- frozen reply line of a request, the summary of a chat and the chat a fork was made from. Columns are added, not
-- rebuilt: the tables keep their rows, rowids and foreign keys; every existing row gets NULL and reads as before (a
-- request without a line falls back to thread.Reply of its steps, a chat without a summary sends its pairs).
-- Column CHECKs may name other columns: the all-or-none rule of the summary sits on summary_at, the last one added.
-- Sizes are bytes of UTF-8 (values.MaxAgentReplyBytes, values.MaxSummaryBytes).
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- agent_turns: the reply line frozen when the request became final (thread.FreezeReply); NULL before, and for requests
-- that ended before this migration or were cancelled by a later request.
ALTER TABLE agent_turns ADD COLUMN reply_line TEXT CHECK (reply_line IS NULL OR length(CAST(reply_line AS BLOB)) BETWEEN 1 AND 2048);
-- agent_threads: the summary of requests 1..summary_through (untrusted model output, data only) and when it was made
-- (ms); all three NULL without a summary. forked_from: the chat this one was forked from, informational, no foreign key.
ALTER TABLE agent_threads ADD COLUMN summary_text TEXT CHECK (summary_text IS NULL OR length(CAST(summary_text AS BLOB)) BETWEEN 1 AND 8192);
ALTER TABLE agent_threads ADD COLUMN summary_through INTEGER CHECK (summary_through IS NULL OR summary_through >= 1);
ALTER TABLE agent_threads ADD COLUMN summary_at INTEGER CHECK ((summary_at IS NULL) = (summary_text IS NULL) AND (summary_at IS NULL) = (summary_through IS NULL));
ALTER TABLE agent_threads ADD COLUMN forked_from TEXT CHECK (forked_from IS NULL OR length(forked_from) BETWEEN 1 AND 128);

-- +goose Down
-- Loses the frozen reply lines (Window builds them again from the steps while those are kept), the summaries (the next
-- compaction makes a new one) and the fork sources. summary_at goes first: its CHECK names the other two columns, and
-- SQLite refuses to drop a column another column's CHECK names.
ALTER TABLE agent_threads DROP COLUMN forked_from;
ALTER TABLE agent_threads DROP COLUMN summary_at;
ALTER TABLE agent_threads DROP COLUMN summary_through;
ALTER TABLE agent_threads DROP COLUMN summary_text;
ALTER TABLE agent_turns DROP COLUMN reply_line;
