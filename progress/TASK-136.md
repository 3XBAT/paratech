# TASK-136: faster SQLite tests

## Timings (go test -race -count=1, through heavy.sh, package Elapsed from -json, same idle machine)

| Package | Before | After |
|---|---|---|
| desktop/internal/adapters/sqlite | 364 s (owner saw 713 s under load) | 34 s |
| desktop/tests | 144 s | 38 s |

Where the time went: every test database was migrated from scratch (23 goose migrations, about 1 s each under
-race), and many tests open several databases (table-driven subtests). Median test after the change: 0.05 s.

## What was done
- A database is migrated once per test process (`sync.Once`), closed (WAL checkpointed, side files removed) and
  its bytes kept in memory; each new test database is a copy of those bytes. `Open` still runs with `Migrate: true`
  on the copy and finds it up to date, so the open path stays the production one.
  - adapters/sqlite: `seedFromTemplate`/`migratedTemplate` in `sqlite_test.go`, used by `openAt` (so `openTest`)
    when `Migrate` is set and the file does not exist yet. An existing file is never touched; `Migrate: false`
    still gives an empty database.
  - desktop/tests: new `sqlitedb_test.go` with `openSQLite`, the same logic around `sqlite.Open`.
- Tests of the migrations themselves migrate for real through the new `openMigrating` helper and run with
  `t.Parallel()` (each has its own file; goose Provider API, no globals).
- No production code changed; pragmas and defaults are untouched (fast test-only pragmas were not needed).

## Changed existing tests
- migrate_test.go: `TestMigrateCreatesAllTables`, `TestMigrateIsIdempotent`, `TestMigrateDownAndUpAgain`,
  `TestMigrationMatchesContractSchema` use `openMigrating` (were `openTest`) so they keep migrating for real now
  that `openTest` uses the template.
- `t.Parallel()` added to the real-migration tests: the four above, `TestMigrationToVersion8DropsCompletionNotices`,
  `TestContractComparisonDetectsDrift`, and `TestMigrationToVersion{2,9,10,11,12,13,14}*` (assignments, transcriptparts,
  jobs, speeds, silent, lessonsnapshot, lessons test files).
- `seedFromTemplate(t, path)` before a direct `Open(Migrate: true)` on a new path, where migration is not what is
  tested: `TestCloseReleasesFiles`, `TestReopenKeepsData`, `TestOpenRefusesNewerSchemaWithAndWithoutMigrate`,
  `TestTranscriptPartsSurviveRestart`, `TestCloseWakesWritersWaitingForSlot`, `reopenSettings` (settings tests).
- desktop/tests: `sqlite.Open(..., Migrate: true)` replaced by `openSQLite(...)` in 11 scenario files (same
  arguments and error handling).

No test was removed, skipped or weakened.

## Follow-ups
- Remaining slow sqlite tests are the real migration tests (2-6 s each, now in parallel).
- desktop/tests: the slowest ones wait on real timers/processes (quota wait, recorder child), not on SQLite.
