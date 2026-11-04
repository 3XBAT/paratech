-- The chat summary reader of context compaction (ADR-0028, TASK-267), served by Complete with a server prompt.
-- Down fails while key records of chat_summary exist; delete them first if a rollback is really needed.

-- +goose Up
ALTER TABLE quota_idempotency_keys
    DROP CONSTRAINT quota_idempotency_keys_operation_type_check,
    ADD CONSTRAINT quota_idempotency_keys_operation_type_check
        CHECK (operation_type IN ('summary', 'extract_homework_deadlines', 'homework_draft', 'lecture_text',
                                  'agent_step', 'inspect_material', 'answer', 'triage_assignment', 'chat_summary'));

-- +goose Down
ALTER TABLE quota_idempotency_keys
    DROP CONSTRAINT quota_idempotency_keys_operation_type_check,
    ADD CONSTRAINT quota_idempotency_keys_operation_type_check
        CHECK (operation_type IN ('summary', 'extract_homework_deadlines', 'homework_draft', 'lecture_text',
                                  'agent_step', 'inspect_material', 'answer', 'triage_assignment'));
