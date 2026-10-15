package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"paratech/hub/internal/domain/access/apikey"
	"paratech/hub/internal/domain/shared"
)

// Operation names used in error messages.
const (
	opCreateKey = "create api key"
	opGetKey    = "get api key"
	opRevokeKey = "revoke api key"
	opListKeys  = "list api keys"
)

// pgUniqueViolation is the SQLSTATE of a unique constraint violation.
const pgUniqueViolation = "23505"

const keyColumns = "id, label, secret_hash, created_at, revoked_at"

// APIKeys is the API key repository (table api_keys of migration 0002). It stores only the hash of a secret.
type APIKeys struct {
	db *DB
}

// APIKeys returns the API key repository of db.
func (db *DB) APIKeys() *APIKeys { return &APIKeys{db: db} }

// Create stores a new key. An existing id is shared.ErrAlreadyExists.
func (r *APIKeys) Create(ctx context.Context, k *apikey.Key) error {
	if err := r.db.open(); err != nil {
		return fmt.Errorf("%s: %w", opCreateKey, err)
	}
	_, err := r.db.pool.Exec(ctx,
		"INSERT INTO api_keys (id, label, secret_hash, created_at) VALUES ($1, $2, $3, $4)",
		k.ID().String(), k.Label().String(), k.SecretHash(), k.CreatedAt())
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return fmt.Errorf("%s: %w", opCreateKey, shared.ErrAlreadyExists)
	}
	return r.db.queryError(ctx, opCreateKey, err)
}

// Get loads a key. A missing one is shared.ErrNotFound.
func (r *APIKeys) Get(ctx context.Context, id shared.APIKeyID) (*apikey.Key, error) {
	if err := r.db.open(); err != nil {
		return nil, fmt.Errorf("%s: %w", opGetKey, err)
	}
	row := r.db.pool.QueryRow(ctx, "SELECT "+keyColumns+" FROM api_keys WHERE id = $1", id.String())
	k, err := scanKey(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%s: %w", opGetKey, shared.ErrNotFound)
	}
	if err != nil {
		return nil, r.db.rowError(ctx, opGetKey, err)
	}
	return k, nil
}

// SaveRevocation stores the revocation time of a revoked key if the stored key is still active. Otherwise it returns
// apikey.ErrAlreadyRevoked, or shared.ErrNotFound for a missing key.
func (r *APIKeys) SaveRevocation(ctx context.Context, k *apikey.Key) error {
	if err := r.db.open(); err != nil {
		return fmt.Errorf("%s: %w", opRevokeKey, err)
	}
	if k.RevokedAt().IsZero() {
		return fmt.Errorf("%s: %w: the key is not revoked", opRevokeKey, apikey.ErrInvalid)
	}

	tag, err := r.db.pool.Exec(ctx,
		"UPDATE api_keys SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL", k.ID().String(), k.RevokedAt())
	if err != nil {
		return r.db.queryError(ctx, opRevokeKey, err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}

	var exists bool
	if err := r.db.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM api_keys WHERE id = $1)", k.ID().String()).Scan(&exists); err != nil {
		return r.db.queryError(ctx, opRevokeKey, err)
	}
	if !exists {
		return fmt.Errorf("%s: %w", opRevokeKey, shared.ErrNotFound)
	}
	return fmt.Errorf("%s: %w", opRevokeKey, apikey.ErrAlreadyRevoked)
}

// List returns every key ordered by creation time, then id.
func (r *APIKeys) List(ctx context.Context) ([]*apikey.Key, error) {
	if err := r.db.open(); err != nil {
		return nil, fmt.Errorf("%s: %w", opListKeys, err)
	}
	rows, err := r.db.pool.Query(ctx, "SELECT "+keyColumns+" FROM api_keys ORDER BY created_at, id")
	if err != nil {
		return nil, r.db.queryError(ctx, opListKeys, err)
	}
	defer rows.Close()

	var out []*apikey.Key
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, r.db.rowError(ctx, opListKeys, err)
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, r.db.queryError(ctx, opListKeys, err)
	}
	return out, nil
}

// errRestore marks a row that the domain rejected, so rowError can tell it from a driver error.
type errRestore struct{ err error }

func (e errRestore) Error() string { return e.err.Error() }

func scanKey(row pgx.Row) (*apikey.Key, error) {
	var (
		id, label string
		hash      []byte
		createdAt time.Time
		revokedAt *time.Time
	)
	if err := row.Scan(&id, &label, &hash, &createdAt, &revokedAt); err != nil {
		return nil, err
	}

	l, err := apikey.NewLabel(label)
	if err != nil {
		return nil, errRestore{err}
	}
	var revoked time.Time
	if revokedAt != nil {
		revoked = revokedAt.UTC()
	}
	k, err := apikey.Restore(shared.APIKeyID(id), l, hash, createdAt.UTC(), revoked)
	if err != nil {
		return nil, errRestore{err}
	}
	return k, nil
}

// open reports ErrClosed after Close.
func (db *DB) open() error {
	if db.closed.Load() {
		return &Error{Kind: ErrClosed}
	}
	return nil
}

// queryError translates a driver error of op (ARCH-32).
func (db *DB) queryError(ctx context.Context, op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, translate(ctx, ErrQuery, err, db.secret))
}

// rowError is queryError that reports a row rejected by the domain as ErrCorrupt.
func (db *DB) rowError(ctx context.Context, op string, err error) error {
	var bad errRestore
	if errors.As(err, &bad) {
		return fmt.Errorf("%s: %w", op, &Error{Kind: ErrCorrupt, Detail: bad.err.Error()})
	}
	return db.queryError(ctx, op, err)
}
