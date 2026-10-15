package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/internal/domain/shared"
)

// Operation names used in error messages.
const (
	opModifyQuota = "modify quota account"
	opReadQuota   = "read quota account"
)

// quotaTimeout bounds one quota transaction, lock wait included, so a hanging database fails the command instead of
// hanging it.
const quotaTimeout = 5 * time.Second

// pgForeignKeyViolation is the SQLSTATE of a foreign key violation.
const pgForeignKeyViolation = "23503"

const reservationColumns = "id, idempotency_key, attempt, amount, state, release_reason, charged, actual, partial, " +
	"created_at, expires_at, closed_at"

// querier is what loading needs from a transaction.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// QuotaAccounts is the quota account repository (tables quota_* of migration 0003). It implements the Accounts port
// of the quota use case.
type QuotaAccounts struct {
	db *DB
}

// QuotaAccounts returns the quota account repository of db.
func (db *DB) QuotaAccounts() *QuotaAccounts { return &QuotaAccounts{db: db} }

// Modify runs fn on the account of scope.Owner in one transaction: it creates the account row if needed, locks it
// with SELECT ... FOR UPDATE (every command of one tester waits for the previous one), loads what scope asks for,
// runs fn and, if fn returns nil, stores the changes and the ledger movements. An error of fn is returned unchanged
// and rolls everything back. An owner that is not a stored API key is shared.ErrNotFound.
func (r *QuotaAccounts) Modify(ctx context.Context, scope account.Scope, fn func(*account.Account) error) error {
	if err := r.db.open(); err != nil {
		return fmt.Errorf("%s: %w", opModifyQuota, err)
	}
	ctx, cancel := context.WithTimeout(ctx, quotaTimeout)
	defer cancel()

	tx, err := r.db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return r.db.queryError(ctx, opModifyQuota, err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // a no-op after Commit

	owner := scope.Owner.String()
	_, err = tx.Exec(ctx, "INSERT INTO quota_accounts (api_key_id, used, held, created_at, updated_at) "+
		"VALUES ($1, 0, 0, $2, $2) ON CONFLICT (api_key_id) DO NOTHING", owner, scope.Now)
	if err != nil {
		return r.writeError(ctx, err)
	}
	var used, held int64
	if err := tx.QueryRow(ctx, "SELECT used, held FROM quota_accounts WHERE api_key_id = $1 FOR UPDATE", owner).Scan(&used, &held); err != nil {
		return r.db.queryError(ctx, opModifyQuota, err)
	}

	a, err := r.load(ctx, tx, opModifyQuota, scope, used, held, true)
	if err != nil {
		return err
	}
	if err := fn(a); err != nil {
		return err
	}
	if err := r.save(ctx, tx, scope, a.Changes()); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return r.db.queryError(ctx, opModifyQuota, err)
	}
	return nil
}

