# TASK-185: проверка владельцем

Кнопки на странице пока нет (TASK-186). Проверка автоматическая:
1. `go test -race ./desktop/internal/input/wails/...` — тесты StartForAssignment проходят.
2. `npm --prefix paratech/desktop/frontend run test` — привязка `startForAssignment` разбирает ответ.
3. После TASK-186: «Сделать под ключ» на странице задания запускает разбор, повторный клик открывает ту же работу без второго платного вызова.
