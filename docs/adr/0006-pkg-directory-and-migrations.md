# ADR-0006: The `pkg/` directory and where migrations live

- **Status:** Accepted
- **Source:** review of PR #3 (TASK-005, SQLite adapter), comment "migration code should live not in the adapter but in pkg"; decision of the project owner

## Context
`docs/architecture.md` section 2 listed the directories of a service (`cmd`, `config`, `internal`, `testenv`, `tests`) and had no place for
artifacts that are bound to a technology but are neither business code nor a layer. TASK-005 therefore put the goose migrations
(`0001_init.sql`) and the `go:embed` declaration inside the SQLite adapter (`internal/adapters/sqlite/migrations`). The PR review pointed out
that migrations do not belong to the adapter. The owner confirmed that `pkg` was forgotten in the architecture: it must be added, and
migrations of any database always live there.

## Decision
1. Every service gets a top-level directory `<service>/pkg/` (rule **ARCH-04** in `docs/architecture.md`). It holds non-business,
   technology-bound artifacts that are shared by adapters and `app` and are not code of a layer. The first content is `pkg/migrations`.
2. `pkg` imports only the standard library (and its own tree from external tests), has no business logic, and every package directly under
   it has a `doc.go`. It is imported only by `adapters` and `app` (and by tests). `domain`, `cases`, `input`, `cmd`, `config` and `metrics`
   must not import it.
3. Migrations of any database always live in `<service>/pkg/migrations`: `desktop/pkg/migrations` for SQLite now, `hub/pkg/migrations` when the
   Hub gets its Postgres database. The database adapter only executes them.
4. The rules are enforced in both modules by `tests/arch_test.go` (a `pkg` layer in `classify`, negative tests on synthetic trees, the layout
   check allows `pkg` and reports unknown top-level directories with Go code) and by `depguard` rules in `paratech/.golangci.yml`.

## Consequences
- `go:embed` can only embed files that are in the embedding package's directory tree, so the SQL files and the embedding code move together.
  `paratech/desktop/pkg/migrations` is package `migrations`: it embeds `*.sql` and exposes them through `func FS() fs.FS`, so there is no
  exported mutable global (ARCH-53). The embedded variable itself is read-only build-time data and stays unexported. The files are at the root
  of the returned file system, which is the layout `goose.NewProvider` expects, so the adapter passes `migrations.FS()` to goose directly.
- The SQLite adapter imports `paratech/desktop/pkg/migrations`. `0001_init.sql` was moved with
  `git mv`; its content and goose markers are unchanged, so databases that already applied version 1 are unaffected.
- `sqlc` reads the same files: `schema` in `internal/adapters/sqlite/sqlc.yaml` is `../../../pkg/migrations`. The generated `sqlcgen` is
  byte-identical, and the CI drift step (which runs `sqlc generate` inside the adapter directory) needs no change.
- The Hub module gets the same rules now (`hub/tests/arch_test.go`, `depguard`) but no `hub/pkg` directory until it has a database.
- The acceptance text of TASK-005 that names `adapters/sqlite/migrations` is superseded by this ADR; `docs/contracts/schema.sql`, the README
  and `docs/refs/sqlite-goose-sqlc.md` point to the new location. A schema change is still a new migration file plus the same change in
  `docs/contracts/schema.sql`.
- Further technology-bound assets (for example other embedded resources shared by adapters) may be added under `pkg/` without a new ADR as long as
  they satisfy ARCH-04; anything that needs another dependency or contains logic does not belong there.
