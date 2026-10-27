# TASK-149 — check steps

The task has no UI: it is the model and storage of «Мой конспект». Nothing to click.

1. `go -C <repo>/paratech test -race ./desktop/internal/domain/library/... ./desktop/internal/adapters/sqlite/...` passes.
2. Start the app on a copy of an existing database: it opens and migrates to version 36 without errors; recordings, subjects and materials are as before.
3. Optional (needs sqlite3): the tables `notes` and `note_parts` exist and are empty.
