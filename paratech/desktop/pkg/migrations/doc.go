// Package migrations holds the embedded SQL migrations of the desktop database (docs/architecture.md section 2,
// ARCH-04, ADR-0006).
//
// The files are goose migrations (NNNN_name.sql with "-- +goose Up" and "-- +goose Down" markers). They live here
// and not in the SQLite adapter because migrations of any database always live in pkg/migrations; the adapter only
// runs them. sqlc reads the same directory as its schema (see the sqlc.yaml of the adapter), so queries are checked
// against the real schema. docs/contracts/schema.sql is the source of truth for the schema; a schema change is a new
// migration file, never an edit of an applied one.
//
// The package imports only the standard library (ARCH-04) and may be imported only by adapters and app.
package migrations
