# TASK-236 — QA

There is no UI yet (the page is TASK-238), so there is nothing to click.

1. `go test -race ./desktop/internal/input/wails/...` in `paratech/` passes (problem codes, DTO, no name/e-mail in logs).
2. `npm --prefix paratech/desktop/frontend run test` passes (libraryTeacher.test.mjs).
3. After TASK-238: set, change and clear a teacher on a subject page; check that the log file contains no name or e-mail.
