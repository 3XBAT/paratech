-- The agent operations (ADR-0023, TASK-114): agent_step names the ledger entries of GatewayService.AgentStep;
-- inspect_material, answer and triage_assignment are the quarantined readers served by Complete with a server prompt.
-- Down fails while key records of these operations exist; delete them first if a rollback is really needed.

-- +goose Up
ALTER TABLE quota_idempotency_keys
    DROP CONSTRAINT quota_idempotency_keys_operation_type_check,
    ADD CONSTRAINT quota_idempotency_keys_operation_type_check
        CHECK (operation_type IN ('summary', 'extract_homework_deadlines', 'homework_draft', 'lecture_text',
                                  'agent_step', 'inspect_material', 'answer', 'triage_assignment'));

-- +goose Down
ALTER TABLE quota_idempotency_keys
    DROP CONSTRAINT quota_idempotency_keys_operation_type_check,
    ADD CONSTRAINT quota_idempotency_keys_operation_type_check
        CHECK (operation_type IN ('summary', 'extract_homework_deadlines', 'homework_draft', 'lecture_text'));
