# TASK-205 — check steps

No UI yet (bindings TASK-206, panel TASK-207), so nothing to click.

1. From `paratech/`: `go test -race ./desktop/internal/cases/agentchats/... ./desktop/internal/cases/agentturn/... ./desktop/internal/app/...` — all pass.
2. Architecture test: `go test ./desktop/tests/...` (TestRealTree, TestImportMatrix) — passes.
3. Read `progress/TASK-205.md`: the decisions (Rename is refused while a request runs; Earlier includes the current request) and the one follow-up that blocks wiring (sqlite `RequestMentions`, goes into TASK-206).
