# TASK-251 — check steps

No visible UI yet (the ring is drawn by TASK-208), so a cold app shows nothing new.

1. From `paratech/`: `go test -race ./desktop/internal/domain/agent/... ./desktop/internal/cases/agentchats/... ./desktop/internal/input/... ./desktop/internal/app/...` — all pass.
2. Architecture test: `go test ./desktop/tests/...` — passes.
3. `npm run test` and `npm run build` in `desktop/frontend` — pass (`parseFill` tests in agent.test.mjs).
4. Read `progress/TASK-251.md`: decisions to confirm (STOPPED is an end of a request; overflow = 100 %).
