# TASK-231 — owner's check

The task adds bindings only; the buttons come in TASK-232, so there is nothing to click yet.

1. `go test -race ./desktop/internal/input/wails/...` in `paratech/` passes.
2. `npm run test` in `paratech/desktop/frontend` passes (homeworkchange tests: every problem code, malformed replies become `failed`).
3. After TASK-232: delete an assignment, press «Вернуть», edit title/note/deadline; a second click on a stale card says the assignment changed.
