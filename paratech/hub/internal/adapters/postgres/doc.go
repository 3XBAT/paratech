// Package postgres is the Hub's Postgres adapter: a pgx connection pool, readiness ping and the goose migrations of
// pkg/migrations (ARCH-04: the adapter only runs them). Repositories of later tasks (API keys, quota ledger) live here.
//
// Driver errors are translated into the typed errors of errors.go (ARCH-32). The database URL holds a password, so it
// never appears in errors or logs.
package postgres
