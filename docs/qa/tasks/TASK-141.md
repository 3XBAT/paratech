# QA TASK-141: «Повторить» (core)

No UI yet (TASK-143), so the owner checks through tests only.

1. `go -C <repo>/paratech test -race ./desktop/internal/domain/agent/thread/... ./desktop/internal/cases/agentturn/... ./desktop/internal/cases/agentjournal/... ./desktop/internal/input/agent/... ./desktop/internal/input/wails/...` passes.
2. `go -C <repo>/paratech test ./desktop/tests/...` (architecture test) passes.
3. `GOOS=windows go -C <repo>/paratech vet ./desktop/...` passes.
4. Read `rerun_test.go` in `cases/agentturn`: a finished request repeats under keys `ag:<thread>:2:…`; a second click is `stale`; a request with a live action or a proposal gives `rerun_blocked`; after the action is undone the repeat runs.
