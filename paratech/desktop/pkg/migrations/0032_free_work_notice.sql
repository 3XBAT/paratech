-- The free-work card of the agent (TASK-188).
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- settings: 1 when the agent shows «Такого задания нет — могу сделать как свободную работу» before a work on a topic
-- (settings.FreeWorkNotice, on by default), 0 after «Больше не показывать».
ALTER TABLE settings ADD COLUMN free_work_notice INTEGER NOT NULL DEFAULT 1 CHECK (free_work_notice IN (0, 1));

-- +goose Down
ALTER TABLE settings DROP COLUMN free_work_notice;
