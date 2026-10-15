-- Per-tester API keys (PRD section 12, AC-25). Only the SHA-256 of the secret part is stored; the plaintext key is
-- shown once when it is issued and exists nowhere else. id is the public part of the key. A NULL revoked_at means the
-- key is active; revocation is one-way.

-- +goose Up
CREATE TABLE api_keys (
    id          text        PRIMARY KEY CHECK (id ~ '^[0-9a-f]{16}$'),
    label       text        NOT NULL CHECK (label ~ '^[a-z0-9][a-z0-9_-]{0,31}$'),
    secret_hash bytea       NOT NULL CHECK (octet_length(secret_hash) = 32),
    created_at  timestamptz NOT NULL,
    revoked_at  timestamptz
);

-- +goose Down
DROP TABLE api_keys;
