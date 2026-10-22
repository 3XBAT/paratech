# TASK-121 owner check

No UI yet (frontend comes later), nothing to check by hand. From paratech/: `go test -race ./desktop/internal/domain/library/... ./desktop/internal/cases/library/ ./desktop/internal/adapters/sqlite/ ./desktop/tests/`.
The existing database gets migration 0021 on the next start (new empty library tables; recordings and schedule are untouched).