// Read loads the account of scope.Owner with its overdue reservations from one read-only snapshot. It writes
// nothing; an owner without an account row gets an empty account.
func (r *QuotaAccounts) Read(ctx context.Context, scope account.Scope) (*account.Account, error) {
	if err := r.db.open(); err != nil {
		return nil, fmt.Errorf("%s: %w", opReadQuota, err)
	}
	ctx, cancel := context.WithTimeout(ctx, quotaTimeout)
	defer cancel()

	tx, err := r.db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, r.db.queryError(ctx, opReadQuota, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var used, held int64
	err = tx.QueryRow(ctx, "SELECT used, held FROM quota_accounts WHERE api_key_id = $1", scope.Owner.String()).Scan(&used, &held)
	if errors.Is(err, pgx.ErrNoRows) {
		return account.New(scope.Owner, scope.Policy)
	}
	if err != nil {
		return nil, r.db.queryError(ctx, opReadQuota, err)
	}
	return r.load(ctx, tx, opReadQuota, scope, used, held, false)
}

// load restores the account with the reservations scope asks for (account.Scope): the overdue RESERVED ones and,
// with targets, the latest attempt of scope.Key and scope.Reservation. Every query is scoped by the owner.
func (r *QuotaAccounts) load(ctx context.Context, q querier, op string, scope account.Scope, used, held int64, targets bool) (*account.Account, error) {
	owner := scope.Owner.String()
	seen := map[shared.ReservationID]bool{}
	var rs []account.ReservationRecord
	add := func(rec account.ReservationRecord) {
		if !seen[rec.ID] {
			seen[rec.ID] = true
			rs = append(rs, rec)
		}
	}

	rows, err := q.Query(ctx, "SELECT "+reservationColumns+" FROM quota_reservations "+
		"WHERE api_key_id = $1 AND state = 'reserved' AND expires_at <= $2 ORDER BY expires_at, id LIMIT $3",
		owner, scope.Now, account.MaxExpiredPerLoad)
	if err != nil {
		return nil, r.db.queryError(ctx, op, err)
	}
	overdue, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (account.ReservationRecord, error) { return scanReservation(row) })
	if err != nil {
		return nil, r.db.queryError(ctx, op, err)
	}
	for _, rec := range overdue {
		add(rec)
	}

	var keys []account.KeyRecord
	if targets && scope.Key != "" {
		k, err := scanKeyRecord(q.QueryRow(ctx, "SELECT idempotency_key, operation_type, model_alias, amount, created_at, "+
			"attempts, latest_reservation_id FROM quota_idempotency_keys WHERE api_key_id = $1 AND idempotency_key = $2",
			owner, string(scope.Key)))
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return nil, r.db.queryError(ctx, op, err)
		default:
			latest, err := r.reservation(ctx, q, op, owner, k.Latest)
			if err != nil {
				return nil, err
			}
			keys = append(keys, k)
			add(latest)
		}
	}
	if targets && scope.Reservation != "" {
		rec, err := r.reservation(ctx, q, op, owner, scope.Reservation)
		switch {
		case errors.Is(err, shared.ErrNotFound): // unknown or another tester's: the aggregate reports it as not found
		case err != nil:
			return nil, err
		default:
			add(rec)
		}
	}

	a, err := account.Restore(scope.Owner, scope.Policy, used, held, rs, keys)
	if errors.Is(err, account.ErrInvalidRecord) {
		return nil, fmt.Errorf("%s: %w", op, &Error{Kind: ErrCorrupt, Detail: err.Error()})
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return a, nil
}

// reservation loads one reservation of owner; a missing one (or one of another owner) is shared.ErrNotFound.
func (r *QuotaAccounts) reservation(ctx context.Context, q querier, op, owner string, id shared.ReservationID) (account.ReservationRecord, error) {
	rec, err := scanReservation(q.QueryRow(ctx, "SELECT "+reservationColumns+" FROM quota_reservations WHERE id = $1 AND api_key_id = $2",
		id.String(), owner))
	if errors.Is(err, pgx.ErrNoRows) {
		return account.ReservationRecord{}, fmt.Errorf("%s: %w", op, shared.ErrNotFound)
	}
	if err != nil {
		return account.ReservationRecord{}, r.db.queryError(ctx, op, err)
	}
	return rec, nil
}

