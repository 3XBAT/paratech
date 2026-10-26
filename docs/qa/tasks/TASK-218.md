# TASK-218 — check steps

Binding only (no UI yet); the owner's check is by tests.

1. From `paratech/`: `go test -race ./desktop/internal/input/wails/... ./desktop/internal/app/... ./desktop/tests/...` must pass.
2. `npm --prefix paratech/desktop/frontend run test` and `run build` pass (`test/homeworkcreate.test.mjs`).
3. The form and the button come with TASK-219; nothing changes in the running app yet.
