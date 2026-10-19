-- The lecture_text operation (TASK-090): full readable text of the lesson formatted from the transcript, quota counted as its own operation.
-- Down fails while key records of lecture_text exist; delete them first if a rollback is really needed.

-- +goose Up
ALTER TABLE quota_idempotency_keys
    DROP CONSTRAINT quota_idempotency_keys_operation_type_check,
    ADD CONSTRAINT quota_idempotency_keys_operation_type_check
        CHECK (operation_type IN ('summary', 'extract_homework_deadlines', 'homework_draft', 'lecture_text'));

-- +goose Down
ALTER TABLE quota_idempotency_keys
    DROP CONSTRAINT quota_idempotency_keys_operation_type_check,
    ADD CONSTRAINT quota_idempotency_keys_operation_type_check
        CHECK (operation_type IN ('summary', 'extract_homework_deadlines', 'homework_draft'));
