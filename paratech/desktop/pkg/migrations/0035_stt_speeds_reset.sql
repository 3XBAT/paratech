-- Reset of the measured recognition speeds (TASK-222, ADR-0030): whisper-cli now skips silence (voice activity
-- detection), so a run takes about half the time it took before. The speeds of stt_speeds were measured without it
-- and would show twice the real time; they are removed, the estimate falls back to the table until the next runs.
--
-- Data only: docs/contracts/schema.sql is unchanged (TestMigrationMatchesContractSchema).

-- +goose Up
DELETE FROM stt_speeds;

-- +goose Down
-- Nothing to restore: the old measurements are gone and the next runs measure again.
