# TASK-314 — check steps (owner)

No screen changes yet: the use case, the list filter and «Сегодня» without done assignments. The buttons come with TASK-315/316, the agent tool with TASK-320.

1. Start the app on your current profile (cold start). «Сегодня» and «Задания» look as before: nothing can be marked done from the UI yet, so every assignment is still open and appears where it did.
2. Agent: ask for the list of homework; the answer is the same as before (open ones only).
3. Tests from `paratech/`: `go test -race ./desktop/internal/cases/homeworkflow/... ./desktop/internal/cases/todayfeed/... ./desktop/internal/domain/agent/... ./desktop/internal/adapters/sqlite/... ./desktop/internal/input/agent/...` and `go test ./desktop/tests/...`. They cover: bulk mark with partial failures, «Отменить», the 50-id limit, a done assignment not burning in «Сегодня» but visible in the subject history, the filter on real SQLite, and no calendar calls on done.
