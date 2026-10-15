-- Quota ledger (PRD section 9.2, AC-11; ADR-0008). One account per API key holds the used and held totals; the limit
-- comes from the Hub configuration and is not stored. A reservation is one attempt of an idempotency key; the key
-- record keeps the fingerprint (operation, model alias, amount: no request text) and points to its latest attempt.
-- quota_ledger is append-only: one row per movement, and its held/used deltas sum to the account totals. Every
-- command locks its quota_accounts row (SELECT ... FOR UPDATE), so the commands of one tester are serialized.

-- +goose Up
CREATE TABLE quota_accounts (
    api_key_id text        PRIMARY KEY REFERENCES api_keys (id),
    used       bigint      NOT NULL CHECK (used >= 0),
    held       bigint      NOT NULL CHECK (held >= 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE quota_reservations (
    id              text        PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
    api_key_id      text        NOT NULL REFERENCES quota_accounts (api_key_id),
    idempotency_key text        NOT NULL CHECK (idempotency_key ~ '^[!-~]{1,128}$'),
    attempt         integer     NOT NULL CHECK (attempt >= 1),
    amount          bigint      NOT NULL CHECK (amount > 0),
    state           text        NOT NULL CHECK (state IN ('reserved', 'settled', 'released')),
    release_reason  text        NOT NULL CHECK (release_reason IN ('', 'explicit', 'failed', 'expired')),
    charged         bigint      NOT NULL CHECK (charged >= 0),
    actual          bigint      NOT NULL CHECK (actual >= charged),
    partial         boolean     NOT NULL,
    created_at      timestamptz NOT NULL,
    expires_at      timestamptz NOT NULL CHECK (expires_at > created_at),
    closed_at       timestamptz,
    UNIQUE (api_key_id, idempotency_key, attempt),
    UNIQUE (id, api_key_id),
    CHECK (
        (state = 'reserved' AND release_reason = '' AND actual = 0 AND NOT partial AND closed_at IS NULL)
        OR (state = 'settled' AND release_reason = '' AND closed_at IS NOT NULL)
        OR (state = 'released' AND release_reason <> '' AND actual = 0 AND NOT partial AND closed_at IS NOT NULL)
    )
);

-- Lazy expiry looks up the open reservations of one account by expiry.
CREATE INDEX quota_reservations_open ON quota_reservations (api_key_id, expires_at, id) WHERE state = 'reserved';

CREATE TABLE quota_idempotency_keys (
    api_key_id            text        NOT NULL REFERENCES quota_accounts (api_key_id),
    idempotency_key       text        NOT NULL CHECK (idempotency_key ~ '^[!-~]{1,128}$'),
    operation_type        text        NOT NULL CHECK (operation_type IN ('summary', 'extract_homework_deadlines', 'homework_draft')),
    model_alias           text        NOT NULL CHECK (model_alias ~ '^[a-z0-9][a-z0-9_-]{0,63}$'),
    amount                bigint      NOT NULL CHECK (amount > 0),
    created_at            timestamptz NOT NULL,
    attempts              integer     NOT NULL CHECK (attempts >= 1),
    latest_reservation_id text        NOT NULL,
    PRIMARY KEY (api_key_id, idempotency_key),
    FOREIGN KEY (latest_reservation_id, api_key_id) REFERENCES quota_reservations (id, api_key_id)
);

CREATE TABLE quota_ledger (
    id             bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    api_key_id     text        NOT NULL REFERENCES quota_accounts (api_key_id),
    reservation_id text        NOT NULL,
    kind           text        NOT NULL CHECK (kind IN ('reserve', 'settle', 'release', 'expire')),
    held_delta     bigint      NOT NULL,
    used_delta     bigint      NOT NULL,
    created_at     timestamptz NOT NULL,
    FOREIGN KEY (reservation_id, api_key_id) REFERENCES quota_reservations (id, api_key_id),
    CHECK (
        (kind = 'reserve' AND held_delta > 0 AND used_delta = 0)
        OR (kind = 'settle' AND held_delta <= 0 AND used_delta >= 0)
        OR (kind IN ('release', 'expire') AND held_delta < 0 AND used_delta = 0)
    )
);

CREATE INDEX quota_ledger_account ON quota_ledger (api_key_id, id);

-- +goose Down
DROP TABLE quota_ledger;
DROP TABLE quota_idempotency_keys;
DROP TABLE quota_reservations;
DROP TABLE quota_accounts;