// save stores the changes of one command: new and changed reservations, key records, ledger rows and the totals.
func (r *QuotaAccounts) save(ctx context.Context, tx pgx.Tx, scope account.Scope, ch account.Changes) error {
	if len(ch.Created) == 0 && len(ch.Updated) == 0 && len(ch.Keys) == 0 && len(ch.Movements) == 0 {
		return nil // a replay or a no-op
	}
	owner := scope.Owner.String()
	for _, rec := range ch.Created {
		_, err := tx.Exec(ctx, "INSERT INTO quota_reservations ("+reservationColumns+", api_key_id) "+
			"VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)",
			rec.ID.String(), string(rec.Key), rec.Attempt, rec.Amount, string(rec.State), string(rec.Reason),
			rec.Charged, rec.Actual, rec.Partial, rec.CreatedAt, rec.ExpiresAt, nullTime(rec.ClosedAt), owner)
		if err != nil {
			return r.writeError(ctx, err)
		}
	}
	for _, rec := range ch.Updated {
		tag, err := tx.Exec(ctx, "UPDATE quota_reservations SET state = $3, release_reason = $4, charged = $5, actual = $6, "+
			"partial = $7, closed_at = $8 WHERE id = $1 AND api_key_id = $2",
			rec.ID.String(), owner, string(rec.State), string(rec.Reason), rec.Charged, rec.Actual, rec.Partial, nullTime(rec.ClosedAt))
		if err != nil {
			return r.writeError(ctx, err)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("%s: %w", opModifyQuota, &Error{Kind: ErrCorrupt, Detail: "a changed reservation is not stored"})
		}
	}
	for _, k := range ch.Keys {
		_, err := tx.Exec(ctx, "INSERT INTO quota_idempotency_keys (api_key_id, idempotency_key, operation_type, model_alias, "+
			"amount, created_at, attempts, latest_reservation_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) "+
			"ON CONFLICT (api_key_id, idempotency_key) DO UPDATE SET operation_type = EXCLUDED.operation_type, "+
			"model_alias = EXCLUDED.model_alias, amount = EXCLUDED.amount, created_at = EXCLUDED.created_at, "+
			"attempts = EXCLUDED.attempts, latest_reservation_id = EXCLUDED.latest_reservation_id",
			owner, string(k.Key), string(k.Fingerprint.Operation), string(k.Fingerprint.Alias), k.Fingerprint.Amount,
			k.CreatedAt, k.Attempts, k.Latest.String())
		if err != nil {
			return r.writeError(ctx, err)
		}
	}
	for _, m := range ch.Movements {
		_, err := tx.Exec(ctx, "INSERT INTO quota_ledger (api_key_id, reservation_id, kind, held_delta, used_delta, created_at) "+
			"VALUES ($1, $2, $3, $4, $5, $6)", owner, m.Reservation.String(), string(m.Kind), m.HeldDelta, m.UsedDelta, m.At)
		if err != nil {
			return r.writeError(ctx, err)
		}
	}
	_, err := tx.Exec(ctx, "UPDATE quota_accounts SET used = $2, held = $3, updated_at = $4 WHERE api_key_id = $1",
		owner, ch.Used, ch.Held, scope.Now)
	if err != nil {
		return r.writeError(ctx, err)
	}
	return nil
}

// writeError translates a failed write: a missing API key is shared.ErrNotFound, a duplicate shared.ErrAlreadyExists.
func (r *QuotaAccounts) writeError(ctx context.Context, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgForeignKeyViolation:
			return fmt.Errorf("%s: %w", opModifyQuota, shared.ErrNotFound)
		case pgUniqueViolation:
			return fmt.Errorf("%s: %w", opModifyQuota, shared.ErrAlreadyExists)
		}
	}
	return r.db.queryError(ctx, opModifyQuota, err)
}

func scanReservation(row pgx.Row) (account.ReservationRecord, error) {
	var (
		rec                    account.ReservationRecord
		id, key, state, reason string
		createdAt, expiresAt   time.Time
		closedAt               *time.Time
	)
	err := row.Scan(&id, &key, &rec.Attempt, &rec.Amount, &state, &reason, &rec.Charged, &rec.Actual, &rec.Partial,
		&createdAt, &expiresAt, &closedAt)
	if err != nil {
		return account.ReservationRecord{}, err
	}
	rec.ID, rec.Key = shared.ReservationID(id), account.IdempotencyKey(key)
	rec.State, rec.Reason = account.State(state), account.ReleaseReason(reason)
	rec.CreatedAt, rec.ExpiresAt = createdAt.UTC(), expiresAt.UTC()
	if closedAt != nil {
		rec.ClosedAt = closedAt.UTC()
	}
	return rec, nil
}

func scanKeyRecord(row pgx.Row) (account.KeyRecord, error) {
	var (
		k                      account.KeyRecord
		key, op, alias, latest string
		createdAt              time.Time
	)
	if err := row.Scan(&key, &op, &alias, &k.Fingerprint.Amount, &createdAt, &k.Attempts, &latest); err != nil {
		return account.KeyRecord{}, err
	}
	k.Key, k.Latest, k.CreatedAt = account.IdempotencyKey(key), shared.ReservationID(latest), createdAt.UTC()
	k.Fingerprint.Operation, k.Fingerprint.Alias = account.Operation(op), account.ModelAlias(alias)
	return k, nil
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
