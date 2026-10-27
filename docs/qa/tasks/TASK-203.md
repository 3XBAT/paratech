# TASK-203 — check steps

Domain-only change, nothing visible in the app yet (no persistence, no UI; TASK-204/205).

1. From `paratech/`: `go test -race ./desktop/internal/domain/agent/... ./desktop/internal/cases/agentturn/...` — green.
2. `go test ./desktop/tests/...` — architecture test green.
3. Optional: in the app, ask the agent a few questions in one subject: answers still arrive, history is kept (the window is now up to 20 earlier answers).
