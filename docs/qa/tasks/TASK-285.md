# TASK-285 — QA

There is no UI yet (the subject page is TASK-238), so there is nothing to click.

1. `go test -race ./desktop/internal/domain/library/... ./desktop/internal/adapters/sqlite/... ./desktop/internal/input/wails/... ./desktop/tests/...` in `paratech/` passes (three roles, migration 0044 on real SQLite, problem codes, no name/e-mail in logs).
2. `npm --prefix paratech/desktop/frontend run test` passes (libraryTeacher.test.mjs).
3. On a copy of a real database with a subject that has a teacher: start the new build once; after TASK-238 the same teacher shows as lecturer, practice and lab teacher.
