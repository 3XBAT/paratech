-- The confirmed content of the attachment and the send round of a delivery (TASK-044).
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- deliveries: the SHA-256 and size of the attachment the user confirmed (AC-14). The adapter sends the file only if the
-- bytes it read match, so a file changed after the confirmation is never sent. The DEFAULTs exist only because SQLite
-- needs them to add a NOT NULL column; no delivery row exists before this migration, and the aggregate rejects them.
ALTER TABLE deliveries ADD COLUMN attachment_sha256 TEXT NOT NULL DEFAULT '' CHECK (attachment_sha256 = '' OR length(attachment_sha256) = 64);
ALTER TABLE deliveries ADD COLUMN attachment_size INTEGER NOT NULL DEFAULT 0 CHECK (attachment_size >= 0);
-- deliveries: the send round, 0 for the first job of a delivery and one more for every "send again" (UNKNOWN to
-- PENDING). The job idempotency key is per round, so retries, repeats and restarts never add a second job.
ALTER TABLE deliveries ADD COLUMN send_round INTEGER NOT NULL DEFAULT 0 CHECK (send_round >= 0);

-- +goose Down
ALTER TABLE deliveries DROP COLUMN send_round;
ALTER TABLE deliveries DROP COLUMN attachment_size;
ALTER TABLE deliveries DROP COLUMN attachment_sha256;
