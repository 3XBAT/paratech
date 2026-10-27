# TASK-256: check steps

Nothing is visible in the app yet (the core only; import and UI come in later tasks).

1. Run `go test -race ./desktop/internal/domain/schedule/... ./desktop/internal/adapters/sqlite/... ./desktop/internal/adapters/googlecal/...` from `paratech/`: all green.
2. Start the app on an existing database: it opens, the schedule and «Сегодня» show the lessons as before (the migration adds an empty link column).
