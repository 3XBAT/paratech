# TASK-313 — check steps (owner)

No screen changes in this task: it adds the «Выполнено» mark to the data model only (the buttons and filters come with TASK-314 and later).

1. Start the app on your current profile (cold start). Everything looks as before: the «Задания» list and «Сегодня» show the same assignments; the database migrates to version 46 without errors.
2. Nothing in the UI can mark an assignment done yet, so there is nothing to click; the existing assignments are all «не выполнены».
3. Tests: `go test -race ./desktop/internal/domain/coursework/assignment/... ./desktop/internal/adapters/sqlite/... ./desktop/pkg/migrations/...` and `go test ./desktop/tests/...` (architecture) from `paratech/`.
