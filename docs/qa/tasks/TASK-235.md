# TASK-235 — QA

This task is core only: there is no screen yet (Wails binding TASK-236, page TASK-238). Nothing is sent.

1. Start the app on a copy of the data: the migration 0038 applies without errors and existing subjects open as before.
2. Run `go test -race ./desktop/internal/domain/library/... ./desktop/internal/cases/library/... ./desktop/internal/adapters/sqlite/... ./desktop/tests/...` from `paratech/`: all green.
3. Check that a long agent session still lists subjects by name only: the Hub request log (fake Hub test `TestDriverBaseCarriesNoTeacher`) contains no teacher name or address.
4. After TASK-236/238: set a teacher, restart the app, the teacher is still there; clear it, it is gone.
