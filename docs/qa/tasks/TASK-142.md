# TASK-142: QA steps

Core change; no visible UI yet (the Rerun button comes with its UI task).

1. Run `go test -race` for `./desktop/internal/domain/coursework/answer/...`, `./desktop/internal/cases/answering/...`, `./desktop/internal/input/agent/...`, `./desktop/internal/adapters/sqlite/...`, `./desktop/tests/...`: all pass.
2. Upgrade check: start the new build on a copy of a database with saved answers. Old answers still open from their cards; the app starts without a migration error (version 30).
3. Ask the agent the same question twice in separate requests: two paid answers (quota drops twice). «Продолжить» after «Стоп» in the same request: no extra charge.
