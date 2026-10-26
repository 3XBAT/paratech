# TASK-187: QA

Core only, no UI yet (the topic path is reachable from TASK-188/189).

1. `go test -race ./desktop/internal/domain/coursework/... ./desktop/internal/cases/turnkey/... ./desktop/internal/adapters/sqlite/...` from `paratech/`: green.
2. Migration: start the app on an existing profile; works and their rounds are intact (migration 0031 rebuilds the tables).
3. After TASK-188/189: ask for a work on a topic without an assignment; the notice card, questions, the paper/presentation follow; the work cannot be sent to a teacher.
