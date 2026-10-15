// Package postgres is the Hub's Postgres adapter: a pgx connection pool, readiness ping and the goose migrations of
// pkg/migrations (ARCH-04: the adapter only runs them), and the repositories: APIKeys (apikeys.go); the quota ledger
// of a later task lives here too.
//
// Driver errors are translated into the typed errors of errors.go (ARCH-32). The database URL holds a password, so it
// never appears in errors or logs.
package postgres
