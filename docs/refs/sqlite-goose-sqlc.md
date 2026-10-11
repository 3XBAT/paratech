# SQLite driver, goose migrations, sqlc (verified notes for TASK-005)

Everything below was read from the module source in the Go module cache (not from memory) and exercised by the tests in
`paratech/desktop/internal/adapters/sqlite`. Re-verify after bumping a version.

## Versions and sources

| Piece | Version | Source |
|---|---|---|
| `modernc.org/sqlite` (pure Go, no CGO; SQLite 3.53.x) | v1.60.1 | https://gitlab.com/cznic/sqlite , docs https://pkg.go.dev/modernc.org/sqlite@v1.60.1 ; files read: `driver.go` (DSN docs), `sqlite.go` (`applyQueryParams`), `tx.go`, `error.go`, `connector.go` |
| `modernc.org/libc` | v1.77.1 (indirect) | the package doc of modernc.org/sqlite says to use exactly the libc version in ITS go.mod (v1.77.1; https://gitlab.com/cznic/sqlite/-/issues/177) |
| `github.com/pressly/goose/v3` | v3.28.0 | https://github.com/pressly/goose/tree/v3.28.0 ; files read: `provider.go`, `provider_options.go`, `provider_types.go`, `internal/sqlparser/parser.go` |
| `sqlc` (generator, NOT in go.mod) | v1.31.1 | https://github.com/sqlc-dev/sqlc/tree/v1.31.1 , docs https://docs.sqlc.dev |

Why goose v3.28.0 and not the newest v3.29.0: v3.29.0 requires `modernc.org/libc v1.78.0`, which would move libc above the version
modernc.org/sqlite v1.60.1 asks for. v3.28.0 requires an older libc, so Go resolves exactly sqlite's v1.77.1. Revisit when
modernc.org/sqlite is upgraded (take the libc version from the new sqlite go.mod).

Other indirect modules added by `go mod tidy`: go-humanize, google/uuid, mattn/go-isatty, mfridman/interpolate, ncruces/go-strftime,
remyoudompheng/bigfft, sethvargo/go-retry, go.uber.org/multierr, golang.org/x/sync, golang.org/x/sys, modernc.org/mathutil, modernc.org/memory.
`go.work` `go` line is `1.26.0` because the new modules declare `go 1.26.0`.

## modernc.org/sqlite DSN (v1.60.1)

- `sql.Open("sqlite", dsn)` or `msqlite.NewConnector(dsn)` + `sql.OpenDB(connector)` (the adapter uses the connector: no global driver name needed).
- DSN = a plain file name, optionally followed by `?` and `&`-separated parameters. For a plain name (not starting with `file:`) the query is
  cut off before the name reaches SQLite, so a Windows path like `C:\Users\x\app.db?_txlock=immediate` works; a path containing `?` does not
  (the adapter rejects it). A `file:` URI passes SQLite's own parameters (`mode`, `cache`, ...) through; the adapter does not use it.
- Shorthand keys (validated before anything is executed; values case-insensitive), applied on EVERY new connection in this fixed order:
  `_busy_timeout` (integer ms, first), then `_pragma` values, then `_foreign_keys`, `_journal_mode`, `_synchronous`, `_query_only` last.
  - `_busy_timeout=5000` / alias `_timeout`
  - `_foreign_keys=1` / alias `_fk` (0 1 false true no yes off on)
  - `_journal_mode=WAL` / alias `_journal` (DELETE TRUNCATE PERSIST MEMORY WAL OFF)
  - `_synchronous=NORMAL` / alias `_sync` (0 OFF 1 NORMAL 2 FULL 3 EXTRA)
  - `_pragma=name(value)` runs `PRAGMA name(value)` verbatim (not used: unvalidated).
- The adapter DSN: `<path>?_busy_timeout=5000&_foreign_keys=1&_journal_mode=WAL&_synchronous=NORMAL&_txlock=immediate`
  (built with `url.Values.Encode`, so keys come out sorted; order does not matter, the driver sorts them itself).
- `_txlock=deferred|immediate|exclusive`: `tx.go` issues `begin <mode>` for transactions that are not `ReadOnly`; `database/sql` `BeginTx(ctx, nil)`
  therefore runs `begin immediate`. This also applies to goose, which starts its own transactions on the pool.
- Pragmas verified per connection by `TestOpenAppliesPragmasOnEveryConnection` (journal_mode=wal, foreign_keys=1, busy_timeout, synchronous=1).
- `Close()` of the last connection checkpoints and deletes `-wal` and `-shm`; the file can then be deleted on Windows (`TestCloseReleasesFiles`).

### Errors
- Driver errors are `*msqlite.Error` (`errors.As`), `Code()` returns the EXTENDED result code (the driver enables extended result codes);
  the primary code is `code & 0xff`. Constants live in `modernc.org/sqlite/lib` (`SQLITE_CONSTRAINT_UNIQUE` 2067, `_PRIMARYKEY` 1555,
  `_FOREIGNKEY` 787, `_TRIGGER` 1811, `_CHECK` 275, `_NOTNULL` 1299, `_ROWID` 2579, `SQLITE_BUSY` 5, `SQLITE_LOCKED` 6, `SQLITE_INTERRUPT` 9).
- Observed quirk: deleting a parent row that is referenced with `ON DELETE RESTRICT` fails with code 1811 (`SQLITE_CONSTRAINT_TRIGGER`), message
  `FOREIGN KEY constraint failed`, NOT 787. A plain FK violation on insert/update is 787.
- Observed: `sqlite3_interrupt` (used by the driver when a statement's context ends) does NOT stop SQLite's busy-handler sleep. A
  `BEGIN IMMEDIATE` waiting for a lock keeps waiting until the busy timeout even after the context is cancelled. `DB.Do` therefore runs BEGIN in
  a goroutine when the context can end and returns the context error immediately (`TestBeginImmediateWaitIsInterruptedByContext`).

## goose v3.28.0 (library use, SQL migrations embedded)

```go
//go:embed migrations/*.sql
var migrationsFS embed.FS

sub, _ := fs.Sub(migrationsFS, "migrations")          // goose wants the migration files at the root of the fs.FS
p, err := goose.NewProvider(goose.DialectSQLite3, sqlDB, sub,
    goose.WithSlog(logger),                           // else it logs through the std log package
    goose.WithDisableGlobalRegistry(true))            // only the embedded files are migrations
results, err := p.Up(ctx)                             // no-op (nil results) when nothing is pending
current, target, err := p.GetVersions(ctx)            // highest applied, highest known; current > target = DB from a newer build
v, err := p.GetDBVersion(ctx)                         // 0 when nothing was applied
_, err = p.DownTo(ctx, 0)                             // used by tests only
```
- `NewProvider(dialect Dialect, db *sql.DB, fsys fs.FS, opts ...ProviderOption)`; the dialect constant is `goose.DialectSQLite3`.
- `Provider.Close()` CLOSES the `*sql.DB` it was given. Never call it when the pool is owned elsewhere.
- Version table: `goose_db_version` (columns `id, version_id, is_applied, tstamp`); version 0 row is inserted on first use.
- File name: `<number>_<name>.sql`, e.g. `0001_init.sql` (version = number).
- SQL annotations are lines `-- +goose Up` / `-- +goose Down` with NO leading whitespace. ANY comment line that contains the text `+goose`
  is parsed as an annotation (an explanatory comment mentioning it breaks the parse), so the header comment of a migration must not quote it.
  Each statement ends with `;` at the end of a line (a trailing `-- comment` after the `;` is fine, comment-only lines are kept inside a statement).
  Triggers/compound statements need `-- +goose StatementBegin` / `StatementEnd`. Each migration runs in a transaction by default.
- Comments before the first statement of a section are dropped; comments inside a statement are kept, so `sqlite_master.sql` contains them. The
  adapter test therefore compares DDL with comments and layout stripped.

## sqlc v1.31.1

- Run without adding it to go.mod, from `paratech/` (builds with `CGO_ENABLED=0`, the first run compiles it for a few minutes):
  `CGO_ENABLED=0 go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate -f desktop/internal/adapters/sqlite/sqlc.yaml`
  (paths inside the yaml are relative to the yaml file). `... version` prints `v1.31.1`.
- `sqlc.yaml` (version "2"): `engine: sqlite`, `schema: migrations` (the goose directory: sqlc reads the Up part of each file in order and stops at the
  goose Down marker), `queries: queries`, `gen.go.package/out: sqlcgen`, `emit_interface: true`, `emit_empty_slices: true`.
- Generated: `db.go` (`DBTX` interface, `New(DBTX)`, `WithTx(*sql.Tx)`), `models.go`, `querier.go`, one `<file>.sql.go` per query file. INTEGER maps to `int64`,
  TEXT NOT NULL to `string`, nullable TEXT to `sql.NullString`, nullable INTEGER to `sql.NullInt64`.
- Query annotations: `-- name: GetSettings :one`, `:exec`, `:execrows` (returns rows affected as `int64`), `:many`. Named parameters: `sqlc.arg(name)` becomes `?N` and a field
  of the generated `<Query>Params` struct. `:execrows` is what the version compare-and-swap uses (zero rows = `shared.ErrVersionConflict`).
- Generated files carry `// Code generated by sqlc. DO NOT EDIT.` and are committed (ARCH-35).
