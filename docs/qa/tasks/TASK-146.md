# TASK-146 — check steps

Core-only change; nothing is visible in the UI yet (the draft sink is wired in TASK-147).

1. In `paratech/` run `go test -race ./desktop/internal/domain/coursework/answer/... ./desktop/internal/cases/answering/... ./desktop/internal/adapters/hubclient/...`: all pass.
2. Run `go test -race -run Arch ./desktop/tests/...`: passes.
3. Optional fuzz: `go test -run xxx -fuzz FuzzDraft -fuzztime 20s ./desktop/internal/domain/coursework/answer/`: no failures.
4. Asking a plain-text question still produces the same verified answer (Hub with streaming off falls back to the unary call).
