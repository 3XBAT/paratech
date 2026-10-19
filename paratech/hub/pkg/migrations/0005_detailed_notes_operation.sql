-- The detailed_notes operation (TASK-090): detailed structured lecture notes, quota counted as its own operation.
-- Down fails while key records of detailed_notes exist; delete them first if a rollback is really needed.

-- +goose Up
ALTER TABLE quota_idempotency_keys
    DROP CONSTRAINT quota_idempotency_keys_operation_type_check,
    ADD CONSTRAINT quota_idempotency_keys_operation_type_check
        CHECK (operation_type IN ('summary', 'extract_homework_deadlines', 'homework_draft', 'detailed_notes'));

-- +goose Down
ALTER TABLE quota_idempotency_keys
    DROP CONSTRAINT quota_idempotency_keys_operation_type_check,
    ADD CONSTRAINT quota_idempotency_keys_operation_type_check
        CHECK (operation_type IN ('summary', 'extract_homework_deadlines', 'homework_draft'));
