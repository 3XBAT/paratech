// Package sqlite is the SQLite storage adapter of the desktop core (ARCH-30..35, ADR-0001, ADR-0004).
//
// It owns everything that touches the local database file: opening it with the pure Go driver modernc.org/sqlite (no
// CGO, hard rule 1), the goose migrations that turn docs/contracts/schema.sql into the real schema, the sqlc-generated
// queries (package sqlcgen, committed), the transaction port implementation (DB.Do) and the translation of driver
// errors into domain sentinels (MapError, VersionCAS).
//
// Repositories for the aggregates are added by the tasks that own the aggregates (jobs, settings, recording, ...).
// They live in this package, take the active transaction from the context with DB.DBTX or DB.Queries, translate every
// database error with MapError and implement the optimistic concurrency protocol of ADR-0004(d) with VersionCAS.
//
// Connection model. Every pooled connection is opened with the same DSN (see Open), so the pragmas that are
// per-connection (foreign keys, busy timeout, synchronous) hold on all of them. Reads run concurrently under WAL.
// Writers are serialized inside the process by a write slot that DB.Do takes before it takes a pooled connection, so
// waiting writers never hold connections; SQLite's BEGIN IMMEDIATE and busy timeout then arbitrate only against other
// processes and fail with ErrBusy. A function running inside DB.Do holds the write lock, so it must be short and do no
// external I/O. Rule for repositories: every write runs through DB.Do (a nested Do joins the outer transaction); a bare
// autocommit write on the pool would skip the slot and could wait in SQLite's busy handler while holding a connection.
package sqlite
