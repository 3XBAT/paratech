# TASK-315 — check steps (owner)

No screen changes yet: the Wails binding and its parsing. The buttons and the toast come with TASK-316.

1. Cold start on your current profile. «Задания» and «Сегодня» look exactly as before: all assignments are open, nothing is struck through, nothing is missing.
2. Tests from `paratech/`: `go test -race ./desktop/internal/input/wails/...` and `npm --prefix desktop/frontend run test`. They cover: per-assignment result codes (changed, unchanged, not_found, deleted, conflict, failed), «Отменить» sending back exactly what the call returned, the 1..50 limit, the list filter open/done/all, `done`/`doneAt` on rows and the opened assignment, `homeworkDone` in «Сегодня».
