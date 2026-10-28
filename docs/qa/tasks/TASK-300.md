# TASK-300 — check steps (owner)

1. Import an old recording through «Импортировать запись» in «Записи и задачи» (see TASK-162). The new row is marked «импортирована».
2. Restart the app and open «Записи и задачи»: the row is still marked «импортирована». A recording made by the app itself has no mark.
3. Tests: `go test -race ./desktop/internal/cases/activity/... ./desktop/internal/input/wails/...`; `npm --prefix paratech/desktop/frontend run test` and `run build`.
