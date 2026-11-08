# TASK-276: check steps

The task is core-only: no screen, binding or visible behaviour changes yet (the setting appears in TASK-279, the question in TASK-277/279). A cold start of the app must look exactly as before.

1. Start the app on your existing data. Migration 0051 runs silently; the app opens as usual and settings keep their values.
2. Nothing else is visible. The check is automated:
   - `go -C <repo>/paratech test -race ./desktop/internal/domain/... ./desktop/internal/cases/preferences/... ./desktop/internal/adapters/sqlite/... ./desktop/tests/...`
   - the eventrefresh table test shows every case of policy × event × due change.
3. Optional (database): `settings.deadline_event_on_due_change` = `ask` and an empty table `deadline_event_updates` exist after the migration.
