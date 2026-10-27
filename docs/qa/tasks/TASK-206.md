# TASK-206 — check steps

No UI yet (panel is TASK-207), so nothing to click.

1. From `paratech/`: `go test -race ./desktop/internal/cases/agentchats/... ./desktop/internal/cases/agentturn/... ./desktop/internal/input/... ./desktop/internal/adapters/sqlite/... ./desktop/internal/app/...` — all pass.
2. Architecture test: `go test ./desktop/tests/...` — passes.
3. `npm run test` in `desktop/frontend` — passes (agentchats.test.mjs covers the new bindings).
4. Read `progress/TASK-206.md`: decisions to confirm (missing chat reports `failed`; retry instead of a lock).
