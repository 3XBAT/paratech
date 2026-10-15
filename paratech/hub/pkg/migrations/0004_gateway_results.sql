-- LLM gateway key records (TASK-035, ADR-0009). A key used by the gateway records a SHA-256 of the whole request
-- (request_hash; the request text itself is never stored), and the answer of its latest attempt once that attempt
-- completed, so that a retry with the same key replays it. The answer is model output: it is kept as bytes (provider
-- text may hold NUL) and cleared by the Hub's purge when the key's retention (HUB_IDEMPOTENCY_RETENTION, counted from
-- created_at) ends. A plain Reserve has no request hash ('').

-- +goose Up
ALTER TABLE quota_idempotency_keys
    ADD COLUMN request_hash         text   NOT NULL DEFAULT '' CHECK (request_hash ~ '^([0-9a-f]{64})?$'),
    ADD COLUMN result_output        bytea,
    ADD COLUMN result_input_tokens  bigint,
    ADD COLUMN result_output_tokens bigint,
    ADD CONSTRAINT quota_idempotency_keys_result CHECK (
        (result_output IS NULL AND result_input_tokens IS NULL AND result_output_tokens IS NULL)
        OR (result_output IS NOT NULL AND request_hash <> '' AND result_input_tokens >= 0 AND result_output_tokens >= 0)
    );

-- The purge finds stored results by the start of their retention.
CREATE INDEX quota_idempotency_keys_results ON quota_idempotency_keys (created_at) WHERE result_output IS NOT NULL;

-- +goose Down
DROP INDEX quota_idempotency_keys_results;
ALTER TABLE quota_idempotency_keys
    DROP CONSTRAINT quota_idempotency_keys_result,
    DROP COLUMN result_output_tokens,
    DROP COLUMN result_input_tokens,
    DROP COLUMN result_output,
    DROP COLUMN request_hash;
